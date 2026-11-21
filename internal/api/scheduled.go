package api

import (
	"database/sql"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type ScheduledHandler struct {
    Queries *database.Queries
}

func RegisterScheduledRoutes(r *gin.Engine, h *ScheduledHandler) {
    // UI routes (form-based)
    r.POST("/scheduled", h.CreateScheduled)
    r.GET("/scheduled", h.ListScheduled)
    r.DELETE("/scheduled/:id", h.DeleteScheduled)
    
    // API routes (JSON-based)
    r.POST("/api/scheduled", h.CreateScheduledAPI)
    r.GET("/api/scheduled", h.ListScheduledAPI)
    r.GET("/api/scheduled/:id", h.GetScheduledAPI)
    r.DELETE("/api/scheduled/:id", h.DeleteScheduledAPI)
    r.PUT("/api/scheduled/:id/cancel", h.CancelScheduledAPI)
}

// RegisterScheduledRoutesGroup registers scheduled routes on a RouterGroup (for middleware).
func RegisterScheduledRoutesGroup(rg *gin.RouterGroup, h *ScheduledHandler) {
    // UI routes (form-based)
    rg.POST("/scheduled", h.CreateScheduled)
    rg.GET("/scheduled", h.ListScheduled)
    rg.DELETE("/scheduled/:id", h.DeleteScheduled)
    
    // API routes (JSON-based)
    rg.POST("/api/scheduled", h.CreateScheduledAPI)
    rg.GET("/api/scheduled", h.ListScheduledAPI)
    rg.GET("/api/scheduled/:id", h.GetScheduledAPI)
    rg.DELETE("/api/scheduled/:id", h.DeleteScheduledAPI)
    rg.PUT("/api/scheduled/:id/cancel", h.CancelScheduledAPI)
}

type CreateScheduledRequest struct {
    SubscriptionID string `json:"subscription_id" binding:"required"`
    Payload        string `json:"payload" binding:"required"`
    ScheduledFor   string `json:"scheduled_for" binding:"required"`
    Recurrence     string `json:"recurrence"` // none, daily, weekly, monthly
}

type ScheduledWebhookResponse struct {
    ID             string  `json:"id"`
    SubscriptionID string  `json:"subscription_id"`
    Payload        string  `json:"payload"`
    ScheduledFor   string  `json:"scheduled_for"`
    Recurrence     *string `json:"recurrence,omitempty"`
    Status         string  `json:"status"`
    CreatedAt      string  `json:"created_at"`
    UpdatedAt      string  `json:"updated_at"`
}

type ScheduledListResponse struct {
    Webhooks []ScheduledWebhookResponse `json:"webhooks"`
    Page     int                        `json:"page"`
    Limit    int                        `json:"limit"`
    HasMore  bool                       `json:"has_more"`
}
// CreateScheduled handles POST /scheduled
func (h *ScheduledHandler) CreateScheduled(c *gin.Context) {
    subscriptionID := c.PostForm("subscription_id")
    payload := c.PostForm("payload")
    scheduledForStr := c.PostForm("scheduled_for")
    recurrence := c.PostForm("recurrence")

    if subscriptionID == "" || payload == "" || scheduledForStr == "" {
        c.JSON(http.StatusBadRequest, gin.H{"error": "Missing required form fields"})
        return
    }

    scheduledFor, err := time.Parse(time.RFC3339, scheduledForStr)
    if err != nil {
        log.Printf("Error parsing scheduled_for time '%s': %v", scheduledForStr, err)
        c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid scheduled_for date format. Use YYYY-MM-DDTHH:MM."})
        return
    }
    if scheduledFor.Before(time.Now()) {
        c.JSON(http.StatusBadRequest, gin.H{"error": "Scheduled time must be in the future"})
        return
    }

    id := uuid.New().String()
    err = h.Queries.CreateScheduledWebhook(c, database.CreateScheduledWebhookParams{
        ID:             id,
        SubscriptionID: subscriptionID,
        Payload:        payload,
        ScheduledFor:   scheduledFor.UTC().Format(time.RFC3339), 
        Recurrence:     sql.NullString{String: recurrence, Valid: recurrence != "" && recurrence != "none"},
    })
    if err != nil {
        log.Printf("Error creating scheduled webhook in DB: %v", err)
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to schedule webhook"})
        return
    }

    redirectURL := "/ui/subscriptions/"
    c.Redirect(http.StatusFound, redirectURL)
}
// ListScheduled handles GET /scheduled
func (h *ScheduledHandler) ListScheduled(c *gin.Context) {
    subID := c.Query("subscription_id") 
    limit := int64(1000) 
    offset := int64(0)

    var tasks []database.ScheduledWebhook
    var err error

    if subID != "" {
        log.Printf("Fetching scheduled webhooks for subscription ID: %s", subID)
        tasks, err = h.Queries.ListScheduledWebhooks(c, database.ListScheduledWebhooksParams{
            SubscriptionID: subID,
            Limit:          limit,
            Offset:         offset,
        })
    } else {
        log.Println("Fetching all scheduled webhooks (Global View)")
        tasks, err = h.Queries.ListAllScheduledWebhooks(c, database.ListAllScheduledWebhooksParams{
            Limit:  limit,
            Offset: offset,
        })
    }

    if err != nil {
        log.Printf("Error fetching scheduled webhooks from DB: %v", err)
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error fetching scheduled webhooks"})
        return
    }

    if tasks == nil {
        tasks = []database.ScheduledWebhook{} 
    }

    log.Printf("Successfully fetched %d scheduled webhooks", len(tasks))
    c.Header("Cache-Control", "no-store, no-cache, must-revalidate, proxy-revalidate")
    c.Header("Pragma", "no-cache")
    c.Header("Expires", "0")
    c.JSON(http.StatusOK, tasks)
}
// DeleteScheduled handles DELETE /scheduled/:id
func (h *ScheduledHandler) DeleteScheduled(c *gin.Context) {
    id := c.Param("id")
    err := h.Queries.DeleteScheduledWebhook(c, id)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }
    c.Status(http.StatusNoContent)
}

