package api

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"time"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/api/middleware"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/gin-gonic/gin"
)

// MetricsHandler serves the /api/metrics endpoints.
type MetricsHandler struct {
	Queries *database.Queries
}

// QueueMetrics counts delivery tasks by status. Active = Pending + InFlight.
type QueueMetrics struct {
	Pending   int64 `json:"pending"`
	InFlight  int64 `json:"in_flight"`
	Active    int64 `json:"active"`
	Delivered int64 `json:"delivered"`
	Failed    int64 `json:"failed"`
	Total     int64 `json:"total"`
}

func toQueueMetrics(q database.APITaskCountsRow) QueueMetrics {
	return QueueMetrics{
		Pending: q.Pending, InFlight: q.InFlight, Active: q.Pending + q.InFlight,
		Delivered: q.Delivered, Failed: q.Failed,
		Total: q.Pending + q.InFlight + q.Delivered + q.Failed,
	}
}

// OutcomeMetrics counts delivery attempts by outcome.
type OutcomeMetrics struct {
	Success     int64   `json:"success"`
	Failure     int64   `json:"failure"`
	Total       int64   `json:"total"`
	SuccessRate float64 `json:"success_rate"`
}

func toOutcomeMetrics(s database.APIOutcomeStatsRow) OutcomeMetrics {
	return OutcomeMetrics{
		Success: s.Successful, Failure: s.FailedAttempts, Total: s.Total,
		SuccessRate: successRate(s.Successful, s.Total),
	}
}

// LatencyMetrics is the mean time from task creation to successful delivery.
type LatencyMetrics struct {
	AverageMs  float64 `json:"average_ms"`
	SampleSize int64   `json:"sample_size"`
}

// DLQMetrics counts dead-letter entries by status.
type DLQMetrics struct {
	Dead    int64 `json:"dead"`
	Retried int64 `json:"retried"`
	Total   int64 `json:"total"`
}

// ScheduledMetrics counts scheduled webhooks by status.
type ScheduledMetrics struct {
	Pending  int64 `json:"pending"`
	Enqueued int64 `json:"enqueued"`
	// Deprecated: legacy name for Enqueued, kept for the dashboard page; not serialized.
	Delivered int64 `json:"-"`
	Failed    int64 `json:"failed"`
	Cancelled int64 `json:"cancelled"`
	Total     int64 `json:"total"`
}

// MetricsResponse is the payload of GET /api/metrics. Degraded is true when
// one or more sections failed to load and were left out.
type MetricsResponse struct {
	Timestamp string            `json:"timestamp"`
	Degraded  bool              `json:"degraded"`
	Queue     *QueueMetrics     `json:"queue"`
	Outcomes  *OutcomeMetrics   `json:"outcomes"`
	Latency   *LatencyMetrics   `json:"latency"`
	DLQ       *DLQMetrics       `json:"dlq"`
	Scheduled *ScheduledMetrics `json:"scheduled"`
}

// RegisterMetricsRoutesGroup registers the metrics endpoints.
func RegisterMetricsRoutesGroup(rg *gin.RouterGroup, h *MetricsHandler) {
	rg.GET("/api/metrics", h.GetMetrics)
	rg.GET("/api/metrics/queue", h.GetQueueMetrics)
	rg.GET("/api/metrics/outcomes", h.GetOutcomeMetrics)
	rg.GET("/api/metrics/dlq", h.GetDLQMetrics)
	rg.GET("/api/metrics/scheduled", h.GetScheduledMetrics)
}

// nullFloatToInt64 converts a nullable SQL aggregate to int64 (0 when NULL).
// Used by the dashboard page handlers.
func nullFloatToInt64(nf sql.NullFloat64) int64 {
	if nf.Valid {
		return int64(nf.Float64)
	}
	return 0
}

// section runs one metrics query; on failure it logs, marks the response
// degraded and returns false so the section is left null.
func section(c *gin.Context, degraded *bool, name string, err error) bool {
	if err == nil {
		return true
	}
	slog.ErrorContext(c.Request.Context(), "metrics section failed",
		"section", name, "request_id", middleware.GetRequestID(c), "err", err)
	*degraded = true
	return false
}

