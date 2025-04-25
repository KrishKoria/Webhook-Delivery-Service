package api

import (
	"net/http"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/gin-gonic/gin"
)

type AnalyticsHandler struct {
    Queries *database.Queries
}

func RegisterAnalyticsRoutes(r *gin.Engine, h *AnalyticsHandler) {
    r.GET("/deliveries/:delivery_task_id", h.GetDeliveryTaskStatus)
    r.GET("/subscriptions/:id/deliveries", h.ListRecentDeliveriesForSubscription)
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