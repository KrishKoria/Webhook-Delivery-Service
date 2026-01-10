package api

import (
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/gin-gonic/gin"
)

// BacklogHandler handles backlog and retry visibility endpoints.
type BacklogHandler struct {
	Queries *database.Queries
}

// BacklogTaskResponse represents a task in the backlog.
type BacklogTaskResponse struct {
	ID              string     `json:"id"`
	SubscriptionID  string     `json:"subscription_id"`
	SubscriptionURL string     `json:"subscription_url"`
	Status          string     `json:"status"`
	AttemptCount    int64      `json:"attempt_count"`
	CreatedAt       time.Time  `json:"created_at"`
	LastAttemptAt   *time.Time `json:"last_attempt_at,omitempty"`
	NextAttemptAt   *time.Time `json:"next_attempt_at,omitempty"`
	ClaimedBy       *string    `json:"claimed_by,omitempty"`
	LeaseUntil      *time.Time `json:"lease_until,omitempty"`
}

// BacklogStatsResponse represents backlog statistics.
type BacklogStatsResponse struct {
	TotalPending    int64   `json:"total_pending"`
	NewTasks        int64   `json:"new_tasks"`
	RetryingTasks   int64   `json:"retrying_tasks"`
	ClaimedTasks    int64   `json:"claimed_tasks"`
	WaitingForRetry int64   `json:"waiting_for_retry"`
	MaxRetryCount   int64   `json:"max_retry_count"`
	AvgRetryCount   float64 `json:"avg_retry_count"`
}

// BacklogBySubscriptionResponse represents backlog grouped by subscription.
type BacklogBySubscriptionResponse struct {
	SubscriptionID  string     `json:"subscription_id"`
	SubscriptionURL string     `json:"subscription_url"`
	PendingCount    int64      `json:"pending_count"`
	RetryingCount   int64      `json:"retrying_count"`
	MaxRetries      int64      `json:"max_retries"`
	OldestTask      *time.Time `json:"oldest_task,omitempty"`
}

// RetryDistributionResponse represents the distribution of retry counts.
type RetryDistributionResponse struct {
	RetryCount int64 `json:"retry_count"`
	TaskCount  int64 `json:"task_count"`
}

// BacklogOverviewResponse represents the full backlog overview.
type BacklogOverviewResponse struct {
	Timestamp        string                          `json:"timestamp"`
	Stats            BacklogStatsResponse            `json:"stats"`
	BySubscription   []BacklogBySubscriptionResponse `json:"by_subscription"`
	RetryDistribution []RetryDistributionResponse    `json:"retry_distribution"`
}

// RegisterBacklogRoutes registers backlog endpoints on a gin.Engine.
func RegisterBacklogRoutes(r *gin.Engine, h *BacklogHandler) {
	r.GET("/api/backlog", h.GetBacklogOverview)
	r.GET("/api/backlog/stats", h.GetBacklogStats)
	r.GET("/api/backlog/tasks", h.ListBacklogTasks)
	r.GET("/api/backlog/retrying", h.ListRetryingTasks)
	r.GET("/api/backlog/active", h.ListActiveClaimedTasks)
	r.GET("/api/backlog/by-subscription", h.GetBacklogBySubscription)
	r.GET("/api/backlog/retry-distribution", h.GetRetryDistribution)
}

// RegisterBacklogRoutesGroup registers backlog endpoints on a RouterGroup.
func RegisterBacklogRoutesGroup(rg *gin.RouterGroup, h *BacklogHandler) {
	rg.GET("/api/backlog", h.GetBacklogOverview)
	rg.GET("/api/backlog/stats", h.GetBacklogStats)
	rg.GET("/api/backlog/tasks", h.ListBacklogTasks)
	rg.GET("/api/backlog/retrying", h.ListRetryingTasks)
	rg.GET("/api/backlog/active", h.ListActiveClaimedTasks)
	rg.GET("/api/backlog/by-subscription", h.GetBacklogBySubscription)
	rg.GET("/api/backlog/retry-distribution", h.GetRetryDistribution)
}

// Helper function to convert interface{} to int64
func interfaceToInt64(v interface{}) int64 {
	if v == nil {
		return 0
	}
	switch val := v.(type) {
	case int64:
		return val
	case int:
		return int64(val)
	case float64:
		return int64(val)
	case string:
		if i, err := strconv.ParseInt(val, 10, 64); err == nil {
			return i
		}
	}
	return 0
}

// Helper to convert interface{} to time.Time pointer
func interfaceToTime(v interface{}) *time.Time {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case time.Time:
		return &val
	case string:
		if t, err := time.Parse(time.RFC3339, val); err == nil {
			return &t
		}
		// Try SQLite datetime format
		if t, err := time.Parse("2006-01-02 15:04:05", val); err == nil {
			return &t
		}
	}
	return nil
}

