package api

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/gin-gonic/gin"
)

// MetricsHandler handles metrics endpoints.
type MetricsHandler struct {
	Queries *database.Queries
}

// QueueMetrics represents queue status metrics.
type QueueMetrics struct {
	Pending   int64 `json:"pending"`
	Delivered int64 `json:"delivered"`
	Failed    int64 `json:"failed"`
	Total     int64 `json:"total"`
}

// OutcomeMetrics represents delivery outcome metrics.
type OutcomeMetrics struct {
	Success    int64   `json:"success"`
	Failure    int64   `json:"failure"`
	Total      int64   `json:"total"`
	SuccessRate float64 `json:"success_rate"`
}

// LatencyMetrics represents delivery latency metrics.
type LatencyMetrics struct {
	AverageMs float64 `json:"average_ms"`
	SampleSize int64   `json:"sample_size"`
}

// MetricsResponse represents the full metrics response.
type MetricsResponse struct {
	Timestamp     string         `json:"timestamp"`
	Queue         QueueMetrics   `json:"queue"`
	Outcomes      OutcomeMetrics `json:"outcomes"`
	Latency       *LatencyMetrics `json:"latency,omitempty"`
	DLQ           *DLQMetrics    `json:"dlq,omitempty"`
	Scheduled     *ScheduledMetrics `json:"scheduled,omitempty"`
}

// DLQMetrics represents dead letter queue metrics.
type DLQMetrics struct {
	Dead    int64 `json:"dead"`
	Retried int64 `json:"retried"`
	Total   int64 `json:"total"`
}

// ScheduledMetrics represents scheduled webhooks metrics.
type ScheduledMetrics struct {
	Pending   int64 `json:"pending"`
	Delivered int64 `json:"delivered"`
	Failed    int64 `json:"failed"`
	Cancelled int64 `json:"cancelled"`
	Total     int64 `json:"total"`
}

// RegisterMetricsRoutes registers metrics endpoints on a gin.Engine.
func RegisterMetricsRoutes(r *gin.Engine, h *MetricsHandler) {
	r.GET("/api/metrics", h.GetMetrics)
	r.GET("/api/metrics/queue", h.GetQueueMetrics)
	r.GET("/api/metrics/outcomes", h.GetOutcomeMetrics)
	r.GET("/api/metrics/dlq", h.GetDLQMetrics)
	r.GET("/api/metrics/scheduled", h.GetScheduledMetrics)
}

// RegisterMetricsRoutesGroup registers metrics endpoints on a RouterGroup.
func RegisterMetricsRoutesGroup(rg *gin.RouterGroup, h *MetricsHandler) {
	rg.GET("/api/metrics", h.GetMetrics)
	rg.GET("/api/metrics/queue", h.GetQueueMetrics)
	rg.GET("/api/metrics/outcomes", h.GetOutcomeMetrics)
	rg.GET("/api/metrics/dlq", h.GetDLQMetrics)
	rg.GET("/api/metrics/scheduled", h.GetScheduledMetrics)
}

// Helper functions to safely convert sql.NullFloat64 to int64
func nullFloatToInt64(nf sql.NullFloat64) int64 {
	if nf.Valid {
		return int64(nf.Float64)
	}
	return 0
}