// GetMetrics handles GET /api/metrics: every section over the last 24 hours.
// A failing section is null and sets degraded:true; if all fail it returns 500.
func (h *MetricsHandler) GetMetrics(c *gin.Context) {
	ctx := c.Request.Context()
	resp := MetricsResponse{Timestamp: ts(time.Now())}
	failures := 0
	track := func(name string, err error) bool {
		ok := section(c, &resp.Degraded, name, err)
		if !ok {
			failures++
		}
		return ok
	}

	if q, err := h.Queries.APITaskCounts(ctx); track("queue", err) {
		m := toQueueMetrics(q)
		resp.Queue = &m
	}
	if s, err := h.Queries.APIOutcomeStats(ctx, database.APIOutcomeStatsParams{Hours: 24}); track("outcomes", err) {
		m := toOutcomeMetrics(s)
		resp.Outcomes = &m
	}
	if l, err := h.Queries.APIDeliveryLatency(ctx, 24); track("latency", err) {
		resp.Latency = &LatencyMetrics{AverageMs: l.AvgSeconds * 1000, SampleSize: l.Samples}
	}
	if d, err := dlqMetrics(ctx, h.Queries); track("dlq", err) {
		resp.DLQ = &d
	}
	if s, err := scheduledMetrics(ctx, h.Queries); track("scheduled", err) {
		resp.Scheduled = &s
	}

	if failures == 5 {
		RespondInternalError(c)
		return
	}
	RespondSuccess(c, http.StatusOK, resp)
}

// GetQueueMetrics handles GET /api/metrics/queue.
func (h *MetricsHandler) GetQueueMetrics(c *gin.Context) {
	q, err := h.Queries.APITaskCounts(c.Request.Context())
	if err != nil {
		respondInternal(c, "queue metrics", err)
		return
	}
	RespondSuccess(c, http.StatusOK, gin.H{"timestamp": ts(time.Now()), "queue": toQueueMetrics(q)})
}

// GetOutcomeMetrics handles GET /api/metrics/outcomes?hours=24.
func (h *MetricsHandler) GetOutcomeMetrics(c *gin.Context) {
	hours, ok := queryInt(c, "hours", 24, 1, maxStatsHours)
	if !ok {
		return
	}
	s, err := h.Queries.APIOutcomeStats(c.Request.Context(), database.APIOutcomeStatsParams{Hours: hours})
	if err != nil {
		respondInternal(c, "outcome metrics", err)
		return
	}
	RespondSuccess(c, http.StatusOK, gin.H{"timestamp": ts(time.Now()), "hours": hours, "outcomes": toOutcomeMetrics(s)})
}

// GetDLQMetrics handles GET /api/metrics/dlq.
func (h *MetricsHandler) GetDLQMetrics(c *gin.Context) {
	d, err := dlqMetrics(c.Request.Context(), h.Queries)
	if err != nil {
		respondInternal(c, "dlq metrics", err)
		return
	}
	RespondSuccess(c, http.StatusOK, gin.H{"timestamp": ts(time.Now()), "dlq": d})
}

// GetScheduledMetrics handles GET /api/metrics/scheduled.
func (h *MetricsHandler) GetScheduledMetrics(c *gin.Context) {
	s, err := scheduledMetrics(c.Request.Context(), h.Queries)
	if err != nil {
		respondInternal(c, "scheduled metrics", err)
		return
	}
	RespondSuccess(c, http.StatusOK, gin.H{"timestamp": ts(time.Now()), "scheduled": s})
}

func dlqMetrics(ctx context.Context, q *database.Queries) (DLQMetrics, error) {
	d, err := q.APIDLQCounts(ctx)
	return DLQMetrics{Dead: d.Dead, Retried: d.Retried, Total: d.Total}, err
}

func scheduledMetrics(ctx context.Context, q *database.Queries) (ScheduledMetrics, error) {
	s, err := q.APIScheduledCounts(ctx)
	return ScheduledMetrics{
		Pending: s.Pending, Enqueued: s.Enqueued, Delivered: s.Enqueued,
		Failed: s.Failed, Cancelled: s.Cancelled, Total: s.Total,
	}, err
}
