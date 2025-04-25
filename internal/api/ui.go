package api

import (
	"bytes"
	"database/sql"
	"io"
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
    r.GET("/ui/subscriptions/:id/analytics", h.SubscriptionAnalyticsPage) // NEW

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

// Show create subscription form
func (h *UIHandler) NewSubscriptionForm(c *gin.Context) {
    c.HTML(http.StatusOK, "new_subscription.html", nil)
}

// Handle create subscription POST
func (h *UIHandler) CreateSubscriptionForm(c *gin.Context) {
    targetURL := c.PostForm("target_url")
    secret := c.PostForm("secret")
    id := uuid.New().String()
    err := h.Queries.CreateSubscription(c, database.CreateSubscriptionParams{
        ID:        id,
        TargetUrl: targetURL,
        Secret: sql.NullString{
            String: secret,
            Valid:  secret != "",
        },
    })
    if err != nil {
        c.String(http.StatusInternalServerError, "Error: %v", err)
        return
    }
    c.Redirect(http.StatusSeeOther, "/ui/subscriptions")
}

func (h *UIHandler) SubscriptionLogsPage(c *gin.Context) {
    id := c.Param("id")
    logs, err := h.Queries.ListRecentDeliveryLogsForSubscription(c, id)
    if err != nil {
        c.String(http.StatusInternalServerError, "Error: %v", err)
        return
    }

    // Fetch status for each delivery task
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

// SendTestWebhook proxies a test payload to the ingest endpoint
func (h *UIHandler) SendTestWebhook(c *gin.Context) {
    id := c.Param("id")
    payload := c.PostForm("payload")

    url := "http://localhost:8080/ingest/" + id

    req, err := http.NewRequest("POST", url, bytes.NewBuffer([]byte(payload)))
    if err != nil {
        c.String(http.StatusInternalServerError, "Error: %v", err)
        return
    }
    req.Header.Set("Content-Type", "application/json")

    client := &http.Client{}
    resp, err := client.Do(req)
    if err != nil {
        c.String(http.StatusInternalServerError, "Error: %v", err)
        return
    }
    defer resp.Body.Close()
    io.Copy(io.Discard, resp.Body) 

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