// GetMetrics returns comprehensive metrics.
func (h *MetricsHandler) GetMetrics(c *gin.Context) {
	resp := MetricsResponse{
		Timestamp: time.Now().Format(time.RFC3339),
	}

	// Queue metrics
	queueDepth, err := h.Queries.GetTaskQueueDepth(c)
	if err == nil {
		pending := nullFloatToInt64(queueDepth.Pending)
		delivered := nullFloatToInt64(queueDepth.Delivered)
		failed := nullFloatToInt64(queueDepth.Failed)
		resp.Queue = QueueMetrics{
			Pending:   pending,
			Delivered: delivered,
			Failed:    failed,
			Total:     pending + delivered + failed,
		}
	}

	// Outcome metrics (last 24 hours)
	stats, err := h.Queries.GetDeliveryStats(c, sql.NullString{String: "24", Valid: true})
	if err == nil {
		successful := nullFloatToInt64(stats.Successful)
		failedAttempts := nullFloatToInt64(stats.FailedAttempts)
		total := successful + failedAttempts
		var successRate float64
		if total > 0 {
			successRate = float64(successful) / float64(total) * 100
		}
		resp.Outcomes = OutcomeMetrics{
			Success:     successful,
			Failure:     failedAttempts,
			Total:       total,
			SuccessRate: successRate,
		}
	}

	// Latency metrics (last 24 hours)
	latency, err := h.Queries.GetAverageDeliveryLatency(c, sql.NullString{String: "24", Valid: true})
	if err == nil && latency.Valid {
		// Convert seconds to milliseconds
		resp.Latency = &LatencyMetrics{
			AverageMs:  latency.Float64 * 1000,
			SampleSize: 1, // We don't have sample size from this query
		}
	}

	// DLQ metrics
	dlqStats, err := h.Queries.GetDLQStats(c)
	if err == nil {
		resp.DLQ = &DLQMetrics{
			Dead:    nullFloatToInt64(dlqStats.Dead),
			Retried: nullFloatToInt64(dlqStats.Retried),
			Total:   dlqStats.Total,
		}
	}

	// Scheduled metrics
	scheduledStats, err := h.Queries.CountScheduledWebhooksByStatus(c)
	if err == nil {
		resp.Scheduled = &ScheduledMetrics{
			Pending:   nullFloatToInt64(scheduledStats.Pending),
			Delivered: nullFloatToInt64(scheduledStats.Delivered),
			Failed:    nullFloatToInt64(scheduledStats.Failed),
			Cancelled: nullFloatToInt64(scheduledStats.Cancelled),
			Total:     scheduledStats.Total,
		}
	}

	c.JSON(http.StatusOK, resp)
}

// GetQueueMetrics returns queue-specific metrics.
func (h *MetricsHandler) GetQueueMetrics(c *gin.Context) {
	queueDepth, err := h.Queries.GetTaskQueueDepth(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get queue metrics"})
		return
	}

	pending := nullFloatToInt64(queueDepth.Pending)
	delivered := nullFloatToInt64(queueDepth.Delivered)
	failed := nullFloatToInt64(queueDepth.Failed)

	c.JSON(http.StatusOK, gin.H{
		"timestamp": time.Now().Format(time.RFC3339),
		"queue": QueueMetrics{
			Pending:   pending,
			Delivered: delivered,
			Failed:    failed,
			Total:     pending + delivered + failed,
		},
	})
}

// GetOutcomeMetrics returns delivery outcome metrics.
func (h *MetricsHandler) GetOutcomeMetrics(c *gin.Context) {
	hours := c.DefaultQuery("hours", "24")
	
	stats, err := h.Queries.GetDeliveryStats(c, sql.NullString{String: hours, Valid: true})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get outcome metrics"})
		return
	}

	successful := nullFloatToInt64(stats.Successful)
	failedAttempts := nullFloatToInt64(stats.FailedAttempts)
	total := successful + failedAttempts
	var successRate float64
	if total > 0 {
		successRate = float64(successful) / float64(total) * 100
	}

	c.JSON(http.StatusOK, gin.H{
		"timestamp": time.Now().Format(time.RFC3339),
		"hours":     hours,
		"outcomes": OutcomeMetrics{
			Success:     successful,
			Failure:     failedAttempts,
			Total:       total,
			SuccessRate: successRate,
		},
	})
}

// GetDLQMetrics returns dead letter queue metrics.
func (h *MetricsHandler) GetDLQMetrics(c *gin.Context) {
	dlqStats, err := h.Queries.GetDLQStats(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get DLQ metrics"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"timestamp": time.Now().Format(time.RFC3339),
		"dlq": DLQMetrics{
			Dead:    nullFloatToInt64(dlqStats.Dead),
			Retried: nullFloatToInt64(dlqStats.Retried),
			Total:   dlqStats.Total,
		},
	})
}

// GetScheduledMetrics returns scheduled webhooks metrics.
func (h *MetricsHandler) GetScheduledMetrics(c *gin.Context) {
	scheduledStats, err := h.Queries.CountScheduledWebhooksByStatus(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get scheduled metrics"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"timestamp": time.Now().Format(time.RFC3339),
		"scheduled": ScheduledMetrics{
			Pending:   nullFloatToInt64(scheduledStats.Pending),
			Delivered: nullFloatToInt64(scheduledStats.Delivered),
			Failed:    nullFloatToInt64(scheduledStats.Failed),
			Cancelled: nullFloatToInt64(scheduledStats.Cancelled),
			Total:     scheduledStats.Total,
		},
	})
}