// GetBacklogOverview returns comprehensive backlog information.
func (h *BacklogHandler) GetBacklogOverview(c *gin.Context) {
	resp := BacklogOverviewResponse{
		Timestamp: time.Now().Format(time.RFC3339),
	}

	// Get backlog stats
	stats, err := h.Queries.GetBacklogStats(c)
	if err == nil {
		resp.Stats = BacklogStatsResponse{
			TotalPending:    stats.TotalPending,
			NewTasks:        nullFloatToInt64(stats.NewTasks),
			RetryingTasks:   nullFloatToInt64(stats.RetryingTasks),
			ClaimedTasks:    nullFloatToInt64(stats.ClaimedTasks),
			WaitingForRetry: nullFloatToInt64(stats.WaitingForRetry),
			MaxRetryCount:   interfaceToInt64(stats.MaxRetryCount),
			AvgRetryCount:   stats.AvgRetryCount.Float64,
		}
	}

	// Get backlog by subscription (top 10)
	bySubscription, err := h.Queries.GetBacklogBySubscription(c, 10)
	if err == nil {
		resp.BySubscription = make([]BacklogBySubscriptionResponse, len(bySubscription))
		for i, bs := range bySubscription {
			resp.BySubscription[i] = BacklogBySubscriptionResponse{
				SubscriptionID:  bs.SubscriptionID,
				SubscriptionURL: bs.SubscriptionUrl,
				PendingCount:    bs.PendingCount,
				RetryingCount:   nullFloatToInt64(bs.RetryingCount),
				MaxRetries:      interfaceToInt64(bs.MaxRetries),
				OldestTask:      interfaceToTime(bs.OldestTask),
			}
		}
	}

	// Get retry distribution
	retryDist, err := h.Queries.GetRetryDistribution(c)
	if err == nil {
		resp.RetryDistribution = make([]RetryDistributionResponse, len(retryDist))
		for i, rd := range retryDist {
			resp.RetryDistribution[i] = RetryDistributionResponse{
				RetryCount: rd.RetryCount,
				TaskCount:  rd.TaskCount,
			}
		}
	}

	c.JSON(http.StatusOK, resp)
}

// GetBacklogStats returns backlog statistics.
func (h *BacklogHandler) GetBacklogStats(c *gin.Context) {
	stats, err := h.Queries.GetBacklogStats(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get backlog stats"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"timestamp": time.Now().Format(time.RFC3339),
		"stats": BacklogStatsResponse{
			TotalPending:    stats.TotalPending,
			NewTasks:        nullFloatToInt64(stats.NewTasks),
			RetryingTasks:   nullFloatToInt64(stats.RetryingTasks),
			ClaimedTasks:    nullFloatToInt64(stats.ClaimedTasks),
			WaitingForRetry: nullFloatToInt64(stats.WaitingForRetry),
			MaxRetryCount:   interfaceToInt64(stats.MaxRetryCount),
			AvgRetryCount:   stats.AvgRetryCount.Float64,
		},
	})
}

// ListBacklogTasks returns tasks in the backlog.
func (h *BacklogHandler) ListBacklogTasks(c *gin.Context) {
	limit := int64(50)
	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.ParseInt(limitStr, 10, 64); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	tasks, err := h.Queries.ListBacklogTasks(c, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list backlog tasks"})
		return
	}

	response := make([]BacklogTaskResponse, len(tasks))
	for i, task := range tasks {
		response[i] = BacklogTaskResponse{
			ID:              task.ID,
			SubscriptionID:  task.SubscriptionID,
			SubscriptionURL: task.SubscriptionUrl,
			Status:          task.Status,
			AttemptCount:    task.AttemptCount,
			CreatedAt:       task.CreatedAt,
		}
		if task.LastAttemptAt.Valid {
			response[i].LastAttemptAt = &task.LastAttemptAt.Time
		}
		if task.NextAttemptAt.Valid {
			response[i].NextAttemptAt = &task.NextAttemptAt.Time
		}
		if task.ClaimedBy.Valid {
			response[i].ClaimedBy = &task.ClaimedBy.String
		}
		if task.LeaseUntil.Valid {
			response[i].LeaseUntil = &task.LeaseUntil.Time
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"timestamp": time.Now().Format(time.RFC3339),
		"count":     len(response),
		"tasks":     response,
	})
}