// --- API Handlers (JSON-based) ---

// CreateScheduledAPI handles POST /api/scheduled
func (h *ScheduledHandler) CreateScheduledAPI(c *gin.Context) {
    var req CreateScheduledRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request", "details": err.Error()})
        return
    }

    // Validate recurrence
    validRecurrences := map[string]bool{"": true, "none": true, "daily": true, "weekly": true, "monthly": true}
    if !validRecurrences[req.Recurrence] {
        c.JSON(http.StatusBadRequest, gin.H{"error": "invalid recurrence value, must be: none, daily, weekly, or monthly"})
        return
    }

    // Parse scheduled time
    scheduledFor, err := time.Parse(time.RFC3339, req.ScheduledFor)
    if err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "invalid scheduled_for format, use RFC3339 (e.g., 2024-01-15T14:30:00Z)"})
        return
    }

    if scheduledFor.Before(time.Now()) {
        c.JSON(http.StatusBadRequest, gin.H{"error": "scheduled time must be in the future"})
        return
    }

    // Validate subscription exists
    _, err = h.Queries.GetSubscription(c, req.SubscriptionID)
    if err != nil {
        if err == sql.ErrNoRows {
            c.JSON(http.StatusBadRequest, gin.H{"error": "subscription not found"})
            return
        }
        c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to validate subscription"})
        return
    }

    id := uuid.New().String()
    err = h.Queries.CreateScheduledWebhook(c, database.CreateScheduledWebhookParams{
        ID:             id,
        SubscriptionID: req.SubscriptionID,
        Payload:        req.Payload,
        ScheduledFor:   scheduledFor.UTC().Format(time.RFC3339),
        Recurrence:     sql.NullString{String: req.Recurrence, Valid: req.Recurrence != "" && req.Recurrence != "none"},
    })
    if err != nil {
        log.Printf("Error creating scheduled webhook: %v", err)
        c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create scheduled webhook"})
        return
    }

    var recurrence *string
    if req.Recurrence != "" && req.Recurrence != "none" {
        recurrence = &req.Recurrence
    }

    c.JSON(http.StatusCreated, ScheduledWebhookResponse{
        ID:             id,
        SubscriptionID: req.SubscriptionID,
        Payload:        req.Payload,
        ScheduledFor:   scheduledFor.Format(time.RFC3339),
        Recurrence:     recurrence,
        Status:         "pending",
        CreatedAt:      time.Now().Format(time.RFC3339),
        UpdatedAt:      time.Now().Format(time.RFC3339),
    })
}

