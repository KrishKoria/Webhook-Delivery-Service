package main

import (
	"context"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/api"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/api/middleware"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/cache"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/db"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/delivery"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

// validateConfig checks required environment variables and returns combined errors.
func validateConfig() error {
	var errs []string
	// TURSO_DATABASE_URL is required for Turso/libSQL database.
	if os.Getenv("TURSO_DATABASE_URL") == "" {
		errs = append(errs, "TURSO_DATABASE_URL is required")
	}
	// PORT is optional (defaults to 8080). If set, sanity-check.
	if port := os.Getenv("PORT"); port != "" {
		if port == "0" {
			errs = append(errs, "PORT cannot be 0")
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("config validation failed: %s", strings.Join(errs, "; "))
	}
	return nil
}

// maskURL masks sensitive parts of URLs for logging
func maskURL(url string) string {
	if url == "" {
		return "NOT SET"
	}
	if len(url) > 30 {
		return url[:30] + "..."
	}
	return url
}

// boolStr returns trueStr if condition is true, falseStr otherwise
func boolStr(condition bool, trueStr, falseStr string) string {
	if condition {
		return trueStr
	}
	return falseStr
}

func main() {
	// ---------------------------------------------------------------
	// Load Environment Variables
	// ---------------------------------------------------------------
	_ = godotenv.Load()

	// ---------------------------------------------------------------
	// Log startup info for Cloud Run debugging
	// ---------------------------------------------------------------
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("=== Webhook Delivery Service Starting ===")
	log.Printf("PORT: %s", port)
	log.Printf("TURSO_DATABASE_URL: %s", maskURL(os.Getenv("TURSO_DATABASE_URL")))
	log.Printf("TURSO_AUTH_TOKEN: %s", boolStr(os.Getenv("TURSO_AUTH_TOKEN") != "", "SET", "NOT SET"))

	// ---------------------------------------------------------------
	// Config Validation (fail fast)
	// ---------------------------------------------------------------
	if err := validateConfig(); err != nil {
		log.Fatalf("STARTUP ERROR: %v", err)
	}

	// ---------------------------------------------------------------
	// Database Initialization
	// ---------------------------------------------------------------
	log.Println("Initializing database connection...")
	if err := db.Init(); err != nil {
		log.Fatalf("failed to initialize db: %v", err)
	}
	log.Println("Database connection established")

	// ---------------------------------------------------------------
	// Admin Auth Configuration
	// ---------------------------------------------------------------
	authConfig := middleware.LoadAdminAuthConfig()
	sizeLimitConfig := middleware.LoadRequestSizeLimitConfig()
	rateLimitConfig := middleware.LoadRateLimitConfig()
	rateLimiter := middleware.NewRateLimiter(rateLimitConfig)

	// ---------------------------------------------------------------
	// Gin Setup
	// ---------------------------------------------------------------
	r := gin.Default()

	// Apply request ID middleware to all routes
	r.Use(middleware.RequestID())

	// Define custom template functions
	r.SetFuncMap(template.FuncMap{
		"add": func(a, b int) int {
			return a + b
		},
		"subtract": func(a, b int) int {
			return a - b
		},
	})

	r.LoadHTMLGlob("web/templates/*.html")
	r.Static("/static", "./web/static")
	queries := database.New(db.DB)

	// ---------------------------------------------------------------
	// Public Routes (no auth required)
	// ---------------------------------------------------------------
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// ---------------------------------------------------------------
	// Redis Cache (Optional)
	// ---------------------------------------------------------------
	var subCache *cache.RedisSubscriptionCache
	redisURL := os.Getenv("REDIS_URL")
	if redisURL != "" {
		log.Printf("Attempting Redis connection (REDIS_URL set)")
		c, err := cache.NewRedisSubscriptionCache(redisURL, 5*time.Minute)
		if err != nil {
			log.Printf("WARNING: Redis cache unavailable; proceeding without cache (%v)", err)
		} else {
			subCache = c
		}
	} else {
		log.Println("REDIS_URL not set; running without Redis cache")
	}

	// ---------------------------------------------------------------
	// Data Plane Routes (no admin auth - public ingest endpoint)
	// Applies request size limiting and rate limiting for security
	// ---------------------------------------------------------------
	dataPlane := r.Group("/")
	dataPlane.Use(middleware.RequestSizeLimit(sizeLimitConfig))
	dataPlane.Use(middleware.RateLimit(rateLimiter))
	{
		webhookHandler := &api.WebhookHandler{Queries: queries, Cache: subCache}
		api.RegisterWebhookRoutesGroup(dataPlane, webhookHandler)
	}

	// ---------------------------------------------------------------
	// Admin API Routes (with JSON auth middleware)
	// ---------------------------------------------------------------
	adminAPI := r.Group("/")
	adminAPI.Use(middleware.AdminAuth(authConfig))
	{
		subHandler := &api.SubscriptionHandler{
			Queries: queries,
			Cache:   subCache,
		}
		api.RegisterSubscriptionRoutesGroup(adminAPI, subHandler)

		analyticsHandler := &api.AnalyticsHandler{Queries: queries}
		api.RegisterAnalyticsRoutesGroup(adminAPI, analyticsHandler)

		scheduledHandler := &api.ScheduledHandler{Queries: queries}
		api.RegisterScheduledRoutesGroup(adminAPI, scheduledHandler)
	}

	// ---------------------------------------------------------------
	// Admin UI Routes (with HTML auth middleware)
	// ---------------------------------------------------------------
	adminUI := r.Group("/")
	adminUI.Use(middleware.AdminAuthHTML(authConfig))
	{
		uiHandler := &api.UIHandler{Queries: queries, Cache: subCache}
		api.RegisterUIRoutesGroup(adminUI, uiHandler)

		dlqHandler := &api.DLQHandler{Queries: queries}
		api.RegisterDLQRoutesGroup(adminUI, dlqHandler)

		dashboardHandler := &api.DashboardHandler{Queries: queries, Cache: subCache}
		api.RegisterDashboardRoutesGroup(adminUI, dashboardHandler)

		metricsHandler := &api.MetricsHandler{Queries: queries}
		api.RegisterMetricsRoutesGroup(adminUI, metricsHandler)

		backlogHandler := &api.BacklogHandler{Queries: queries}
		api.RegisterBacklogRoutesGroup(adminUI, backlogHandler)
	}

	// ---------------------------------------------------------------
	// Background Workers
	// ---------------------------------------------------------------
	worker := delivery.NewWorker(queries, subCache)
	go worker.Start(context.Background())

	cleanupWorker := delivery.NewCleanupWorker(queries)
	go cleanupWorker.Start(context.Background())

	scheduledWorker := delivery.NewScheduledWorker(queries)
	go scheduledWorker.Start(context.Background())

	// ---------------------------------------------------------------
	// HTTP Server
	// ---------------------------------------------------------------
	log.Printf("=== Starting HTTP Server on port %s ===", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}