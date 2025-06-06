package api

import (
	"database/sql"
	"net/http"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/cache"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type SubscriptionHandler struct {
    Queries *database.Queries
    Cache   *cache.RedisSubscriptionCache
}

func RegisterSubscriptionRoutes(r *gin.Engine, h *SubscriptionHandler) {
    r.POST("/subscriptions", h.CreateSubscription)
    r.GET("/subscriptions", h.ListSubscriptions)
    r.GET("/subscriptions/:id", h.GetSubscription)
    r.PUT("/subscriptions/:id", h.UpdateSubscription)
    r.DELETE("/subscriptions/:id", h.DeleteSubscription)
}
// CreateSubscription handles POST /subscriptions
func (h *SubscriptionHandler) CreateSubscription(c *gin.Context) {
    var req struct {
        TargetUrl  string `json:"target_url" binding:"required"`
        Secret     string `json:"secret"`
        EventTypes string `json:"event_types"` // comma-separated
    }
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }
    id := uuid.New().String()
    err := h.Queries.CreateSubscription(c, database.CreateSubscriptionParams{
        ID:         id,
        TargetUrl:  req.TargetUrl,
        Secret:     sql.NullString{String: req.Secret, Valid: req.Secret != ""},
        EventTypes: sql.NullString{String: req.EventTypes, Valid: req.EventTypes != ""},
    })
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }
    c.JSON(http.StatusCreated, gin.H{"id": id})
}

// ListSubscriptions handles GET /subscriptions
func (h *SubscriptionHandler) ListSubscriptions(c *gin.Context) {
    subs, err := h.Queries.ListSubscriptions(c)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }
    c.JSON(http.StatusOK, subs)
}

// GetSubscription handles GET /subscriptions/:id
func (h *SubscriptionHandler) GetSubscription(c *gin.Context) {
    id := c.Param("id")
    sub, err := h.Queries.GetSubscription(c, id)
    if err != nil {
        c.JSON(http.StatusNotFound, gin.H{"error": "subscription not found"})
        return
    }
    c.JSON(http.StatusOK, sub)
}

// UpdateSubscription handles PUT /subscriptions/:id
func (h *SubscriptionHandler) UpdateSubscription(c *gin.Context) {
    id := c.Param("id")
    var req struct {
        TargetURL string `json:"target_url" binding:"required"`
        Secret    string `json:"secret"`
        EventTypes string `json:"event_types"` 
    }
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }
    arg := database.UpdateSubscriptionParams{
        TargetUrl: req.TargetURL,
        EventTypes: sql.NullString{
            String: req.EventTypes,
            Valid:  req.EventTypes != "",
        },
		Secret: sql.NullString{
			String: req.Secret,
			Valid:  req.Secret != "",
		},
        ID:        id,
    }
    if err := h.Queries.UpdateSubscription(c, arg); err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }
    if h.Cache != nil {
        h.Cache.Del(id)
    }
    c.Status(http.StatusNoContent)
}

// DeleteSubscription handles DELETE /subscriptions/:id
func (h *SubscriptionHandler) DeleteSubscription(c *gin.Context) {
    id := c.Param("id")
    if err := h.Queries.DeleteSubscription(c, id); err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }
    if h.Cache != nil {
        h.Cache.Del(id)
    }
    c.Status(http.StatusNoContent)
}