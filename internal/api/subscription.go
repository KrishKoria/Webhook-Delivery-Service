package api

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/http"
	"net/url"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/cache"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type SubscriptionHandler struct {
    Queries *database.Queries
    Cache   *cache.RedisSubscriptionCache
}

// SubscriptionResponse is the API response for subscriptions (secrets redacted).
type SubscriptionResponse struct {
    ID         string `json:"id"`
    TargetURL  string `json:"target_url"`
    EventTypes string `json:"event_types,omitempty"`
    HasSecret  bool   `json:"has_secret"`
    CreatedAt  string `json:"created_at,omitempty"`
}

// toSubscriptionResponse converts a database subscription to an API response with secret redacted.
func toSubscriptionResponse(sub database.Subscription) SubscriptionResponse {
    return SubscriptionResponse{
        ID:         sub.ID,
        TargetURL:  sub.TargetUrl,
        EventTypes: sub.EventTypes.String,
        HasSecret:  sub.Secret.Valid && sub.Secret.String != "",
        CreatedAt:  sub.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
    }
}

// validateTargetURL validates that a URL is a valid HTTP/HTTPS URL.
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

// ValidationError represents a field validation error.
type ValidationError struct {
    Field   string
    Message string
}

func (e *ValidationError) Error() string {
    return e.Message
}

func RegisterSubscriptionRoutes(r *gin.Engine, h *SubscriptionHandler) {
    r.POST("/subscriptions", h.CreateSubscription)
    r.GET("/subscriptions", h.ListSubscriptions)
    r.GET("/subscriptions/:id", h.GetSubscription)
    r.PUT("/subscriptions/:id", h.UpdateSubscription)
    r.DELETE("/subscriptions/:id", h.DeleteSubscription)
    r.POST("/subscriptions/:id/rotate-secret", h.RotateSecret)
    r.POST("/subscriptions/:id/test-send", h.TestSend)
}

// RegisterSubscriptionRoutesGroup registers subscription routes on a RouterGroup (for middleware).
func RegisterSubscriptionRoutesGroup(rg *gin.RouterGroup, h *SubscriptionHandler) {
    rg.POST("/subscriptions", h.CreateSubscription)
    rg.GET("/subscriptions", h.ListSubscriptions)
    rg.GET("/subscriptions/:id", h.GetSubscription)
    rg.PUT("/subscriptions/:id", h.UpdateSubscription)
    rg.DELETE("/subscriptions/:id", h.DeleteSubscription)
    rg.POST("/subscriptions/:id/rotate-secret", h.RotateSecret)
    rg.POST("/subscriptions/:id/test-send", h.TestSend)
}
// CreateSubscription handles POST /subscriptions
func (h *SubscriptionHandler) CreateSubscription(c *gin.Context) {
    var req struct {
        TargetUrl  string `json:"target_url" binding:"required"`
        Secret     string `json:"secret"`
        EventTypes string `json:"event_types"` // comma-separated
    }
    if err := c.ShouldBindJSON(&req); err != nil {
        RespondValidationError(c, err.Error())
        return
    }

    // Validate target URL
    if err := validateTargetURL(req.TargetUrl); err != nil {
        if ve, ok := err.(*ValidationError); ok {
            RespondErrorWithField(c, http.StatusBadRequest, ve.Message, ve.Field)
            return
        }
        RespondValidationError(c, err.Error())
        return
    }

    id := uuid.New().String()
    err := h.Queries.CreateSubscription(c, database.CreateSubscriptionParams{
        ID:         id,
        TargetUrl:  req.TargetUrl,
        Secret:     sql.NullString{String: req.Secret, Valid: req.Secret != ""},
        EventTypes: sql.NullString{String: req.EventTypes, Valid: req.EventTypes != ""},
    })
    if err != nil {
        RespondInternalError(c)
        return
    }
    RespondSuccess(c, http.StatusCreated, gin.H{"id": id})
}

// ListSubscriptions handles GET /subscriptions
func (h *SubscriptionHandler) ListSubscriptions(c *gin.Context) {
    subs, err := h.Queries.ListSubscriptions(c)
    if err != nil {
        RespondInternalError(c)
        return
    }

    // Redact secrets from response
    response := make([]SubscriptionResponse, len(subs))
    for i, sub := range subs {
        response[i] = toSubscriptionResponse(sub)
    }
    RespondSuccess(c, http.StatusOK, response)
}

// GetSubscription handles GET /subscriptions/:id
func (h *SubscriptionHandler) GetSubscription(c *gin.Context) {
    id := c.Param("id")
    sub, err := h.Queries.GetSubscription(c, id)
    if err != nil {
        RespondNotFound(c, "subscription")
        return
    }
    // Return response with secret redacted
    RespondSuccess(c, http.StatusOK, toSubscriptionResponse(sub))
}

