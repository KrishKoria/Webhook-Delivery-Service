package api

import (
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type DLQHandler struct {
    Queries *database.Queries
}

func RegisterDLQRoutes(r *gin.Engine, dlqHandler *DLQHandler) {
    r.GET("/ui/subscriptions/:id/dlq", dlqHandler.ListDLQ)
    r.POST("/ui/dlq/:dlq_id/retry", dlqHandler.RetryDLQTask)
    r.POST("/ui/dlq/:dlq_id/delete", dlqHandler.DeleteDLQTask)
}


// List DLQ entries for a subscription (UI)
func (h *DLQHandler) ListDLQ(c *gin.Context) {
    subID := c.Param("id")
    page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
    limit := 20
    offset := (page - 1) * limit

    tasks, err := h.Queries.ListDeadLetterTasksForSubscription(c, database.ListDeadLetterTasksForSubscriptionParams{
        SubscriptionID: subID,
        Limit:          int64(limit),
        Offset:         int64(offset),
    })
    if err != nil {
        c.String(http.StatusInternalServerError, "Error: %v", err)
        return
    }
    c.HTML(http.StatusOK, "dlq.html", gin.H{
        "Tasks": tasks,
        "SubscriptionID": subID,
    })
}

// Retry a DLQ task (requeue as a delivery task)
func (h *DLQHandler) RetryDLQTask(c *gin.Context) {
    id := c.Param("dlq_id")
    task, err := h.Queries.GetDeadLetterTask(c, id)
    if err != nil {
        c.String(http.StatusNotFound, "DLQ task not found")
        return
    }
    // Requeue as a delivery task
    newTaskID := uuid.New().String()
    err = h.Queries.CreateDeliveryTask(c, database.CreateDeliveryTaskParams{
        ID:             newTaskID,
        SubscriptionID: task.SubscriptionID,
        Payload:        task.Payload,
    })
    if err != nil {
        c.String(http.StatusInternalServerError, "Failed to requeue: %v", err)
        return
    }
    // Mark DLQ as retried
    _ = h.Queries.UpdateDeadLetterTaskStatus(c, database.UpdateDeadLetterTaskStatusParams{
        Status:        "retried",
        LastAttemptAt: sql.NullTime{Time: time.Now(), Valid: true},
        ErrorDetails:  sql.NullString{String: "Retried via UI", Valid: true},
        ID:            id,
    })
    c.Redirect(http.StatusSeeOther, c.Request.Referer())
}

// Delete a DLQ task
func (h *DLQHandler) DeleteDLQTask(c *gin.Context) {
    id := c.Param("dlq_id")
    err := h.Queries.DeleteDeadLetterTask(c, id)
    if err != nil {
        c.String(http.StatusInternalServerError, "Failed to delete: %v", err)
        return
    }
    c.Redirect(http.StatusSeeOther, c.Request.Referer())
}