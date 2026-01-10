package api

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/gin-gonic/gin"
)

type AnalyticsHandler struct {
    Queries *database.Queries
}

func RegisterAnalyticsRoutes(r *gin.Engine, h *AnalyticsHandler) {
    r.GET("/deliveries/:delivery_task_id", h.GetDeliveryTaskStatus)
    r.GET("/subscriptions/:id/deliveries", h.ListRecentDeliveriesForSubscription)
    r.GET("/analytics/stats", h.GetDeliveryStats)
    r.GET("/analytics/queue", h.GetQueueDepth)
    r.GET("/analytics/failures", h.GetRecentFailures)
    r.GET("/analytics/rate", h.GetDeliveryRate)
}

// RegisterAnalyticsRoutesGroup registers analytics routes on a RouterGroup (for middleware).
func RegisterAnalyticsRoutesGroup(rg *gin.RouterGroup, h *AnalyticsHandler) {
    rg.GET("/deliveries/:delivery_task_id", h.GetDeliveryTaskStatus)
    rg.GET("/subscriptions/:id/deliveries", h.ListRecentDeliveriesForSubscription)
    rg.GET("/analytics/stats", h.GetDeliveryStats)
    rg.GET("/analytics/queue", h.GetQueueDepth)
    rg.GET("/analytics/failures", h.GetRecentFailures)
    rg.GET("/analytics/rate", h.GetDeliveryRate)
}

// GetDeliveryTaskStatus handles GET /deliveries/:delivery_task_id
func (h *AnalyticsHandler) GetDeliveryTaskStatus(c *gin.Context) {
    id := c.Param("delivery_task_id")
    task, err := h.Queries.GetDeliveryTask(c, id)
    if err != nil {
        c.JSON(http.StatusNotFound, gin.H{"error": "delivery task not found"})
        return
    }
    logs, err := h.Queries.ListDeliveryLogsForTask(c, id)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "could not fetch logs"})
        return
    }
    c.JSON(http.StatusOK, gin.H{
        "task":  task,
        "logs":  logs,
    })
}

// ListRecentDeliveriesForSubscription handles GET /subscriptions/:id/deliveries
func (h *AnalyticsHandler) ListRecentDeliveriesForSubscription(c *gin.Context) {
    id := c.Param("id")
    logs, err := h.Queries.ListRecentDeliveryLogsForSubscription(c, id)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "could not fetch logs"})
        return
    }
    c.JSON(http.StatusOK, logs)
}

// GetDeliveryStats handles GET /analytics/stats
// Query params: hours (default 24), subscription_id (optional)
func (h *AnalyticsHandler) GetDeliveryStats(c *gin.Context) {
    hoursStr := c.DefaultQuery("hours", "24")
    hours, err := strconv.ParseInt(hoursStr, 10, 64)
    if err != nil || hours <= 0 {
        hours = 24
    }

    hoursParam := sql.NullString{String: fmt.Sprintf("%d", hours), Valid: true}
    subscriptionID := c.Query("subscription_id")
    
    if subscriptionID != "" {
        stats, err := h.Queries.GetDeliveryStatsBySubscription(c, database.GetDeliveryStatsBySubscriptionParams{
            SubscriptionID: subscriptionID,
            Column2:        hoursParam,
        })
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"error": "could not fetch stats"})
            return
        }
        c.JSON(http.StatusOK, gin.H{
            "subscription_id":       subscriptionID,
            "hours":                 hours,
            "total_deliveries":      stats.TotalDeliveries,
            "successful":            stats.Successful.Float64,
            "failed_attempts":       stats.FailedAttempts.Float64,
            "avg_attempts_to_success": stats.AvgAttemptsToSuccess.Float64,
        })
        return
    }

    stats, err := h.Queries.GetDeliveryStats(c, hoursParam)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "could not fetch stats"})
        return
    }
    c.JSON(http.StatusOK, gin.H{
        "hours":                 hours,
        "total_deliveries":      stats.TotalDeliveries,
        "successful":            stats.Successful.Float64,
        "failed_attempts":       stats.FailedAttempts.Float64,
        "avg_attempts_to_success": stats.AvgAttemptsToSuccess.Float64,
    })
}

// GetQueueDepth handles GET /analytics/queue
func (h *AnalyticsHandler) GetQueueDepth(c *gin.Context) {
    depth, err := h.Queries.GetTaskQueueDepth(c)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "could not fetch queue depth"})
        return
    }
    c.JSON(http.StatusOK, gin.H{
        "pending":   depth.Pending,
        "delivered": depth.Delivered,
        "failed":    depth.Failed,
    })
}

// GetRecentFailures handles GET /analytics/failures
// Query params: limit (default 20)
func (h *AnalyticsHandler) GetRecentFailures(c *gin.Context) {
    limitStr := c.DefaultQuery("limit", "20")
    limit, err := strconv.ParseInt(limitStr, 10, 64)
    if err != nil || limit <= 0 || limit > 100 {
        limit = 20
    }

    failures, err := h.Queries.GetRecentFailedDeliveries(c, limit)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "could not fetch failures"})
        return
    }
    c.JSON(http.StatusOK, gin.H{
        "failures": failures,
        "count":    len(failures),
    })
}

// GetDeliveryRate handles GET /analytics/rate
// Query params: hours (default 24)
func (h *AnalyticsHandler) GetDeliveryRate(c *gin.Context) {
    hoursStr := c.DefaultQuery("hours", "24")
    hours, err := strconv.ParseInt(hoursStr, 10, 64)
    if err != nil || hours <= 0 || hours > 168 { // Max 1 week
        hours = 24
    }

    hoursParam := sql.NullString{String: fmt.Sprintf("%d", hours), Valid: true}
    rate, err := h.Queries.GetDeliveryRateByHour(c, hoursParam)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "could not fetch delivery rate"})
        return
    }
    c.JSON(http.StatusOK, gin.H{
        "hours": hours,
        "rate":  rate,
    })
}