// UpdateSubscription handles PUT /subscriptions/:id
func (h *SubscriptionHandler) UpdateSubscription(c *gin.Context) {
    id := c.Param("id")
    var req struct {
        TargetURL string `json:"target_url" binding:"required"`
        Secret    string `json:"secret"`
        EventTypes string `json:"event_types"` 
    }
    if err := c.ShouldBindJSON(&req); err != nil {
        RespondValidationError(c, err.Error())
        return
    }

    // Validate target URL
    if err := validateTargetURL(req.TargetURL); err != nil {
        if ve, ok := err.(*ValidationError); ok {
            RespondErrorWithField(c, http.StatusBadRequest, ve.Message, ve.Field)
            return
        }
        RespondValidationError(c, err.Error())
        return
    }

    // Check subscription exists before updating
    if _, err := h.Queries.GetSubscription(c, id); err != nil {
        RespondNotFound(c, "subscription")
        return
    }

    arg := database.UpdateSubscriptionParams{
        TargetUrl: req.TargetURL,
        EventTypes: sql.NullString{
            String: req.EventTypes,
            Valid:  req.EventTypes != "",
        },
		Secret: sql.NullString{
			String: req.Secret,
			Valid:  req.Secret != "",
		},
        ID:        id,
    }
    if err := h.Queries.UpdateSubscription(c, arg); err != nil {
        RespondInternalError(c)
        return
    }
    if h.Cache != nil {
        h.Cache.Del(id)
    }
    c.Status(http.StatusNoContent)
}

// DeleteSubscription handles DELETE /subscriptions/:id
func (h *SubscriptionHandler) DeleteSubscription(c *gin.Context) {
    id := c.Param("id")

    // Check subscription exists before deleting
    if _, err := h.Queries.GetSubscription(c, id); err != nil {
        RespondNotFound(c, "subscription")
        return
    }

    if err := h.Queries.DeleteSubscription(c, id); err != nil {
        RespondInternalError(c)
        return
    }
    if h.Cache != nil {
        h.Cache.Del(id)
    }
    c.Status(http.StatusNoContent)
}

// RotateSecret handles POST /subscriptions/:id/rotate-secret
// This endpoint generates a new secret and returns it (the only time the secret is visible).
func (h *SubscriptionHandler) RotateSecret(c *gin.Context) {
    id := c.Param("id")

    // Check subscription exists
    sub, err := h.Queries.GetSubscription(c, id)
    if err != nil {
        RespondNotFound(c, "subscription")
        return
    }

    // Generate a new secure secret (32 bytes = 64 hex chars)
    newSecret := generateSecureSecret()

    // Update the subscription with the new secret
    arg := database.UpdateSubscriptionParams{
        TargetUrl:  sub.TargetUrl,
        Secret:     sql.NullString{String: newSecret, Valid: true},
        EventTypes: sub.EventTypes,
        ID:         id,
    }
    if err := h.Queries.UpdateSubscription(c, arg); err != nil {
        RespondInternalError(c)
        return
    }

    // Invalidate cache
    if h.Cache != nil {
        h.Cache.Del(id)
    }

    // Return the new secret (this is the ONLY time it should be visible)
    RespondSuccess(c, http.StatusOK, gin.H{
        "id":     id,
        "secret": newSecret,
    })
}

// generateSecureSecret generates a cryptographically secure random secret.
func generateSecureSecret() string {
    b := make([]byte, 32)
    if _, err := rand.Read(b); err != nil {
        // Fallback to UUID if crypto/rand fails (should never happen)
        return uuid.New().String()
    }
    return hex.EncodeToString(b)
}

// TestSend handles POST /subscriptions/:id/test-send
// This endpoint allows admins to trigger a test delivery for a subscription.
func (h *SubscriptionHandler) TestSend(c *gin.Context) {
    id := c.Param("id")

    var req struct {
        Payload   string `json:"payload"`
        EventType string `json:"event_type"`
    }
    // Allow empty body - defaults will be used
    _ = c.ShouldBindJSON(&req)

    // Set defaults if not provided
    if req.Payload == "" {
        req.Payload = `{"event":"test","source":"admin_test_send"}`
    }
    if req.EventType == "" {
        req.EventType = "test"
    }

    // Check subscription exists
    _, err := h.Queries.GetSubscription(c, id)
    if err != nil {
        RespondNotFound(c, "subscription")
        return
    }

    // Create delivery task (bypasses signature verification for admin-initiated tests)
    taskID := uuid.New().String()
    err = h.Queries.CreateDeliveryTask(c, database.CreateDeliveryTaskParams{
        ID:             taskID,
        SubscriptionID: id,
        Payload:        req.Payload,
    })
    if err != nil {
        RespondInternalError(c)
        return
    }

    // Invalidate cache if needed
    if h.Cache != nil {
        h.Cache.Del(id)
    }

    // Return success without exposing any secrets
    RespondSuccess(c, http.StatusAccepted, gin.H{
        "task_id":         taskID,
        "subscription_id": id,
        "status":          "queued",
    })
}