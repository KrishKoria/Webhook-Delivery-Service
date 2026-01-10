package api

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/cache"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/gin-gonic/gin"
)

// DashboardHandler handles dashboard and global view pages.
type DashboardHandler struct {
	Queries *database.Queries
	Cache   *cache.RedisSubscriptionCache
}

// RegisterDashboardRoutes registers dashboard routes on a gin.Engine.
func RegisterDashboardRoutes(r *gin.Engine, h *DashboardHandler) {
	// Dashboard
	r.GET("/", h.RedirectToDashboard)
	r.GET("/ui", h.RedirectToDashboard)
	r.GET("/ui/dashboard", h.DashboardPage)
	
	// Global views
	r.GET("/ui/backlog", h.BacklogPage)
	r.GET("/ui/dlq", h.GlobalDLQPage)
	r.GET("/ui/scheduled", h.GlobalScheduledPage)
	
	// Scheduled actions
	r.POST("/ui/scheduled/:id/cancel", h.CancelScheduledWebhook)
	r.POST("/ui/scheduled/:id/delete", h.DeleteScheduledWebhook)
}

// RegisterDashboardRoutesGroup registers dashboard routes on a RouterGroup.
func RegisterDashboardRoutesGroup(rg *gin.RouterGroup, h *DashboardHandler) {
	// Dashboard
	rg.GET("/", h.RedirectToDashboard)
	rg.GET("/ui", h.RedirectToDashboard)
	rg.GET("/ui/dashboard", h.DashboardPage)
	
	// Global views
	rg.GET("/ui/backlog", h.BacklogPage)
	rg.GET("/ui/dlq", h.GlobalDLQPage)
	rg.GET("/ui/scheduled", h.GlobalScheduledPage)
	
	// Scheduled actions
	rg.POST("/ui/scheduled/:id/cancel", h.CancelScheduledWebhook)
	rg.POST("/ui/scheduled/:id/delete", h.DeleteScheduledWebhook)
}

// RedirectToDashboard redirects to the dashboard page.
func (h *DashboardHandler) RedirectToDashboard(c *gin.Context) {
	c.Redirect(http.StatusFound, "/ui/dashboard")
}

// HealthStatus represents the system health status.
type HealthStatus struct {
	Status     string
	DatabaseOK bool
	CacheOK    bool
}

// DashboardStats represents all dashboard statistics.
type DashboardStats struct {
	Subscriptions int64
	Queue         QueueMetrics
	Outcomes      OutcomeMetrics
	DLQ           DLQMetrics
	Scheduled     ScheduledMetrics
	Backlog       BacklogStatsResponse
}

// DashboardPage handles GET /ui/dashboard
func (h *DashboardHandler) DashboardPage(c *gin.Context) {
	// Check health
	health := HealthStatus{
		Status:     "healthy",
		DatabaseOK: true,
		CacheOK:    false,
	}
	
	// Test database
	if _, err := h.Queries.ListSubscriptions(c); err != nil {
		health.Status = "unhealthy"
		health.DatabaseOK = false
	}
	
	// Test cache
	if h.Cache != nil {
		if err := h.Cache.Ping(c); err == nil {
			health.CacheOK = true
		}
	}

	stats := DashboardStats{}

	// Get subscription count
	subs, err := h.Queries.ListSubscriptions(c)
	if err == nil {
		stats.Subscriptions = int64(len(subs))
	}

	// Get queue metrics
	queueDepth, err := h.Queries.GetTaskQueueDepth(c)
	if err == nil {
		pending := nullFloatToInt64(queueDepth.Pending)
		delivered := nullFloatToInt64(queueDepth.Delivered)
		failed := nullFloatToInt64(queueDepth.Failed)
		stats.Queue = QueueMetrics{
			Pending:   pending,
			Delivered: delivered,
			Failed:    failed,
			Total:     pending + delivered + failed,
		}
	}

	// Get outcome metrics (last 24 hours)
	deliveryStats, err := h.Queries.GetDeliveryStats(c, sql.NullString{String: "24", Valid: true})
	if err == nil {
		successful := nullFloatToInt64(deliveryStats.Successful)
		failedAttempts := nullFloatToInt64(deliveryStats.FailedAttempts)
		total := successful + failedAttempts
		var successRate float64
		if total > 0 {
			successRate = float64(successful) / float64(total) * 100
		}
		stats.Outcomes = OutcomeMetrics{
			Success:     successful,
			Failure:     failedAttempts,
			Total:       total,
			SuccessRate: successRate,
		}
	}

	// Get DLQ metrics
	dlqStats, err := h.Queries.GetDLQStats(c)
	if err == nil {
		stats.DLQ = DLQMetrics{
			Dead:    nullFloatToInt64(dlqStats.Dead),
			Retried: nullFloatToInt64(dlqStats.Retried),
			Total:   dlqStats.Total,
		}
	}

	// Get scheduled metrics
	scheduledStats, err := h.Queries.CountScheduledWebhooksByStatus(c)
	if err == nil {
		stats.Scheduled = ScheduledMetrics{
			Pending:   nullFloatToInt64(scheduledStats.Pending),
			Delivered: nullFloatToInt64(scheduledStats.Delivered),
			Failed:    nullFloatToInt64(scheduledStats.Failed),
			Cancelled: nullFloatToInt64(scheduledStats.Cancelled),
			Total:     scheduledStats.Total,
		}
	}

	// Get backlog stats
	backlogStats, err := h.Queries.GetBacklogStats(c)
	if err == nil {
		stats.Backlog = BacklogStatsResponse{
			TotalPending:    backlogStats.TotalPending,
			NewTasks:        nullFloatToInt64(backlogStats.NewTasks),
			RetryingTasks:   nullFloatToInt64(backlogStats.RetryingTasks),
			ClaimedTasks:    nullFloatToInt64(backlogStats.ClaimedTasks),
			WaitingForRetry: nullFloatToInt64(backlogStats.WaitingForRetry),
			MaxRetryCount:   interfaceToInt64(backlogStats.MaxRetryCount),
			AvgRetryCount:   backlogStats.AvgRetryCount.Float64,
		}
	}

	c.HTML(http.StatusOK, "dashboard.html", gin.H{
		"Health": health,
		"Stats":  stats,
	})
}

