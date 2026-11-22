package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/ops"
	"github.com/gin-gonic/gin"
)

// AnalyticsHandler serves delivery tasks, delivery logs and delivery statistics.
type AnalyticsHandler struct {
	Queries *database.Queries
	Ops     *ops.Ops
}

// RegisterAnalyticsRoutesGroup registers delivery and analytics routes. The
// un-prefixed paths are kept as aliases for existing clients.
func RegisterAnalyticsRoutesGroup(rg *gin.RouterGroup, h *AnalyticsHandler) {
	rg.GET("/api/deliveries/:id", h.GetDeliveryTask)
	rg.GET("/deliveries/:id", h.GetDeliveryTask)
	rg.POST("/api/deliveries/:id/redeliver", h.Redeliver)

	// Canonical delivery-log endpoint; /subscriptions/:id/deliveries is an alias.
	rg.GET("/api/subscriptions/:id/deliveries", h.ListDeliveryLogs)
	rg.GET("/subscriptions/:id/deliveries", h.ListDeliveryLogs)

	for _, p := range []string{"/api/analytics", "/analytics"} {
		rg.GET(p+"/stats", h.GetDeliveryStats)
		rg.GET(p+"/queue", h.GetQueueDepth)
		rg.GET(p+"/failures", h.GetRecentFailures)
		rg.GET(p+"/rate", h.GetDeliveryRate)
	}
}

// DeliveryTaskResponse is the API view of a delivery task.
type DeliveryTaskResponse struct {
	ID             string  `json:"id"`
	SubscriptionID string  `json:"subscription_id"`
	Status         string  `json:"status"`
	EventType      *string `json:"event_type"`
	IdempotencyKey *string `json:"idempotency_key,omitempty"`
	AttemptCount   int64   `json:"attempt_count"`
	Payload        string  `json:"payload"`
	CreatedAt      string  `json:"created_at"`
	LastAttemptAt  *string `json:"last_attempt_at"`
	NextAttemptAt  *string `json:"next_attempt_at"`
}

// DeliveryLogResponse is the API view of one delivery attempt.
type DeliveryLogResponse struct {
	ID             string  `json:"id"`
	DeliveryTaskID string  `json:"delivery_task_id"`
	SubscriptionID string  `json:"subscription_id"`
	TargetURL      string  `json:"target_url"`
	Timestamp      string  `json:"timestamp"`
	AttemptNumber  int64   `json:"attempt_number"`
	Outcome        string  `json:"outcome"`
	HTTPStatus     *int64  `json:"http_status"`
	ErrorDetails   *string `json:"error_details"`
	TaskStatus     *string `json:"task_status,omitempty"`
	EventType      *string `json:"event_type,omitempty"`
}

func toDeliveryLog(l database.DeliveryLog) DeliveryLogResponse {
	return DeliveryLogResponse{
		ID: l.ID, DeliveryTaskID: l.DeliveryTaskID, SubscriptionID: l.SubscriptionID,
		TargetURL: l.TargetUrl, Timestamp: ts(l.Timestamp), AttemptNumber: l.AttemptNumber,
		Outcome: l.Outcome, HTTPStatus: nullInt(l.HttpStatus), ErrorDetails: nullStr(l.ErrorDetails),
	}
}

func toDeliveryLogRow(l database.APIListDeliveryLogsRow) DeliveryLogResponse {
	r := toDeliveryLog(database.DeliveryLog{
		ID: l.ID, DeliveryTaskID: l.DeliveryTaskID, SubscriptionID: l.SubscriptionID, TargetUrl: l.TargetUrl,
		Timestamp: l.Timestamp, AttemptNumber: l.AttemptNumber, Outcome: l.Outcome,
		HttpStatus: l.HttpStatus, ErrorDetails: l.ErrorDetails,
	})
	r.TaskStatus, r.EventType = nullStr(l.TaskStatus), nullStr(l.EventType)
	return r
}

// GetDeliveryTask handles GET /api/deliveries/:id: the task plus its attempt log.
func (h *AnalyticsHandler) GetDeliveryTask(c *gin.Context) {
	id := c.Param("id")
	task, err := h.Queries.GetDeliveryTask(c.Request.Context(), id)
	if err != nil {
		respondDBError(c, "delivery task", err)
		return
	}
	logs, err := h.Queries.APIListDeliveryLogsForTask(c.Request.Context(), id)
	if err != nil {
		respondInternal(c, "list task logs", err)
		return
	}
	attempts := make([]DeliveryLogResponse, len(logs))
	for i, l := range logs {
		attempts[i] = toDeliveryLog(l)
	}
	RespondSuccess(c, http.StatusOK, gin.H{
		"task": DeliveryTaskResponse{
			ID: task.ID, SubscriptionID: task.SubscriptionID, Status: task.Status,
			EventType: nullStr(task.EventType), IdempotencyKey: nullStr(task.IdempotencyKey),
			AttemptCount: task.AttemptCount, Payload: task.Payload, CreatedAt: ts(task.CreatedAt),
			LastAttemptAt: nullTS(task.LastAttemptAt), NextAttemptAt: nullTS(task.NextAttemptAt),
		},
		"logs": attempts,
	})
}

// Redeliver handles POST /api/deliveries/:id/redeliver: replays any task as a new task.
func (h *AnalyticsHandler) Redeliver(c *gin.Context) {
	taskID, err := h.Ops.Redeliver(c.Request.Context(), c.Param("id"))
	if !respondOpsErr(c, "delivery task", "redeliver", err) {
		return
	}
	RespondSuccess(c, http.StatusAccepted, gin.H{"task_id": taskID})
}

