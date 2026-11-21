package api

import (
	"cmp"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/delivery"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/ops"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// SubscriptionHandler serves the subscription admin API.
type SubscriptionHandler struct {
	Queries *database.Queries
	Ops     *ops.Ops
	// AllowPrivateTargets skips the SSRF (private/loopback address) check on
	// target URLs. Set from WEBHOOK_ALLOW_PRIVATE_IPS for local development only.
	AllowPrivateTargets bool
}

// SubscriptionResponse is the API view of a subscription. The secret is never returned.
type SubscriptionResponse struct {
	ID         string `json:"id"`
	TargetURL  string `json:"target_url"`
	EventTypes string `json:"event_types,omitempty"`
	HasSecret  bool   `json:"has_secret"`
	Paused     bool   `json:"paused"`
	CreatedAt  string `json:"created_at,omitempty"`
	UpdatedAt  string `json:"updated_at,omitempty"`
}

func toSubscriptionResponse(sub database.Subscription) SubscriptionResponse {
	r := SubscriptionResponse{
		ID:         sub.ID,
		TargetURL:  sub.TargetUrl,
		EventTypes: sub.EventTypes.String,
		HasSecret:  sub.Secret.Valid && sub.Secret.String != "",
		Paused:     sub.Paused != 0,
	}
	if !sub.CreatedAt.IsZero() {
		r.CreatedAt = ts(sub.CreatedAt)
	}
	if !sub.UpdatedAt.IsZero() {
		r.UpdatedAt = ts(sub.UpdatedAt)
	}
	return r
}

// ValidationError is a field-level validation failure.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

// validateTargetURL checks that a URL is an absolute http(s) URL with a host.
func validateTargetURL(targetURL string) error {
	if targetURL == "" {
		return &ValidationError{Field: "target_url", Message: "target_url is required"}
	}
	u, err := url.Parse(targetURL)
	if err != nil {
		return &ValidationError{Field: "target_url", Message: "invalid URL format"}
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return &ValidationError{Field: "target_url", Message: "URL scheme must be http or https"}
	}
	if u.Host == "" {
		return &ValidationError{Field: "target_url", Message: "URL must have a host"}
	}
	return nil
}

// checkTarget validates shape and, unless private targets are allowed, rejects
// URLs that resolve to private, loopback or link-local addresses (SSRF).
// It answers 400 itself and returns false on failure.
func (h *SubscriptionHandler) checkTarget(c *gin.Context, target string) bool {
	if err := validateTargetURL(target); err != nil {
		var ve *ValidationError
		errors.As(err, &ve)
		RespondErrorWithField(c, http.StatusBadRequest, ve.Message, ve.Field)
		return false
	}
	if !h.AllowPrivateTargets {
		if err := delivery.ValidateTargetURL(target); err != nil {
			RespondErrorWithField(c, http.StatusBadRequest, "target_url is not an allowed public address", "target_url")
			return false
		}
	}
	return true
}

// RegisterSubscriptionRoutesGroup registers subscription routes under both the
// legacy /subscriptions prefix and /api/subscriptions.
func RegisterSubscriptionRoutesGroup(rg *gin.RouterGroup, h *SubscriptionHandler) {
	for _, p := range []string{"/subscriptions", "/api/subscriptions"} {
		rg.POST(p, h.CreateSubscription)
		rg.GET(p, h.ListSubscriptions)
		rg.GET(p+"/:id", h.GetSubscription)
		rg.PUT(p+"/:id", h.UpdateSubscription)
		rg.DELETE(p+"/:id", h.DeleteSubscription)
		rg.POST(p+"/:id/rotate-secret", h.RotateSecret)
		rg.POST(p+"/:id/test-send", h.TestSend)
		rg.POST(p+"/:id/pause", h.PauseSubscription)
		rg.POST(p+"/:id/resume", h.ResumeSubscription)
	}
}

type createSubscriptionRequest struct {
	TargetURL  string `json:"target_url"`
	Secret     string `json:"secret"`
	EventTypes string `json:"event_types"` // comma-separated allowlist; empty accepts all
}

// CreateSubscription handles POST /subscriptions.
func (h *SubscriptionHandler) CreateSubscription(c *gin.Context) {
	var req createSubscriptionRequest
	if !bindJSON(c, &req, false) {
		return
	}
	req.TargetURL = strings.TrimSpace(req.TargetURL)
	if !h.checkTarget(c, req.TargetURL) {
		return
	}
	id := uuid.NewString()
	eventTypes := normalizeEventTypes(req.EventTypes)
	err := h.Queries.CreateSubscription(c.Request.Context(), database.CreateSubscriptionParams{
		ID:         id,
		TargetUrl:  req.TargetURL,
		Secret:     sql.NullString{String: req.Secret, Valid: req.Secret != ""},
		EventTypes: sql.NullString{String: eventTypes, Valid: eventTypes != ""},
	})
	if err != nil {
		respondInternal(c, "create subscription", err)
		return
	}
	h.respondSubscription(c, http.StatusCreated, id)
}

// ListSubscriptions handles GET /subscriptions?limit=&cursor=.
func (h *SubscriptionHandler) ListSubscriptions(c *gin.Context) {
	limit, cur, ok := pageParams(c)
	if !ok {
		return
	}
	curTS, curID := cur.params()
	subs, err := h.Queries.APIListSubscriptions(c.Request.Context(), database.APIListSubscriptionsParams{
		CursorTs: curTS, CursorID: curID, Lim: int64(limit + 1),
	})
	if err != nil {
		respondInternal(c, "list subscriptions", err)
		return
	}
	RespondSuccess(c, http.StatusOK, pageOf(subs, limit,
		func(s database.Subscription) (time.Time, string) { return s.CreatedAt, s.ID },
		toSubscriptionResponse))
}

// GetSubscription handles GET /subscriptions/:id.
func (h *SubscriptionHandler) GetSubscription(c *gin.Context) {
	h.respondSubscription(c, http.StatusOK, c.Param("id"))
}

func (h *SubscriptionHandler) respondSubscription(c *gin.Context, status int, id string) {
	sub, err := h.Queries.GetSubscription(c.Request.Context(), id)
	if err != nil {
		respondDBError(c, "subscription", err)
		return
	}
	RespondSuccess(c, status, toSubscriptionResponse(sub))
}

type updateSubscriptionRequest struct {
	TargetURL  string  `json:"target_url"`
	EventTypes string  `json:"event_types"`
	Secret     *string `json:"secret"`       // omitted or "" keeps the current secret
	ClearSecret bool   `json:"clear_secret"` // true removes the secret
}

// UpdateSubscription handles PUT /subscriptions/:id. target_url and event_types
// are replaced; the secret is kept unless a new one is given or clear_secret is true.
func (h *SubscriptionHandler) UpdateSubscription(c *gin.Context) {
	var req updateSubscriptionRequest
	if !bindJSON(c, &req, false) {
		return
	}
	req.TargetURL = strings.TrimSpace(req.TargetURL)
	if !h.checkTarget(c, req.TargetURL) {
		return
	}
	if req.ClearSecret && req.Secret != nil && *req.Secret != "" {
		RespondErrorWithField(c, http.StatusBadRequest, "secret and clear_secret cannot both be set", "clear_secret")
		return
	}
	id := c.Param("id")
	err := h.Ops.UpdateSubscription(c.Request.Context(), ops.UpdateSubscriptionInput{
		ID:          id,
		TargetURL:   req.TargetURL,
		EventTypes:  normalizeEventTypes(req.EventTypes),
		Secret:      req.Secret,
		ClearSecret: req.ClearSecret,
	})
	if !h.opsErr(c, "update subscription", err) {
		return
	}
	h.respondSubscription(c, http.StatusOK, id)
}

// DeleteSubscription handles DELETE /subscriptions/:id and removes its tasks,
// logs, DLQ entries and schedules.
func (h *SubscriptionHandler) DeleteSubscription(c *gin.Context) {
	err := h.Ops.DeleteSubscription(c.Request.Context(), c.Param("id"))
	if !h.opsErr(c, "delete subscription", err) {
		return
	}
	c.Status(http.StatusNoContent)
}

// RotateSecret handles POST /subscriptions/:id/rotate-secret. The new secret is
// returned once, in this response only.
func (h *SubscriptionHandler) RotateSecret(c *gin.Context) {
	id := c.Param("id")
	sub, err := h.Queries.GetSubscription(c.Request.Context(), id)
	if err != nil {
		respondDBError(c, "subscription", err)
		return
	}
	secret, err := generateSecureSecret()
	if err != nil {
		respondInternal(c, "generate secret", err)
		return
	}
	err = h.Ops.UpdateSubscription(c.Request.Context(), ops.UpdateSubscriptionInput{
		ID:         id,
		TargetURL:  sub.TargetUrl,
		EventTypes: sub.EventTypes.String,
		Secret:     &secret,
	})
	if !h.opsErr(c, "rotate secret", err) {
		return
	}
	RespondSuccess(c, http.StatusOK, gin.H{"id": id, "secret": secret})
}

// generateSecureSecret returns 32 random bytes, hex-encoded.
func generateSecureSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

const (
	defaultTestPayload   = `{"event":"test","source":"admin_test_send"}`
	defaultTestEventType = "test"
)

// TestSend handles POST /subscriptions/:id/test-send with an optional
// {"payload": <JSON>, "event_type": "..."} body. Signature checks and the event
// allowlist are bypassed because the admin initiated it.
func (h *SubscriptionHandler) TestSend(c *gin.Context) {
	var req struct {
		Payload   json.RawMessage `json:"payload"`
		EventType string          `json:"event_type"`
	}
	if !bindJSON(c, &req, true) {
		return
	}
	payload := defaultTestPayload
	if len(req.Payload) > 0 && string(req.Payload) != "null" {
		// A JSON string is taken as the raw payload text; any other JSON value is sent as-is.
		var s string
		if json.Unmarshal(req.Payload, &s) == nil {
			payload = s
		} else {
			payload = string(req.Payload)
		}
	}
	eventType := cmp.Or(req.EventType, defaultTestEventType)

	id := c.Param("id")
	if _, err := h.Queries.GetSubscription(c.Request.Context(), id); err != nil {
		respondDBError(c, "subscription", err)
		return
	}
	taskID, _, err := h.Ops.Enqueue(c.Request.Context(), ops.EnqueueInput{
		SubscriptionID: id, Payload: payload, EventType: eventType,
	})
	if err != nil {
		respondInternal(c, "test-send enqueue", err)
		return
	}
	RespondSuccess(c, http.StatusAccepted, gin.H{"task_id": taskID, "subscription_id": id, "status": "queued"})
}

// PauseSubscription handles POST /subscriptions/:id/pause. Ingest keeps
// accepting events; the worker holds deliveries until resumed.
func (h *SubscriptionHandler) PauseSubscription(c *gin.Context) { h.setPaused(c, true) }

// ResumeSubscription handles POST /subscriptions/:id/resume.
func (h *SubscriptionHandler) ResumeSubscription(c *gin.Context) { h.setPaused(c, false) }

func (h *SubscriptionHandler) setPaused(c *gin.Context, paused bool) {
	id := c.Param("id")
	if !h.opsErr(c, "set paused", h.Ops.SetPaused(c.Request.Context(), id, paused)) {
		return
	}
	h.respondSubscription(c, http.StatusOK, id)
}

// opsErr maps an ops error to a response. It returns true when err is nil.
func (h *SubscriptionHandler) opsErr(c *gin.Context, msg string, err error) bool {
	return respondOpsErr(c, "subscription", msg, err)
}

// respondOpsErr maps ops.ErrNotFound → 404, ops.ErrConflict → 409, other → 500.
// It returns true when err is nil.
func respondOpsErr(c *gin.Context, resource, msg string, err error) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, ops.ErrNotFound), errors.Is(err, sql.ErrNoRows):
		RespondNotFound(c, resource)
	case errors.Is(err, ops.ErrConflict):
		RespondError(c, http.StatusConflict, resource+" is not in a state that allows this action", ErrCodeConflict)
	default:
		respondInternal(c, msg, err)
	}
	return false
}

// normalizeEventTypes trims each comma-separated entry and drops empties.
func normalizeEventTypes(s string) string {
	var parts []string
	for p := range strings.SplitSeq(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, ",")
}
