package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/api/middleware"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/cache"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/ops"
	"github.com/gin-gonic/gin"
)

// MaxIdempotencyKeyLen bounds the Idempotency-Key header.
const MaxIdempotencyKeyLen = 255

// WebhookHandler serves the public ingest endpoint.
type WebhookHandler struct {
	Queries *database.Queries
	Cache   *cache.RedisSubscriptionCache
	Ops     *ops.Ops
}

// RegisterWebhookRoutesGroup registers the ingest endpoint.
func RegisterWebhookRoutesGroup(rg *gin.RouterGroup, h *WebhookHandler) {
	rg.POST("/ingest/:subscription_id", h.IngestWebhook)
}

// IngestWebhook accepts a JSON event for a subscription and queues a delivery task.
//
//	415 non-JSON Content-Type · 404 unknown subscription · 503 database unavailable
//	413 body over limit · 400 invalid JSON or Idempotency-Key · 401 bad signature
//	204 event type not in the subscription's allowlist (dropped, not an error)
//	202 {task_id} queued · 200 {task_id, duplicate:true} for a repeated Idempotency-Key
//
// Paused subscriptions still accept events; the worker defers their delivery.
func (h *WebhookHandler) IngestWebhook(c *gin.Context) {
	if !middleware.IsJSONContentType(c.GetHeader("Content-Type")) {
		RespondError(c, http.StatusUnsupportedMediaType, "Content-Type must be application/json", ErrCodeUnsupportedMedia)
		return
	}
	idemKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if len(idemKey) > MaxIdempotencyKeyLen {
		RespondErrorWithField(c, http.StatusBadRequest, "Idempotency-Key must be at most 255 characters", "Idempotency-Key")
		return
	}

	subID := c.Param("subscription_id")
	sub, err := h.subscription(c, subID)
	if errors.Is(err, sql.ErrNoRows) {
		RespondNotFound(c, "subscription")
		return
	}
	if err != nil {
		respondUnavailable(c, "ingest: load subscription", err)
		return
	}

	body, ok := readBody(c)
	if !ok {
		return
	}
	if !json.Valid(body) {
		RespondValidationError(c, "request body must be valid JSON")
		return
	}

	// Authenticate before revealing anything about the subscription's configuration
	// (such as which event types it accepts).
	if sub.Secret.Valid && sub.Secret.String != "" &&
		!verifySignature(body, sub.Secret.String, c.GetHeader("X-Hub-Signature-256")) {
		RespondUnauthorized(c, "invalid signature")
		return
	}

	eventType := strings.TrimSpace(c.GetHeader("X-Event-Type"))
	if !subscriptionAllowsEvent(sub, eventType) {
		c.Status(http.StatusNoContent)
		return
	}

	taskID, duplicate, err := h.Ops.Enqueue(c.Request.Context(), ops.EnqueueInput{
		SubscriptionID: sub.ID,
		Payload:        string(body),
		EventType:      eventType,
		IdempotencyKey: idemKey,
	})
	if err != nil {
		respondUnavailable(c, "ingest: enqueue task", err)
		return
	}
	if duplicate {
		RespondSuccess(c, http.StatusOK, gin.H{"task_id": taskID, "duplicate": true})
		return
	}
	RespondSuccess(c, http.StatusAccepted, gin.H{"task_id": taskID})
}

// subscription loads a subscription through the cache when one is configured.
func (h *WebhookHandler) subscription(c *gin.Context, id string) (database.Subscription, error) {
	if h.Cache != nil {
		if sub, ok := h.Cache.Get(id); ok {
			return sub, nil
		}
	}
	sub, err := h.Queries.GetSubscription(c.Request.Context(), id)
	if err != nil {
		return database.Subscription{}, err
	}
	if h.Cache != nil {
		h.Cache.Set(id, sub)
	}
	return sub, nil
}

// verifySignature checks an inbound "sha256=<hex HMAC-SHA256(secret, body)>" header in constant time.
func verifySignature(body []byte, secret, signature string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

// subscriptionAllowsEvent reports whether eventType passes the subscription's allowlist.
// No allowlist, or no event type on the request, means everything passes.
func subscriptionAllowsEvent(sub database.Subscription, eventType string) bool {
	if !sub.EventTypes.Valid || strings.TrimSpace(sub.EventTypes.String) == "" || eventType == "" {
		return true
	}
	for et := range strings.SplitSeq(sub.EventTypes.String, ",") {
		if strings.TrimSpace(et) == eventType {
			return true
		}
	}
	return false
}
