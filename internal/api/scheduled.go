package api

import (
	"database/sql"
	"log"
	"net/http"
	"time"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type ScheduledHandler struct {
    Queries *database.Queries
}

func RegisterScheduledRoutes(r *gin.Engine, h *ScheduledHandler) {
    r.POST("/scheduled", h.CreateScheduled)
    r.GET("/scheduled", h.ListScheduled)
    r.DELETE("/scheduled/:id", h.DeleteScheduled)
}

type CreateScheduledRequest struct {
    SubscriptionID string    `json:"subscription_id"`
    Payload        string    `json:"payload"`
    ScheduledFor   time.Time `json:"scheduled_for"`
    Recurrence     string    `json:"recurrence"` 
}

func (h *ScheduledHandler) CreateScheduled(c *gin.Context) {
    subscriptionID := c.PostForm("subscription_id")
    payload := c.PostForm("payload")
    scheduledForStr := c.PostForm("scheduled_for")
    recurrence := c.PostForm("recurrence")

    if subscriptionID == "" || payload == "" || scheduledForStr == "" {
        c.JSON(http.StatusBadRequest, gin.H{"error": "Missing required form fields"})
        return
    }

    const layout = "2006-01-02T15:04" 
    scheduledFor, err := time.Parse(layout, scheduledForStr)
    if err != nil {
        log.Printf("Error parsing scheduled_for time '%s': %v", scheduledForStr, err)
        c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid scheduled_for date format. Use YYYY-MM-DDTHH:MM."})
        return
    }

    id := uuid.New().String()
    err = h.Queries.CreateScheduledWebhook(c, database.CreateScheduledWebhookParams{
        ID:             id,
        SubscriptionID: subscriptionID,
        Payload:        payload,
        ScheduledFor:   scheduledFor, 
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

func (h *ScheduledHandler) DeleteScheduled(c *gin.Context) {
    id := c.Param("id")
    err := h.Queries.DeleteScheduledWebhook(c, id)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }
    c.Status(http.StatusNoContent)
}