// BacklogPage handles GET /ui/backlog
func (h *DashboardHandler) BacklogPage(c *gin.Context) {
	// Get backlog stats
	stats := BacklogStatsResponse{}
	backlogStats, err := h.Queries.GetBacklogStats(c)
	if err == nil {
		stats = BacklogStatsResponse{
			TotalPending:    backlogStats.TotalPending,
			NewTasks:        nullFloatToInt64(backlogStats.NewTasks),
			RetryingTasks:   nullFloatToInt64(backlogStats.RetryingTasks),
			ClaimedTasks:    nullFloatToInt64(backlogStats.ClaimedTasks),
			WaitingForRetry: nullFloatToInt64(backlogStats.WaitingForRetry),
			MaxRetryCount:   interfaceToInt64(backlogStats.MaxRetryCount),
			AvgRetryCount:   backlogStats.AvgRetryCount.Float64,
		}
	}

	// Get backlog by subscription
	bySubscription, _ := h.Queries.GetBacklogBySubscription(c, 20)
	bySubResponse := make([]BacklogBySubscriptionResponse, len(bySubscription))
	for i, bs := range bySubscription {
		bySubResponse[i] = BacklogBySubscriptionResponse{
			SubscriptionID:  bs.SubscriptionID,
			SubscriptionURL: bs.SubscriptionUrl,
			PendingCount:    bs.PendingCount,
			RetryingCount:   nullFloatToInt64(bs.RetryingCount),
			MaxRetries:      interfaceToInt64(bs.MaxRetries),
			OldestTask:      interfaceToTime(bs.OldestTask),
		}
	}

	// Get retry distribution
	retryDist, _ := h.Queries.GetRetryDistribution(c)
	retryDistResponse := make([]RetryDistributionResponse, len(retryDist))
	for i, rd := range retryDist {
		retryDistResponse[i] = RetryDistributionResponse{
			RetryCount: rd.RetryCount,
			TaskCount:  rd.TaskCount,
		}
	}

	// Get active tasks
	activeTasks, _ := h.Queries.ListActiveClaimedTasks(c, 20)
	activeResponse := make([]BacklogTaskResponse, len(activeTasks))
	for i, task := range activeTasks {
		activeResponse[i] = BacklogTaskResponse{
			ID:              task.ID,
			SubscriptionID:  task.SubscriptionID,
			SubscriptionURL: task.SubscriptionUrl,
			Status:          task.Status,
			AttemptCount:    task.AttemptCount,
			CreatedAt:       task.CreatedAt,
		}
		if task.ClaimedBy.Valid {
			activeResponse[i].ClaimedBy = &task.ClaimedBy.String
		}
		if task.LeaseUntil.Valid {
			activeResponse[i].LeaseUntil = &task.LeaseUntil.Time
		}
	}

	// Get retrying tasks
	retryingTasks, _ := h.Queries.ListRetryingTasks(c, 20)
	retryingResponse := make([]BacklogTaskResponse, len(retryingTasks))
	for i, task := range retryingTasks {
		retryingResponse[i] = BacklogTaskResponse{
			ID:              task.ID,
			SubscriptionID:  task.SubscriptionID,
			SubscriptionURL: task.SubscriptionUrl,
			Status:          task.Status,
			AttemptCount:    task.AttemptCount,
			CreatedAt:       task.CreatedAt,
		}
		if task.NextAttemptAt.Valid {
			retryingResponse[i].NextAttemptAt = &task.NextAttemptAt.Time
		}
	}

	c.HTML(http.StatusOK, "backlog.html", gin.H{
		"Stats":             stats,
		"BySubscription":    bySubResponse,
		"RetryDistribution": retryDistResponse,
		"ActiveTasks":       activeResponse,
		"RetryingTasks":     retryingResponse,
	})
}

