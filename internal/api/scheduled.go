package api

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ScheduledHandler serves scheduled (future and recurring) webhooks.
type ScheduledHandler struct {
	Queries *database.Queries
}

// RegisterScheduledRoutesGroup registers the JSON scheduled-webhook API.
func RegisterScheduledRoutesGroup(rg *gin.RouterGroup, h *ScheduledHandler) {
	rg.POST("/api/scheduled", h.CreateScheduledAPI)
	rg.GET("/api/scheduled", h.ListScheduledAPI)
	rg.GET("/api/scheduled/:id", h.GetScheduledAPI)
	rg.DELETE("/api/scheduled/:id", h.DeleteScheduledAPI)
	rg.POST("/api/scheduled/:id/cancel", h.CancelScheduledAPI)
}

var validRecurrences = map[string]bool{"": true, "none": true, "daily": true, "weekly": true, "monthly": true}

// CreateScheduledRequest is the body of POST /api/scheduled.
type CreateScheduledRequest struct {
	SubscriptionID string `json:"subscription_id"`
	Payload        string `json:"payload"`
	ScheduledFor   string `json:"scheduled_for"` // RFC3339, in the future
	Recurrence     string `json:"recurrence"`    // none, daily, weekly, monthly
}

// ScheduledWebhookResponse is the API view of a scheduled webhook.
// Status is pending, enqueued, cancelled or failed (legacy "delivered" reads as enqueued).
type ScheduledWebhookResponse struct {
	ID             string  `json:"id"`
	SubscriptionID string  `json:"subscription_id"`
	Payload        string  `json:"payload"`
	ScheduledFor   string  `json:"scheduled_for"`
	Recurrence     *string `json:"recurrence"`
	Status         string  `json:"status"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

func toScheduledResponse(s database.ScheduledWebhook) ScheduledWebhookResponse {
	r := ScheduledWebhookResponse{
		ID: s.ID, SubscriptionID: s.SubscriptionID, Payload: s.Payload,
		ScheduledFor: ts(s.ScheduledFor), Status: s.Status,
		CreatedAt: ts(s.CreatedAt), UpdatedAt: ts(s.UpdatedAt),
	}
	if r.Status == "delivered" {
		r.Status = "enqueued"
	}
	if s.Recurrence.Valid && s.Recurrence.String != "" && s.Recurrence.String != "none" {
		r.Recurrence = &s.Recurrence.String
	}
	return r
}

// validateSchedule checks a schedule request. It returns a field and message on failure.
func validateSchedule(req CreateScheduledRequest) (time.Time, string, string) {
	switch {
	case req.SubscriptionID == "":
		return time.Time{}, "subscription_id", "subscription_id is required"
	case req.Payload == "":
		return time.Time{}, "payload", "payload is required"
	case !validRecurrences[req.Recurrence]:
		return time.Time{}, "recurrence", "recurrence must be none, daily, weekly or monthly"
	}
	at, err := time.Parse(time.RFC3339, req.ScheduledFor)
	if err != nil {
		return time.Time{}, "scheduled_for", "scheduled_for must be RFC3339, e.g. 2030-01-15T14:30:00Z"
	}
	if !at.After(time.Now()) {
		return time.Time{}, "scheduled_for", "scheduled_for must be in the future"
	}
	return at, "", ""
}

// insertSchedule validates and stores a schedule. It answers errors itself and
// returns the new ID, or "" on failure.
func (h *ScheduledHandler) insertSchedule(c *gin.Context, req CreateScheduledRequest) string {
	at, field, msg := validateSchedule(req)
	if field != "" {
		RespondErrorWithField(c, http.StatusBadRequest, msg, field)
		return ""
	}
	if _, err := h.Queries.GetSubscription(c.Request.Context(), req.SubscriptionID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			RespondErrorWithField(c, http.StatusBadRequest, "subscription not found", "subscription_id")
		} else {
			respondInternal(c, "schedule: load subscription", err)
		}
		return ""
	}
	id := uuid.NewString()
	err := h.Queries.CreateScheduledWebhook(c.Request.Context(), database.CreateScheduledWebhookParams{
		ID:             id,
		SubscriptionID: req.SubscriptionID,
		Payload:        req.Payload,
		ScheduledFor:   at.UTC(),
		Recurrence:     sql.NullString{String: req.Recurrence, Valid: req.Recurrence != "" && req.Recurrence != "none"},
	})
	if err != nil {
		respondInternal(c, "create scheduled webhook", err)
		return ""
	}
	return id
}

// CreateScheduledAPI handles POST /api/scheduled.
func (h *ScheduledHandler) CreateScheduledAPI(c *gin.Context) {
	var req CreateScheduledRequest
	if !bindJSON(c, &req, false) {
		return
	}
	id := h.insertSchedule(c, req)
	if id == "" {
		return
	}
	h.respondScheduled(c, http.StatusCreated, id)
}

// ListScheduledAPI handles GET /api/scheduled?subscription_id=&status=&limit=&cursor=,
// newest scheduled_for first.
func (h *ScheduledHandler) ListScheduledAPI(c *gin.Context) {
	limit, cur, ok := pageParams(c)
	if !ok {
		return
	}
	curTS, curID := cur.params()
	rows, err := h.Queries.APIListScheduled(c.Request.Context(), database.APIListScheduledParams{
		SubscriptionID: queryString(c, "subscription_id"),
		Status:         queryString(c, "status"),
		CursorTs:       curTS,
		CursorID:       curID,
		Lim:            int64(limit + 1),
	})
	if err != nil {
		respondInternal(c, "list scheduled webhooks", err)
		return
	}
	RespondSuccess(c, http.StatusOK, pageOf(rows, limit,
		func(s database.ScheduledWebhook) (time.Time, string) { return s.ScheduledFor, s.ID },
		toScheduledResponse))
}

// GetScheduledAPI handles GET /api/scheduled/:id.
func (h *ScheduledHandler) GetScheduledAPI(c *gin.Context) {
	h.respondScheduled(c, http.StatusOK, c.Param("id"))
}

func (h *ScheduledHandler) respondScheduled(c *gin.Context, status int, id string) {
	s, err := h.Queries.GetScheduledWebhook(c.Request.Context(), id)
	if err != nil {
		respondDBError(c, "scheduled webhook", err)
		return
	}
	RespondSuccess(c, status, toScheduledResponse(s))
}

// DeleteScheduledAPI handles DELETE /api/scheduled/:id.
func (h *ScheduledHandler) DeleteScheduledAPI(c *gin.Context) {
	n, err := h.Queries.APIDeleteScheduled(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondInternal(c, "delete scheduled webhook", err)
		return
	}
	if n == 0 {
		RespondNotFound(c, "scheduled webhook")
		return
	}
	c.Status(http.StatusNoContent)
}

// CancelScheduledAPI handles POST /api/scheduled/:id/cancel. Only a pending
// schedule can be cancelled; anything else is 409.
func (h *ScheduledHandler) CancelScheduledAPI(c *gin.Context) {
	id := c.Param("id")
	n, err := h.Queries.APICancelScheduled(c.Request.Context(), id)
	if err != nil {
		respondInternal(c, "cancel scheduled webhook", err)
		return
	}
	if n == 0 {
		if _, err := h.Queries.GetScheduledWebhook(c.Request.Context(), id); err != nil {
			respondDBError(c, "scheduled webhook", err)
			return
		}
		RespondError(c, http.StatusConflict, "only a pending scheduled webhook can be cancelled", ErrCodeConflict)
		return
	}
	h.respondScheduled(c, http.StatusOK, id)
}
