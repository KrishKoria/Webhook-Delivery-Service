package api

import (
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/gin-gonic/gin"
)

// BacklogHandler serves backlog and retry visibility. The active backlog is
// every unsettled task: status pending or in_flight.
type BacklogHandler struct {
	Queries *database.Queries
}

// BacklogTaskResponse is a task in the backlog.
type BacklogTaskResponse struct {
	ID              string     `json:"id"`
	SubscriptionID  string     `json:"subscription_id"`
	SubscriptionURL string     `json:"subscription_url"`
	Status          string     `json:"status"`
	EventType       *string    `json:"event_type,omitempty"`
	AttemptCount    int64      `json:"attempt_count"`
	CreatedAt       time.Time  `json:"created_at"`
	LastAttemptAt   *time.Time `json:"last_attempt_at,omitempty"`
	NextAttemptAt   *time.Time `json:"next_attempt_at,omitempty"`
	ClaimedBy       *string    `json:"claimed_by,omitempty"`
	LeaseUntil      *time.Time `json:"lease_until,omitempty"`
}

// BacklogStatsResponse summarizes the active backlog. TotalPending counts
// pending + in_flight; ClaimedTasks is the in_flight subset.
type BacklogStatsResponse struct {
	TotalPending    int64   `json:"total_active"`
	Pending         int64   `json:"pending"`
	ClaimedTasks    int64   `json:"in_flight"`
	NewTasks        int64   `json:"new_tasks"`
	RetryingTasks   int64   `json:"retrying_tasks"`
	WaitingForRetry int64   `json:"waiting_for_retry"`
	MaxRetryCount   int64   `json:"max_attempts"`
	AvgRetryCount   float64 `json:"avg_attempts"`
}

// BacklogBySubscriptionResponse is the active backlog for one subscription.
type BacklogBySubscriptionResponse struct {
	SubscriptionID  string     `json:"subscription_id"`
	SubscriptionURL string     `json:"subscription_url"`
	PendingCount    int64      `json:"active_count"`
	InFlightCount   int64      `json:"in_flight_count"`
	RetryingCount   int64      `json:"retrying_count"`
	MaxRetries      int64      `json:"max_attempts"`
	OldestTask      *time.Time `json:"oldest_task,omitempty"`
}

// RetryDistributionResponse is how many active tasks have a given attempt count.
type RetryDistributionResponse struct {
	RetryCount int64 `json:"attempt_count"`
	TaskCount  int64 `json:"task_count"`
}

// BacklogOverviewResponse is the payload of GET /api/backlog. Degraded is true
// when a section failed to load and was left empty.
type BacklogOverviewResponse struct {
	Timestamp         string                          `json:"timestamp"`
	Degraded          bool                            `json:"degraded"`
	Stats             *BacklogStatsResponse           `json:"stats"`
	BySubscription    []BacklogBySubscriptionResponse `json:"by_subscription"`
	RetryDistribution []RetryDistributionResponse     `json:"retry_distribution"`
}

// RegisterBacklogRoutesGroup registers the backlog endpoints.
func RegisterBacklogRoutesGroup(rg *gin.RouterGroup, h *BacklogHandler) {
	rg.GET("/api/backlog", h.GetBacklogOverview)
	rg.GET("/api/backlog/stats", h.GetBacklogStats)
	rg.GET("/api/backlog/tasks", h.ListBacklogTasks)
	rg.GET("/api/backlog/retrying", h.ListRetryingTasks)
	rg.GET("/api/backlog/active", h.ListActiveClaimedTasks)
	rg.GET("/api/backlog/by-subscription", h.GetBacklogBySubscription)
	rg.GET("/api/backlog/retry-distribution", h.GetRetryDistribution)
}