// GlobalDLQPage handles GET /ui/dlq
func (h *DashboardHandler) GlobalDLQPage(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	limit := 50
	offset := (page - 1) * limit

	// Get DLQ stats
	stats := DLQMetrics{}
	dlqStats, err := h.Queries.GetDLQStats(c)
	if err == nil {
		stats = DLQMetrics{
			Dead:    nullFloatToInt64(dlqStats.Dead),
			Retried: nullFloatToInt64(dlqStats.Retried),
			Total:   dlqStats.Total,
		}
	}

	// Get all DLQ tasks
	tasks, _ := h.Queries.ListAllDeadLetterTasks(c, database.ListAllDeadLetterTasksParams{
		Limit:  int64(limit + 1),
		Offset: int64(offset),
	})

	hasMore := len(tasks) > limit
	if hasMore {
		tasks = tasks[:limit]
	}

	c.HTML(http.StatusOK, "dlq_global.html", gin.H{
		"Stats":   stats,
		"Tasks":   tasks,
		"Page":    page,
		"HasMore": hasMore,
	})
}

// GlobalScheduledPage handles GET /ui/scheduled
func (h *DashboardHandler) GlobalScheduledPage(c *gin.Context) {
	// Get scheduled stats
	stats := ScheduledMetrics{}
	scheduledStats, err := h.Queries.CountScheduledWebhooksByStatus(c)
	if err == nil {
		stats = ScheduledMetrics{
			Pending:   nullFloatToInt64(scheduledStats.Pending),
			Delivered: nullFloatToInt64(scheduledStats.Delivered),
			Failed:    nullFloatToInt64(scheduledStats.Failed),
			Cancelled: nullFloatToInt64(scheduledStats.Cancelled),
			Total:     scheduledStats.Total,
		}
	}

	// Get all scheduled webhooks - we need to list from all subscriptions
	// For now, get from each subscription
	subs, _ := h.Queries.ListSubscriptions(c)
	var allScheduled []database.ScheduledWebhook
	for _, sub := range subs {
		scheduled, _ := h.Queries.ListScheduledWebhooks(c, database.ListScheduledWebhooksParams{
			SubscriptionID: sub.ID,
			Limit:          100,
			Offset:         0,
		})
		allScheduled = append(allScheduled, scheduled...)
	}

	c.HTML(http.StatusOK, "scheduled_global.html", gin.H{
		"Stats":             stats,
		"ScheduledWebhooks": allScheduled,
	})
}

// CancelScheduledWebhook handles POST /ui/scheduled/:id/cancel
func (h *DashboardHandler) CancelScheduledWebhook(c *gin.Context) {
	id := c.Param("id")
	
	// Get the scheduled webhook first
	scheduled, err := h.Queries.GetScheduledWebhook(c, id)
	if err != nil {
		c.String(http.StatusNotFound, "Scheduled webhook not found")
		return
	}
	
	if scheduled.Status != "pending" {
		c.String(http.StatusBadRequest, "Can only cancel pending webhooks")
		return
	}
	
	// Update status to cancelled
	err = h.Queries.UpdateScheduledWebhookStatus(c, database.UpdateScheduledWebhookStatusParams{
		Status: "cancelled",
		ID:     id,
	})
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to cancel: %v", err)
		return
	}
	
	// Redirect back
	referer := c.Request.Referer()
	if referer == "" {
		referer = "/ui/scheduled"
	}
	c.Redirect(http.StatusSeeOther, referer)
}

// DeleteScheduledWebhook handles POST /ui/scheduled/:id/delete
func (h *DashboardHandler) DeleteScheduledWebhook(c *gin.Context) {
	id := c.Param("id")
	
	err := h.Queries.DeleteScheduledWebhook(c, id)
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to delete: %v", err)
		return
	}
	
	// Redirect back
	referer := c.Request.Referer()
	if referer == "" {
		referer = "/ui/scheduled"
	}
	c.Redirect(http.StatusSeeOther, referer)
}
