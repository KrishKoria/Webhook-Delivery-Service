package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/api"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/cache"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/db"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/delivery"
	"github.com/gin-gonic/gin"
)

func main() {
    r := gin.Default()
    if err := db.Init(); err != nil {
        log.Fatalf("failed to initialize db: %v", err)
    }
    r.LoadHTMLGlob("web/templates/*.html")
    r.Static("/static", "./web/static")
    queries := database.New(db.DB)
    
    r.GET("/healthz", func(c *gin.Context) {
        c.JSON(http.StatusOK, gin.H{"status": "ok"})
    })
    r.GET("/", func(c *gin.Context) {
        c.Redirect(http.StatusFound, "/ui/subscriptions")
    })

    redisURL := os.Getenv("REDIS_URL")
    if redisURL == "" {
        redisURL = "localhost:6379"
    }
    subCache := cache.NewRedisSubscriptionCache(redisURL, 5 * time.Minute)

    subHandler := &api.SubscriptionHandler{
        Queries: queries,
    }
    api.RegisterSubscriptionRoutes(r, subHandler)

    analyticsHandler := &api.AnalyticsHandler{Queries: queries}
    api.RegisterAnalyticsRoutes(r, analyticsHandler)

    webhookHandler := &api.WebhookHandler{Queries: queries, Cache: subCache}
    api.RegisterWebhookRoutes(r, webhookHandler)

    dlqHandler := &api.DLQHandler{
        Queries: queries,
    }
    api.RegisterDLQRoutes(r, dlqHandler)

    uiHandler := &api.UIHandler{Queries: queries}
    api.RegisterUIRoutes(r, uiHandler)

    scheduledHandler := &api.ScheduledHandler{Queries: queries}
    api.RegisterScheduledRoutes(r, scheduledHandler)

    worker := delivery.NewWorker(queries, subCache)
    go worker.Start(context.Background())

    cleanupWorker := delivery.NewCleanupWorker(queries)
    go cleanupWorker.Start(context.Background())
    
    scheduledWorker := delivery.NewScheduledWorker(queries)
    go scheduledWorker.Start(context.Background())



    port := os.Getenv("PORT")
    if port == "" {
        port = "8080"
    }
    r.Run(":" + port)
}