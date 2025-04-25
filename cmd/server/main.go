package main

import (
	"context"
	"log"
	"net/http"
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

    subCache := cache.NewSubscriptionCache(5 * time.Minute) // 5 min TTL


    subHandler := &api.SubscriptionHandler{
        Queries: queries,
    }
    api.RegisterSubscriptionRoutes(r, subHandler)

    analyticsHandler := &api.AnalyticsHandler{Queries: queries}
    api.RegisterAnalyticsRoutes(r, analyticsHandler)

    webhookHandler := &api.WebhookHandler{Queries: queries, Cache: subCache}
    api.RegisterWebhookRoutes(r, webhookHandler)

    uiHandler := &api.UIHandler{Queries: queries}
    api.RegisterUIRoutes(r, uiHandler)

    worker := delivery.NewWorker(queries, subCache)
    go worker.Start(context.Background())
    cleanupWorker := delivery.NewCleanupWorker(queries)
    go cleanupWorker.Start(context.Background())


    r.Run(":8080") 
}