// interfaceToInt64 converts a loosely typed SQL aggregate to int64.
// Used by the dashboard page handlers.
func interfaceToInt64(v any) int64 {
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

// interfaceToTime converts a loosely typed SQL timestamp aggregate to *time.Time.
// Used by the dashboard page handlers.
func interfaceToTime(v any) *time.Time {
	switch val := v.(type) {
	case time.Time:
		return &val
	case string:
		return parseDBTime(val)
	}
	return nil
}

func parseDBTime(s string) *time.Time {
	for _, layout := range []string{dbTimeLayout, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return &t
		}
	}
	return nil
}

func nullTimePtr(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}

func toBacklogStats(s database.APIBacklogStatsRow) BacklogStatsResponse {
	return BacklogStatsResponse{
		TotalPending: s.TotalActive, Pending: s.Pending, ClaimedTasks: s.InFlight,
		NewTasks: s.NewTasks, RetryingTasks: s.RetryingTasks, WaitingForRetry: s.WaitingForRetry,
		MaxRetryCount: s.MaxAttempts, AvgRetryCount: s.AvgAttempts,
	}
}

func toBySubscription(rows []database.APIBacklogBySubscriptionRow) []BacklogBySubscriptionResponse {
	out := make([]BacklogBySubscriptionResponse, len(rows))
	for i, r := range rows {
		out[i] = BacklogBySubscriptionResponse{
			SubscriptionID: r.SubscriptionID, SubscriptionURL: r.SubscriptionUrl,
			PendingCount: r.ActiveCount, InFlightCount: r.InFlightCount, RetryingCount: r.RetryingCount,
			MaxRetries: r.MaxAttempts, OldestTask: parseDBTime(r.OldestTask),
		}
	}
	return out
}

func toRetryDistribution(rows []database.APIRetryDistributionRow) []RetryDistributionResponse {
	out := make([]RetryDistributionResponse, len(rows))
	for i, r := range rows {
		out[i] = RetryDistributionResponse{RetryCount: r.AttemptCount, TaskCount: r.TaskCount}
	}
	return out
}

// GetBacklogOverview handles GET /api/backlog: stats, top 10 subscriptions and
// retry distribution. Failing sections set degraded:true; all failing is a 500.
func (h *BacklogHandler) GetBacklogOverview(c *gin.Context) {
	ctx := c.Request.Context()
	resp := BacklogOverviewResponse{
		Timestamp:         ts(time.Now()),
		BySubscription:    []BacklogBySubscriptionResponse{},
		RetryDistribution: []RetryDistributionResponse{},
	}
	failures := 0
	track := func(name string, err error) bool {
		ok := section(c, &resp.Degraded, name, err)
		if !ok {
			failures++
		}
		return ok
	}
	if s, err := h.Queries.APIBacklogStats(ctx); track("backlog_stats", err) {
		st := toBacklogStats(s)
		resp.Stats = &st
	}
	if rows, err := h.Queries.APIBacklogBySubscription(ctx, 10); track("backlog_by_subscription", err) {
		resp.BySubscription = toBySubscription(rows)
	}
	if rows, err := h.Queries.APIRetryDistribution(ctx); track("retry_distribution", err) {
		resp.RetryDistribution = toRetryDistribution(rows)
	}
	if failures == 3 {
		RespondInternalError(c)
		return
	}
	RespondSuccess(c, http.StatusOK, resp)
}

// GetBacklogStats handles GET /api/backlog/stats.
func (h *BacklogHandler) GetBacklogStats(c *gin.Context) {
	s, err := h.Queries.APIBacklogStats(c.Request.Context())
	if err != nil {
		respondInternal(c, "backlog stats", err)
		return
	}
	RespondSuccess(c, http.StatusOK, gin.H{"timestamp": ts(time.Now()), "stats": toBacklogStats(s)})
}

// backlogTaskRow is the column set shared by the backlog task list queries.
type backlogTaskRow = database.APIListActiveTasksRow

