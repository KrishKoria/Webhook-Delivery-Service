package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type UIHandler struct {
    Queries *database.Queries
}

func RegisterUIRoutes(r *gin.Engine, h *UIHandler) {
    r.GET("/ui/subscriptions", h.ListSubscriptionsPage)
    r.GET("/ui/subscriptions/new", h.NewSubscriptionForm)
    r.POST("/ui/subscriptions/new", h.CreateSubscriptionForm)
    r.GET("/ui/subscriptions/:id/logs", h.SubscriptionLogsPage)
    r.POST("/ui/subscriptions/:id/send", h.SendTestWebhook)
    r.GET("/ui/subscriptions/:id/analytics", h.SubscriptionAnalyticsPage)
    r.GET("/api/subscriptions/:id/logs", h.GetLogsJSON)
    r.GET("/ui/subscriptions/:id/edit", h.EditSubscriptionForm)
    r.POST("/ui/subscriptions/:id/edit", h.UpdateSubscriptionForm)
    r.POST("/ui/subscriptions/:id/delete", h.DeleteSubscription)
    r.GET("/ui/subscriptions/:id/scheduled", h.ScheduledPage)
    r.GET("/ui/subscriptions/:id/scheduled/new", h.NewScheduledPage) 
    r.GET("/ui/scheduled", h.GlobalScheduledPage)



}

// List all subscriptions
func (h *UIHandler) ListSubscriptionsPage(c *gin.Context) {
    subs, err := h.Queries.ListSubscriptions(c)
    if err != nil {
        c.String(http.StatusInternalServerError, "Error: %v", err)
        return
    }
    c.HTML(http.StatusOK, "subscriptions.html", gin.H{
        "Subscriptions": subs,
    })
}

func (h *UIHandler) NewSubscriptionForm(c *gin.Context) {
    c.HTML(http.StatusOK, "new_subscription.html", nil)
}

func (h *UIHandler) EditSubscriptionForm(c *gin.Context) {
    id := c.Param("id")
    sub, err := h.Queries.GetSubscription(c, id)
    if err != nil {
        c.String(404, "Subscription not found")
        return
    }
    c.HTML(200, "edit_subscription.html", gin.H{"Subscription": sub})
}

func (h *UIHandler) CreateSubscriptionForm(c *gin.Context) {
    targetURL := c.PostForm("target_url")
    secret := c.PostForm("secret")
    eventTypes := c.PostForm("event_types")
    id := uuid.New().String()
    err := h.Queries.CreateSubscription(c, database.CreateSubscriptionParams{
        ID:         id,
        TargetUrl:  targetURL,
        Secret:     sql.NullString{String: secret, Valid: secret != ""},
        EventTypes: sql.NullString{String: eventTypes, Valid: eventTypes != ""},
    })
    if err != nil {
        c.String(http.StatusInternalServerError, "Error: %v", err)
        return
    }
    c.Redirect(http.StatusSeeOther, "/ui/subscriptions")
}

func (h *UIHandler) UpdateSubscriptionForm(c *gin.Context) {
    id := c.Param("id")
    targetURL := c.PostForm("target_url")
    secret := c.PostForm("secret")
    eventTypes := c.PostForm("event_types")
    err := h.Queries.UpdateSubscription(c, database.UpdateSubscriptionParams{
        TargetUrl:  targetURL,
        Secret:     sql.NullString{String: secret, Valid: secret != ""},
        EventTypes: sql.NullString{String: eventTypes, Valid: eventTypes != ""},
        ID:         id,
    })
    if err != nil {
        c.String(500, "Update failed: %v", err)
        return
    }
    c.Redirect(303, "/ui/subscriptions")
}

func (h *UIHandler) DeleteSubscription(c *gin.Context) {
    id := c.Param("id")
    err := h.Queries.DeleteSubscription(c, id)
    if err != nil {
        c.String(500, "Delete failed: %v", err)
        return
    }
    c.Redirect(303, "/ui/subscriptions")
}

