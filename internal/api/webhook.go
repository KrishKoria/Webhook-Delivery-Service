package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/net/context"
)

type WebhookHandler struct {
    Queries *database.Queries
}

// RegisterWebhookRoutes registers the webhook ingestion endpoint.
func RegisterWebhookRoutes(r *gin.Engine, h *WebhookHandler) {
    r.POST("/ingest/:subscription_id", h.IngestWebhook)
}

func (h *WebhookHandler) IngestWebhook(c *gin.Context) {
    subID := c.Param("subscription_id")

    body, err := io.ReadAll(c.Request.Body)
    if err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
        return
    }

    sub, err := h.Queries.GetSubscription(context.Background(), subID)
    if err != nil {
        c.JSON(http.StatusNotFound, gin.H{"error": "subscription not found"})
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
    err = h.Queries.CreateDeliveryTask(context.Background(), database.CreateDeliveryTaskParams{
        ID:             taskID,
        SubscriptionID: subID,
        Payload:        string(body),
    })
    if err != nil {
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