// ListDeliveryLogs handles GET /api/subscriptions/:id/deliveries with cursor
// paging and filters: outcome, status (task status), event_type, since, until (RFC3339).
func (h *AnalyticsHandler) ListDeliveryLogs(c *gin.Context) {
	limit, cur, ok := pageParams(c)
	if !ok {
		return
	}
	since, ok := queryTime(c, "since")
	if !ok {
		return
	}
	until, ok := queryTime(c, "until")
	if !ok {
		return
	}
	curTS, curID := cur.params()
	rows, err := h.Queries.APIListDeliveryLogs(c.Request.Context(), database.APIListDeliveryLogsParams{
		SubscriptionID: c.Param("id"),
		Outcome:        queryString(c, "outcome"),
		TaskStatus:     queryString(c, "status"),
		EventType:      queryString(c, "event_type"),
		Since:          since,
		Until:          until,
		CursorTs:       curTS,
		CursorID:       curID,
		Lim:            int64(limit + 1),
	})
	if err != nil {
		respondInternal(c, "list delivery logs", err)
		return
	}
	RespondSuccess(c, http.StatusOK, pageOf(rows, limit,
		func(r database.APIListDeliveryLogsRow) (time.Time, string) { return r.Timestamp, r.ID },
		toDeliveryLogRow))
}

// queryInt parses an optional integer query parameter within [lo, hi].
// It answers 400 itself and returns false on invalid input.
func queryInt(c *gin.Context, name string, def, lo, hi int64) (int64, bool) {
	s := c.Query(name)
	if s == "" {
		return def, true
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < lo || n > hi {
		RespondErrorWithField(c, http.StatusBadRequest,
			name+" must be an integer between "+strconv.FormatInt(lo, 10)+" and "+strconv.FormatInt(hi, 10), name)
		return 0, false
	}
	return n, true
}

const maxStatsHours = 24 * 30

// GetDeliveryStats handles GET /api/analytics/stats?hours=24&subscription_id=.
func (h *AnalyticsHandler) GetDeliveryStats(c *gin.Context) {
	hours, ok := queryInt(c, "hours", 24, 1, maxStatsHours)
	if !ok {
		return
	}
	subID := queryString(c, "subscription_id")
	s, err := h.Queries.APIOutcomeStats(c.Request.Context(), database.APIOutcomeStatsParams{Hours: hours, SubscriptionID: subID})
	if err != nil {
		respondInternal(c, "delivery stats", err)
		return
	}
	RespondSuccess(c, http.StatusOK, gin.H{
		"hours":                   hours,
		"subscription_id":         nullStr(subID),
		"total_attempts":          s.Total,
		"successful":              s.Successful,
		"failed_attempts":         s.FailedAttempts,
		"success_rate":            successRate(s.Successful, s.Total),
		"avg_attempts_to_success": s.AvgAttemptsToSuccess,
	})
}

// GetQueueDepth handles GET /api/analytics/queue.
func (h *AnalyticsHandler) GetQueueDepth(c *gin.Context) {
	q, err := h.Queries.APITaskCounts(c.Request.Context())
	if err != nil {
		respondInternal(c, "queue depth", err)
		return
	}
	RespondSuccess(c, http.StatusOK, toQueueMetrics(q))
}

// GetRecentFailures handles GET /api/analytics/failures?limit=20.
func (h *AnalyticsHandler) GetRecentFailures(c *gin.Context) {
	limit, ok := queryInt(c, "limit", 20, 1, maxPageLimit)
	if !ok {
		return
	}
	rows, err := h.Queries.APIRecentFailures(c.Request.Context(), limit)
	if err != nil {
		respondInternal(c, "recent failures", err)
		return
	}
	failures := make([]DeliveryLogResponse, len(rows))
	for i, r := range rows {
		failures[i] = toDeliveryLogRow(database.APIListDeliveryLogsRow(r))
	}
	RespondSuccess(c, http.StatusOK, gin.H{"failures": failures, "count": len(failures)})
}

// HourlyRate is one bucket of GET /api/analytics/rate.
type HourlyRate struct {
	Hour       string `json:"hour"`
	Total      int64  `json:"total"`
	Successful int64  `json:"successful"`
	Failed     int64  `json:"failed"`
}

// GetDeliveryRate handles GET /api/analytics/rate?hours=24 (max 168).
func (h *AnalyticsHandler) GetDeliveryRate(c *gin.Context) {
	hours, ok := queryInt(c, "hours", 24, 1, 168)
	if !ok {
		return
	}
	rows, err := h.Queries.APIDeliveryRateByHour(c.Request.Context(), hours)
	if err != nil {
		respondInternal(c, "delivery rate", err)
		return
	}
	rate := make([]HourlyRate, len(rows))
	for i, r := range rows {
		hour := r.Hour
		if t, err := time.Parse("2006-01-02 15:04", r.Hour); err == nil {
			hour = ts(t)
		}
		rate[i] = HourlyRate{Hour: hour, Total: r.Total, Successful: r.Successful, Failed: r.Failed}
	}
	RespondSuccess(c, http.StatusOK, gin.H{"hours": hours, "rate": rate})
}

func successRate(ok, total int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(ok) / float64(total) * 100
}
