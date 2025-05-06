package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/cache"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type WebhookHandler struct {
    Queries *database.Queries
    Cache   *cache.RedisSubscriptionCache
}

// RegisterWebhookRoutes registers the webhook ingestion endpoint.
func RegisterWebhookRoutes(r *gin.Engine, h *WebhookHandler) {
    r.POST("/ingest/:subscription_id", h.IngestWebhook)
}
// IngestWebhook handles incoming webhooks.
func (h *WebhookHandler) IngestWebhook(c *gin.Context) {
    subID := c.Param("subscription_id")
    eventType := c.GetHeader("X-Event-Type")
    var sub database.Subscription
    var ok bool
    var err error
    if h.Cache != nil { 
        sub, ok = h.Cache.Get(subID)
    }
    if !ok {
        sub, err = h.Queries.GetSubscription(c, subID)
        if err != nil {
            log.Printf("Error fetching subscription %s from DB: %v", subID, err)
            c.JSON(http.StatusNotFound, gin.H{"error": "subscription not found"})
            return
        }
        if h.Cache != nil { 
            h.Cache.Set(subID, sub)
        }
    }
    body, err := io.ReadAll(c.Request.Body)
    if err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
        return
    }
    defer c.Request.Body.Close()

    if !subscriptionAllowsEvent(sub, eventType) {
        c.Status(http.StatusNoContent)
        return
    }

    if sub.Secret.Valid && sub.Secret.String != "" {
        sig := c.GetHeader("X-Hub-Signature-256")
        if !verifySignature(body, sub.Secret.String, sig) {
            c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid signature"})
            return
        }
    }

    taskID := uuid.New().String()
    err = h.Queries.CreateDeliveryTask(c, database.CreateDeliveryTaskParams{
        ID:             taskID,
        SubscriptionID: subID,
        Payload:        string(body),
    })
    if err != nil {
        log.Printf("Error creating delivery task for subscription %s: %v", subID, err)
        c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to queue delivery"})
        return
    }

    c.Status(http.StatusAccepted)
}

func verifySignature(body []byte, secret, signature string) bool {
    mac := hmac.New(sha256.New, []byte(secret))
    mac.Write(body)
    expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
    return hmac.Equal([]byte(expected), []byte(signature))
}

func subscriptionAllowsEvent(sub database.Subscription, eventType string) bool {
    if sub.EventTypes.Valid && sub.EventTypes.String != "" && eventType != "" {
        allowed := strings.Split(sub.EventTypes.String, ",")
        for _, et := range allowed {
            if strings.TrimSpace(et) == eventType {
                return true
            }
        }
        return false
    }
    return true
}