func (h *UIHandler) SubscriptionLogsPage(c *gin.Context) {
    id := c.Param("id")
    logs, err := h.Queries.ListRecentDeliveryLogsForSubscription(c, id)
    if err != nil {
        c.String(http.StatusInternalServerError, "Error: %v", err)
        return
    }

    type LogWithStatus struct {
        database.DeliveryLog
        TaskStatus string
    }
    var logsWithStatus []LogWithStatus
    for _, logEntry := range logs {
        task, err := h.Queries.GetDeliveryTask(c, logEntry.DeliveryTaskID)
        status := "-"
        if err == nil {
            status = task.Status
        }
        logsWithStatus = append(logsWithStatus, LogWithStatus{
            DeliveryLog: logEntry,
            TaskStatus:  status,
        })
    }

    c.HTML(http.StatusOK, "logs.html", gin.H{
        "SubscriptionID": id,
        "Logs":           logsWithStatus,
    })
}

func (h *UIHandler) SendTestWebhook(c *gin.Context) {
    id := c.Param("id")
    payload := c.PostForm("payload")
    eventType := c.PostForm("event_type")

    url := "http://localhost:8080/ingest/" + id

    req, err := http.NewRequest("POST", url, bytes.NewBuffer([]byte(payload)))
    if err != nil {
        c.String(http.StatusInternalServerError, "Error: %v", err)
        return
    }
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("X-Event-Type", eventType)

    sub, err := h.Queries.GetSubscription(c, id)
    if err == nil && sub.Secret.Valid && sub.Secret.String != "" {
        mac := hmac.New(sha256.New, []byte(sub.Secret.String))
        mac.Write([]byte(payload))
        signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
        req.Header.Set("X-Hub-Signature-256", signature)
    }

    client := &http.Client{}
    resp, err := client.Do(req)
    if err != nil {
        c.String(http.StatusInternalServerError, "Error: %v", err)
        return
    }
    defer resp.Body.Close()

    c.Redirect(http.StatusSeeOther, "/ui/subscriptions/"+id+"/logs")
}

func (h *UIHandler) SubscriptionAnalyticsPage(c *gin.Context) {
    id := c.Param("id")
    logs, err := h.Queries.ListRecentDeliveryLogsForSubscription(c, id)
    if err != nil {
        c.String(http.StatusInternalServerError, "Error: %v", err)
        return
    }

    var total, success, failed int
    var lastAttempt time.Time

    for _, logEntry := range logs {
        total++
        if logEntry.Outcome == "success" {
            success++
        }
        if logEntry.Outcome == "failed_attempt" || logEntry.Outcome == "failure" {
            failed++
        }
        if logEntry.Timestamp.After(lastAttempt) {
            lastAttempt = logEntry.Timestamp
        }
    }

    var lastAttemptStr string
    if !lastAttempt.IsZero() {
        lastAttemptStr = lastAttempt.Format(time.RFC3339)
    }

    c.HTML(http.StatusOK, "analytics.html", gin.H{
        "SubscriptionID": id,
        "Total":          total,
        "Success":        success,
        "Failed":         failed,
        "LastAttempt":    lastAttemptStr,
        "Logs":           logs,
    })
}

func (h *UIHandler) GetLogsJSON(c *gin.Context) {
    id := c.Param("id")
    logs, err := h.Queries.ListRecentDeliveryLogsForSubscription(c, id)
    if err != nil {
        c.JSON(500, gin.H{"error": err.Error()})
        return
    }

    type LogWithStatus struct {
        database.DeliveryLog
        TaskStatus string `json:"status"`
    }
    var logsWithStatus []LogWithStatus
    for _, logEntry := range logs {
        status := "-"
        task, err := h.Queries.GetDeliveryTask(c, logEntry.DeliveryTaskID)
        if err == nil {
            status = task.Status
        }
        logsWithStatus = append(logsWithStatus, LogWithStatus{
            DeliveryLog: logEntry,
            TaskStatus:  status,
        })
    }

    c.JSON(200, logsWithStatus)
}

func (h *UIHandler) ScheduledPage(c *gin.Context) {
    subID := c.Param("id")
    c.HTML(http.StatusOK, "scheduled.html", gin.H{
        "SubscriptionID": subID,
    })
}
func (h *UIHandler) NewScheduledPage(c *gin.Context) {
    subID := c.Param("id")
    c.HTML(http.StatusOK, "new_scheduled.html", gin.H{
        "SubscriptionID": subID,
    })
}

func (h *UIHandler) GlobalScheduledPage(c *gin.Context) {
    c.HTML(http.StatusOK, "scheduled.html", gin.H{
        "SubscriptionID": "", 
        "IsGlobalView":   true,
    })
}