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
    // UI routes
    r.GET("/ui/subscriptions/:id/dlq", dlqHandler.ListDLQ)
    r.POST("/ui/dlq/:dlq_id/retry", dlqHandler.RetryDLQTask)
    r.POST("/ui/dlq/:dlq_id/delete", dlqHandler.DeleteDLQTask)
    
    // API routes
    r.GET("/api/subscriptions/:id/dlq", dlqHandler.ListDLQAPI)
    r.GET("/api/dlq/:dlq_id", dlqHandler.GetDLQTaskAPI)
    r.POST("/api/dlq/:dlq_id/retry", dlqHandler.RetryDLQTaskAPI)
    r.DELETE("/api/dlq/:dlq_id", dlqHandler.DeleteDLQTaskAPI)
    r.POST("/api/dlq/bulk-retry", dlqHandler.BulkRetryDLQAPI)
}

// RegisterDLQRoutesGroup registers DLQ routes on a RouterGroup (for middleware).
func RegisterDLQRoutesGroup(rg *gin.RouterGroup, dlqHandler *DLQHandler) {
    // UI routes
    rg.GET("/ui/subscriptions/:id/dlq", dlqHandler.ListDLQ)
    rg.POST("/ui/dlq/:dlq_id/retry", dlqHandler.RetryDLQTask)
    rg.POST("/ui/dlq/:dlq_id/delete", dlqHandler.DeleteDLQTask)
    
    // API routes
    rg.GET("/api/subscriptions/:id/dlq", dlqHandler.ListDLQAPI)
    rg.GET("/api/dlq/:dlq_id", dlqHandler.GetDLQTaskAPI)
    rg.POST("/api/dlq/:dlq_id/retry", dlqHandler.RetryDLQTaskAPI)
    rg.DELETE("/api/dlq/:dlq_id", dlqHandler.DeleteDLQTaskAPI)
    rg.POST("/api/dlq/bulk-retry", dlqHandler.BulkRetryDLQAPI)
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

// --- API Handlers ---

// DLQTaskResponse is the JSON response for a DLQ task.
type DLQTaskResponse struct {
    ID             string    `json:"id"`
    SubscriptionID string    `json:"subscription_id"`
    Payload        string    `json:"payload"`
    Reason         string    `json:"reason,omitempty"`
    AttemptCount   int64     `json:"attempt_count"`
    Status         string    `json:"status"`
    LastAttemptAt  *string   `json:"last_attempt_at,omitempty"`
    FailedAt       string    `json:"failed_at"`
}

// DLQListResponse is the JSON response for listing DLQ tasks.
type DLQListResponse struct {
    Tasks      []DLQTaskResponse `json:"tasks"`
    Page       int               `json:"page"`
    Limit      int               `json:"limit"`
    Total      int64             `json:"total,omitempty"`
    HasMore    bool              `json:"has_more"`
}

// BulkRetryRequest is the JSON request for bulk retry.
type BulkRetryRequest struct {
    DLQIDs []string `json:"dlq_ids" binding:"required,min=1,max=100"`
}

// BulkRetryResponse is the JSON response for bulk retry.
type BulkRetryResponse struct {
    Retried    int      `json:"retried"`
    Failed     int      `json:"failed"`
    FailedIDs  []string `json:"failed_ids,omitempty"`
}

// ListDLQAPI returns DLQ tasks for a subscription as JSON.
func (h *DLQHandler) ListDLQAPI(c *gin.Context) {
    subID := c.Param("id")
    page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
    limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
    if page < 1 {
        page = 1
    }
    if limit < 1 || limit > 100 {
        limit = 20
    }
    offset := (page - 1) * limit

    tasks, err := h.Queries.ListDeadLetterTasksForSubscription(c, database.ListDeadLetterTasksForSubscriptionParams{
        SubscriptionID: subID,
        Limit:          int64(limit + 1), // fetch one extra to check if there are more
        Offset:         int64(offset),
    })
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list DLQ tasks", "details": err.Error()})
        return
    }

    hasMore := len(tasks) > limit
    if hasMore {
        tasks = tasks[:limit]
    }

    resp := DLQListResponse{
        Tasks:   make([]DLQTaskResponse, 0, len(tasks)),
        Page:    page,
        Limit:   limit,
        HasMore: hasMore,
    }

    for _, t := range tasks {
        tr := DLQTaskResponse{
            ID:             t.ID,
            SubscriptionID: t.SubscriptionID,
            Payload:        t.Payload,
            Reason:         t.Reason,
            AttemptCount:   t.AttemptCount,
            Status:         t.Status,
            FailedAt:       t.FailedAt.Format(time.RFC3339),
        }
        if t.LastAttemptAt.Valid {
            formatted := t.LastAttemptAt.Time.Format(time.RFC3339)
            tr.LastAttemptAt = &formatted
        }
        resp.Tasks = append(resp.Tasks, tr)
    }

    c.JSON(http.StatusOK, resp)
}