func toBacklogTask(r backlogTaskRow) BacklogTaskResponse {
	t := BacklogTaskResponse{
		ID: r.ID, SubscriptionID: r.SubscriptionID, SubscriptionURL: r.SubscriptionUrl,
		Status: r.Status, EventType: nullStr(r.EventType), AttemptCount: r.AttemptCount,
		CreatedAt: r.CreatedAt, LastAttemptAt: nullTimePtr(r.LastAttemptAt),
		NextAttemptAt: nullTimePtr(r.NextAttemptAt), LeaseUntil: nullTimePtr(r.LeaseUntil),
	}
	t.ClaimedBy = nullStr(r.ClaimedBy)
	return t
}

func (h *BacklogHandler) listTasks(c *gin.Context, msg string, load func(lim int64) ([]backlogTaskRow, error)) {
	limit, ok := queryInt(c, "limit", 50, 1, maxPageLimit)
	if !ok {
		return
	}
	rows, err := load(limit)
	if err != nil {
		respondInternal(c, msg, err)
		return
	}
	tasks := make([]BacklogTaskResponse, len(rows))
	for i, r := range rows {
		tasks[i] = toBacklogTask(r)
	}
	RespondSuccess(c, http.StatusOK, gin.H{"timestamp": ts(time.Now()), "count": len(tasks), "tasks": tasks})
}

// ListBacklogTasks handles GET /api/backlog/tasks: pending and in_flight tasks, oldest first.
func (h *BacklogHandler) ListBacklogTasks(c *gin.Context) {
	h.listTasks(c, "list backlog tasks", func(lim int64) ([]backlogTaskRow, error) {
		return h.Queries.APIListActiveTasks(c.Request.Context(), lim)
	})
}

// ListRetryingTasks handles GET /api/backlog/retrying: pending tasks waiting on a retry.
func (h *BacklogHandler) ListRetryingTasks(c *gin.Context) {
	h.listTasks(c, "list retrying tasks", func(lim int64) ([]backlogTaskRow, error) {
		rows, err := h.Queries.APIListRetryingTasks(c.Request.Context(), lim)
		out := make([]backlogTaskRow, len(rows))
		for i, r := range rows {
			out[i] = backlogTaskRow(r)
		}
		return out, err
	})
}

// ListActiveClaimedTasks handles GET /api/backlog/active: in_flight tasks.
func (h *BacklogHandler) ListActiveClaimedTasks(c *gin.Context) {
	h.listTasks(c, "list in-flight tasks", func(lim int64) ([]backlogTaskRow, error) {
		rows, err := h.Queries.APIListInFlightTasks(c.Request.Context(), lim)
		out := make([]backlogTaskRow, len(rows))
		for i, r := range rows {
			out[i] = backlogTaskRow(r)
		}
		return out, err
	})
}

// GetBacklogBySubscription handles GET /api/backlog/by-subscription?limit=50.
func (h *BacklogHandler) GetBacklogBySubscription(c *gin.Context) {
	limit, ok := queryInt(c, "limit", 50, 1, maxPageLimit)
	if !ok {
		return
	}
	rows, err := h.Queries.APIBacklogBySubscription(c.Request.Context(), limit)
	if err != nil {
		respondInternal(c, "backlog by subscription", err)
		return
	}
	out := toBySubscription(rows)
	RespondSuccess(c, http.StatusOK, gin.H{"timestamp": ts(time.Now()), "count": len(out), "by_subscription": out})
}

// GetRetryDistribution handles GET /api/backlog/retry-distribution.
func (h *BacklogHandler) GetRetryDistribution(c *gin.Context) {
	rows, err := h.Queries.APIRetryDistribution(c.Request.Context())
	if err != nil {
		respondInternal(c, "retry distribution", err)
		return
	}
	dist := toRetryDistribution(rows)
	var total int64
	for _, d := range dist {
		total += d.TaskCount
	}
	RespondSuccess(c, http.StatusOK, gin.H{"timestamp": ts(time.Now()), "total_retrying": total, "distribution": dist})
}