// ListScheduledAPI handles GET /api/scheduled
func (h *ScheduledHandler) ListScheduledAPI(c *gin.Context) {
    subID := c.Query("subscription_id")
    status := c.Query("status")
    page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
    limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
    
    if page < 1 {
        page = 1
    }
    if limit < 1 || limit > 100 {
        limit = 20
    }
    offset := (page - 1) * limit

    var tasks []database.ScheduledWebhook
    var err error

    if subID != "" {
        tasks, err = h.Queries.ListScheduledWebhooks(c, database.ListScheduledWebhooksParams{
            SubscriptionID: subID,
            Limit:          int64(limit + 1),
            Offset:         int64(offset),
        })
    } else {
        tasks, err = h.Queries.ListAllScheduledWebhooks(c, database.ListAllScheduledWebhooksParams{
            Limit:  int64(limit + 1),
            Offset: int64(offset),
        })
    }

    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list scheduled webhooks"})
        return
    }

    // Filter by status if provided
    if status != "" {
        filtered := make([]database.ScheduledWebhook, 0)
        for _, t := range tasks {
            if t.Status == status {
                filtered = append(filtered, t)
            }
        }
        tasks = filtered
    }

    hasMore := len(tasks) > limit
    if hasMore {
        tasks = tasks[:limit]
    }

    resp := ScheduledListResponse{
        Webhooks: make([]ScheduledWebhookResponse, 0, len(tasks)),
        Page:     page,
        Limit:    limit,
        HasMore:  hasMore,
    }

    for _, t := range tasks {
        webhook := ScheduledWebhookResponse{
            ID:             t.ID,
            SubscriptionID: t.SubscriptionID,
            Payload:        t.Payload,
            ScheduledFor:   t.ScheduledFor.Format(time.RFC3339),
            Status:         t.Status,
            CreatedAt:      t.CreatedAt.Format(time.RFC3339),
            UpdatedAt:      t.UpdatedAt.Format(time.RFC3339),
        }
        if t.Recurrence.Valid && t.Recurrence.String != "" {
            webhook.Recurrence = &t.Recurrence.String
        }
        resp.Webhooks = append(resp.Webhooks, webhook)
    }

    c.JSON(http.StatusOK, resp)
}

// GetScheduledAPI handles GET /api/scheduled/:id
func (h *ScheduledHandler) GetScheduledAPI(c *gin.Context) {
    id := c.Param("id")
    
    found, err := h.Queries.GetScheduledWebhook(c, id)
    if err != nil {
        if err == sql.ErrNoRows {
            c.JSON(http.StatusNotFound, gin.H{"error": "scheduled webhook not found"})
            return
        }
        c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get scheduled webhook"})
        return
    }

    resp := ScheduledWebhookResponse{
        ID:             found.ID,
        SubscriptionID: found.SubscriptionID,
        Payload:        found.Payload,
        ScheduledFor:   found.ScheduledFor.Format(time.RFC3339),
        Status:         found.Status,
        CreatedAt:      found.CreatedAt.Format(time.RFC3339),
        UpdatedAt:      found.UpdatedAt.Format(time.RFC3339),
    }
    if found.Recurrence.Valid && found.Recurrence.String != "" {
        resp.Recurrence = &found.Recurrence.String
    }

    c.JSON(http.StatusOK, resp)
}

// DeleteScheduledAPI handles DELETE /api/scheduled/:id
func (h *ScheduledHandler) DeleteScheduledAPI(c *gin.Context) {
    id := c.Param("id")
    
    err := h.Queries.DeleteScheduledWebhook(c, id)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete scheduled webhook"})
        return
    }

    c.JSON(http.StatusOK, gin.H{"message": "scheduled webhook deleted"})
}

// CancelScheduledAPI handles PUT /api/scheduled/:id/cancel
func (h *ScheduledHandler) CancelScheduledAPI(c *gin.Context) {
    id := c.Param("id")
    
    err := h.Queries.UpdateScheduledWebhookStatus(c, database.UpdateScheduledWebhookStatusParams{
        Status: "cancelled",
        ID:     id,
    })
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to cancel scheduled webhook"})
        return
    }

    c.JSON(http.StatusOK, gin.H{"message": "scheduled webhook cancelled"})
}