// GetDLQTaskAPI returns a single DLQ task as JSON.
func (h *DLQHandler) GetDLQTaskAPI(c *gin.Context) {
    id := c.Param("dlq_id")
    task, err := h.Queries.GetDeadLetterTask(c, id)
    if err != nil {
        if err == sql.ErrNoRows {
            c.JSON(http.StatusNotFound, gin.H{"error": "DLQ task not found"})
            return
        }
        c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get DLQ task", "details": err.Error()})
        return
    }

    tr := DLQTaskResponse{
        ID:             task.ID,
        SubscriptionID: task.SubscriptionID,
        Payload:        task.Payload,
        Reason:         task.Reason,
        AttemptCount:   task.AttemptCount,
        Status:         task.Status,
        FailedAt:       task.FailedAt.Format(time.RFC3339),
    }
    if task.LastAttemptAt.Valid {
        formatted := task.LastAttemptAt.Time.Format(time.RFC3339)
        tr.LastAttemptAt = &formatted
    }

    c.JSON(http.StatusOK, tr)
}

// RetryDLQTaskAPI retries a single DLQ task via API.
func (h *DLQHandler) RetryDLQTaskAPI(c *gin.Context) {
    id := c.Param("dlq_id")
    task, err := h.Queries.GetDeadLetterTask(c, id)
    if err != nil {
        if err == sql.ErrNoRows {
            c.JSON(http.StatusNotFound, gin.H{"error": "DLQ task not found"})
            return
        }
        c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get DLQ task", "details": err.Error()})
        return
    }

    if task.Status == "retried" {
        c.JSON(http.StatusConflict, gin.H{"error": "task has already been retried"})
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
        c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to requeue task", "details": err.Error()})
        return
    }

    // Mark DLQ as retried
    err = h.Queries.UpdateDeadLetterTaskStatus(c, database.UpdateDeadLetterTaskStatusParams{
        Status:        "retried",
        ErrorDetails:  sql.NullString{String: "Retried via API", Valid: true},
        ID:            id,
    })
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update DLQ status", "details": err.Error()})
        return
    }

    c.JSON(http.StatusOK, gin.H{
        "message":     "task requeued successfully",
        "new_task_id": newTaskID,
    })
}

// DeleteDLQTaskAPI deletes a DLQ task via API.
func (h *DLQHandler) DeleteDLQTaskAPI(c *gin.Context) {
    id := c.Param("dlq_id")

    // Check if task exists first
    _, err := h.Queries.GetDeadLetterTask(c, id)
    if err != nil {
        if err == sql.ErrNoRows {
            c.JSON(http.StatusNotFound, gin.H{"error": "DLQ task not found"})
            return
        }
        c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get DLQ task", "details": err.Error()})
        return
    }

    err = h.Queries.DeleteDeadLetterTask(c, id)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete DLQ task", "details": err.Error()})
        return
    }

    c.JSON(http.StatusOK, gin.H{"message": "task deleted successfully"})
}

// BulkRetryDLQAPI retries multiple DLQ tasks at once.
func (h *DLQHandler) BulkRetryDLQAPI(c *gin.Context) {
    var req BulkRetryRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request", "details": err.Error()})
        return
    }

    var retried, failed int
    var failedIDs []string

    for _, id := range req.DLQIDs {
        task, err := h.Queries.GetDeadLetterTask(c, id)
        if err != nil {
            failed++
            failedIDs = append(failedIDs, id)
            continue
        }

        if task.Status == "retried" {
            failed++
            failedIDs = append(failedIDs, id)
            continue
        }

        // Requeue as a delivery task
        newTaskID := uuid.New().String()
        err = h.Queries.CreateDeliveryTask(c, database.CreateDeliveryTaskParams{
            ID:             newTaskID,
            SubscriptionID: task.SubscriptionID,
            Payload:        task.Payload,
        })
        if err != nil {
            failed++
            failedIDs = append(failedIDs, id)
            continue
        }

        // Mark DLQ as retried
        _ = h.Queries.UpdateDeadLetterTaskStatus(c, database.UpdateDeadLetterTaskStatusParams{
            Status:        "retried",
            ErrorDetails:  sql.NullString{String: "Retried via bulk API", Valid: true},
            ID:            id,
        })

        retried++
    }

    resp := BulkRetryResponse{
        Retried:   retried,
        Failed:    failed,
        FailedIDs: failedIDs,
    }

    if retried == 0 && failed > 0 {
        c.JSON(http.StatusUnprocessableEntity, resp)
        return
    }

    c.JSON(http.StatusOK, resp)
}