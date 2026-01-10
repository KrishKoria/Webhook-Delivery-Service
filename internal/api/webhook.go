package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/cache"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type WebhookHandler struct {
    Queries *database.Queries
    Cache   *cache.RedisSubscriptionCache
}

// RegisterWebhookRoutes registers the webhook ingestion endpoint.
func RegisterWebhookRoutes(r *gin.Engine, h *WebhookHandler) {
    r.POST("/ingest/:subscription_id", h.IngestWebhook)
}

// RegisterWebhookRoutesGroup registers webhook routes on a RouterGroup (for middleware).
func RegisterWebhookRoutesGroup(rg *gin.RouterGroup, h *WebhookHandler) {
    rg.POST("/ingest/:subscription_id", h.IngestWebhook)
}

// IngestWebhook handles incoming webhooks.
func (h *WebhookHandler) IngestWebhook(c *gin.Context) {
    subID := c.Param("subscription_id")
    eventType := c.GetHeader("X-Event-Type")
    var sub database.Subscription
    var ok bool
    var err error
    if h.Cache != nil { 
        sub, ok = h.Cache.Get(subID)
    }
    if !ok {
        sub, err = h.Queries.GetSubscription(c, subID)
        if err != nil {
            log.Printf("Error fetching subscription %s from DB: %v", subID, err)
            RespondNotFound(c, "subscription")
            return
        }
        if h.Cache != nil { 
            h.Cache.Set(subID, sub)
        }
    }
    body, err := io.ReadAll(c.Request.Body)
    if err != nil {
        RespondError(c, http.StatusBadRequest, "invalid request body", ErrCodeBadRequest)
        return
    }
    defer c.Request.Body.Close()

    // Event allowlist check - returns 204 No Content if not allowed (not an error)
    if !subscriptionAllowsEvent(sub, eventType) {
        c.Status(http.StatusNoContent)
        return
    }

    // Signature verification for subscriptions with secrets configured
    if sub.Secret.Valid && sub.Secret.String != "" {
        sig := c.GetHeader("X-Hub-Signature-256")
        if !verifySignature(body, sub.Secret.String, sig) {
            RespondUnauthorized(c, "invalid signature")
            return
        }
    }

    taskID := uuid.New().String()
    err = h.Queries.CreateDeliveryTask(c, database.CreateDeliveryTaskParams{
        ID:             taskID,
        SubscriptionID: subID,
        Payload:        string(body),
    })
    if err != nil {
        log.Printf("Error creating delivery task for subscription %s: %v", subID, err)
        RespondInternalError(c)
        return
    }

    c.Status(http.StatusAccepted)
}

func verifySignature(body []byte, secret, signature string) bool {
    mac := hmac.New(sha256.New, []byte(secret))
    mac.Write(body)
    expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
    return hmac.Equal([]byte(expected), []byte(signature))
}

func subscriptionAllowsEvent(sub database.Subscription, eventType string) bool {
    if sub.EventTypes.Valid && sub.EventTypes.String != "" && eventType != "" {
        allowed := strings.Split(sub.EventTypes.String, ",")
        for _, et := range allowed {
            if strings.TrimSpace(et) == eventType {
                return true
            }
        }
        return false
    }
    return true
}
