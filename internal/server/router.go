// Package server wires HTTP routes and middleware onto a Gin engine.
package server

import (
	"html/template"
	"log/slog"
	"net/http"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/api"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/api/middleware"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/cache"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/KrishKoria/Webhook-Delivery-Service/web"
	"github.com/gin-gonic/gin"
)

// Deps are the shared dependencies route handlers need.
type Deps struct {
	Queries        *database.Queries
	Cache          *cache.RedisSubscriptionCache
	Logger         *slog.Logger
	TrustedProxies []string
}

// NewRouter builds the Gin engine with all routes and middleware registered.
func NewRouter(d Deps) (*gin.Engine, error) {
	r := gin.New()
	if err := r.SetTrustedProxies(d.TrustedProxies); err != nil {
		return nil, err
	}
	r.Use(gin.Recovery(), middleware.RequestID(), middleware.RequestLogger(d.Logger))

	tmpl, err := template.New("").Funcs(template.FuncMap{
		"add":      func(a, b int) int { return a + b },
		"subtract": func(a, b int) int { return a - b },
	}).ParseFS(web.Templates, "*.html")
	if err != nil {
		return nil, err
	}
	r.SetHTMLTemplate(tmpl)
	r.StaticFS("/static", http.FS(web.Static))

	authConfig := middleware.LoadAdminAuthConfig()
	sizeLimitConfig := middleware.LoadRequestSizeLimitConfig()
	rateLimiter := middleware.NewRateLimiter(middleware.LoadRateLimitConfig())
	queries, subCache := d.Queries, d.Cache

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Data plane: public ingest endpoint with size and rate limits.
	dataPlane := r.Group("/")
	dataPlane.Use(middleware.RequestSizeLimit(sizeLimitConfig), middleware.RateLimit(rateLimiter))
	api.RegisterWebhookRoutesGroup(dataPlane, &api.WebhookHandler{Queries: queries, Cache: subCache})

	// Admin JSON API.
	adminAPI := r.Group("/")
	adminAPI.Use(middleware.AdminAuth(authConfig))
	api.RegisterSubscriptionRoutesGroup(adminAPI, &api.SubscriptionHandler{Queries: queries, Cache: subCache})
	api.RegisterAnalyticsRoutesGroup(adminAPI, &api.AnalyticsHandler{Queries: queries})
	api.RegisterScheduledRoutesGroup(adminAPI, &api.ScheduledHandler{Queries: queries})

	// Admin UI.
	adminUI := r.Group("/")
	adminUI.Use(middleware.AdminAuthHTML(authConfig))
	api.RegisterUIRoutesGroup(adminUI, &api.UIHandler{Queries: queries, Cache: subCache})
	api.RegisterDLQRoutesGroup(adminUI, &api.DLQHandler{Queries: queries})
	api.RegisterDashboardRoutesGroup(adminUI, &api.DashboardHandler{Queries: queries, Cache: subCache})
	api.RegisterMetricsRoutesGroup(adminUI, &api.MetricsHandler{Queries: queries})
	api.RegisterBacklogRoutesGroup(adminUI, &api.BacklogHandler{Queries: queries})

	return r, nil
}