// ListRetryingTasks returns tasks that are being retried.
func (h *BacklogHandler) ListRetryingTasks(c *gin.Context) {
	limit := int64(50)
	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.ParseInt(limitStr, 10, 64); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	tasks, err := h.Queries.ListRetryingTasks(c, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list retrying tasks"})
		return
	}

	response := make([]BacklogTaskResponse, len(tasks))
	for i, task := range tasks {
		response[i] = BacklogTaskResponse{
			ID:              task.ID,
			SubscriptionID:  task.SubscriptionID,
			SubscriptionURL: task.SubscriptionUrl,
			Status:          task.Status,
			AttemptCount:    task.AttemptCount,
			CreatedAt:       task.CreatedAt,
		}
		if task.LastAttemptAt.Valid {
			response[i].LastAttemptAt = &task.LastAttemptAt.Time
		}
		if task.NextAttemptAt.Valid {
			response[i].NextAttemptAt = &task.NextAttemptAt.Time
		}
		if task.ClaimedBy.Valid {
			response[i].ClaimedBy = &task.ClaimedBy.String
		}
		if task.LeaseUntil.Valid {
			response[i].LeaseUntil = &task.LeaseUntil.Time
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"timestamp": time.Now().Format(time.RFC3339),
		"count":     len(response),
		"tasks":     response,
	})
}

// ListActiveClaimedTasks returns tasks currently being processed by workers.
func (h *BacklogHandler) ListActiveClaimedTasks(c *gin.Context) {
	limit := int64(50)
	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.ParseInt(limitStr, 10, 64); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	tasks, err := h.Queries.ListActiveClaimedTasks(c, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list active tasks"})
		return
	}

	response := make([]BacklogTaskResponse, len(tasks))
	for i, task := range tasks {
		response[i] = BacklogTaskResponse{
			ID:              task.ID,
			SubscriptionID:  task.SubscriptionID,
			SubscriptionURL: task.SubscriptionUrl,
			Status:          task.Status,
			AttemptCount:    task.AttemptCount,
			CreatedAt:       task.CreatedAt,
		}
		if task.LastAttemptAt.Valid {
			response[i].LastAttemptAt = &task.LastAttemptAt.Time
		}
		if task.NextAttemptAt.Valid {
			response[i].NextAttemptAt = &task.NextAttemptAt.Time
		}
		if task.ClaimedBy.Valid {
			response[i].ClaimedBy = &task.ClaimedBy.String
		}
		if task.LeaseUntil.Valid {
			response[i].LeaseUntil = &task.LeaseUntil.Time
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"timestamp": time.Now().Format(time.RFC3339),
		"count":     len(response),
		"tasks":     response,
	})
}

// GetBacklogBySubscription returns backlog grouped by subscription.
func (h *BacklogHandler) GetBacklogBySubscription(c *gin.Context) {
	limit := int64(50)
	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.ParseInt(limitStr, 10, 64); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	bySubscription, err := h.Queries.GetBacklogBySubscription(c, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get backlog by subscription"})
		return
	}

	response := make([]BacklogBySubscriptionResponse, len(bySubscription))
	for i, bs := range bySubscription {
		response[i] = BacklogBySubscriptionResponse{
			SubscriptionID:  bs.SubscriptionID,
			SubscriptionURL: bs.SubscriptionUrl,
			PendingCount:    bs.PendingCount,
			RetryingCount:   nullFloatToInt64(bs.RetryingCount),
			MaxRetries:      interfaceToInt64(bs.MaxRetries),
			OldestTask:      interfaceToTime(bs.OldestTask),
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"timestamp":       time.Now().Format(time.RFC3339),
		"count":           len(response),
		"by_subscription": response,
	})
}

// GetRetryDistribution returns the distribution of retry counts.
func (h *BacklogHandler) GetRetryDistribution(c *gin.Context) {
	retryDist, err := h.Queries.GetRetryDistribution(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get retry distribution"})
		return
	}

	response := make([]RetryDistributionResponse, len(retryDist))
	for i, rd := range retryDist {
		response[i] = RetryDistributionResponse{
			RetryCount: rd.RetryCount,
			TaskCount:  rd.TaskCount,
		}
	}

	// Calculate total retrying tasks
	var totalRetrying int64
	for _, rd := range response {
		totalRetrying += rd.TaskCount
	}

	c.JSON(http.StatusOK, gin.H{
		"timestamp":         time.Now().Format(time.RFC3339),
		"total_retrying":    totalRetrying,
		"distribution":      response,
	})
}

// Helper function to safely convert sql.NullFloat64 to int64
// Note: This is already defined in metrics.go, but we need it here too
// for standalone compilation. In production, consider moving to a common package.
func nullFloatToInt64BacklogHelper(nf sql.NullFloat64) int64 {
	if nf.Valid {
		return int64(nf.Float64)
	}
	return 0
}

// Use the helper from metrics.go since both are in the same package
// The nullFloatToInt64 function is already available
