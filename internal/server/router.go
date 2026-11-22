// Package server wires HTTP routes and middleware onto a Gin engine.
package server

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/api"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/api/middleware"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/cache"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/ops"
	"github.com/KrishKoria/Webhook-Delivery-Service/web"
	"github.com/gin-gonic/gin"
)

// Deps are the shared dependencies route handlers need.
type Deps struct {
	// Context stops background helpers (the rate limiter's janitor). Defaults to
	// context.Background().
	Context        context.Context
	DB             *sql.DB
	Queries        *database.Queries
	Cache          *cache.RedisSubscriptionCache
	Logger         *slog.Logger
	TrustedProxies []string
	Version        string
	StartTime      time.Time
}

// NewRouter builds the Gin engine with all routes and middleware registered.
// It fails when admin auth is not configured (see middleware.LoadAdminAuthConfig).
//
// Route groups:
//   - probes:     /healthz, /livez, /readyz (no auth)
//   - data plane: POST /ingest/:subscription_id (size + rate limited, no admin auth)
//   - admin API:  JSON routes; auth, CSRF, size limit, JSON-only bodies
//   - admin UI:   /, /ui/*; auth, CSRF, size limit, CSP headers
func NewRouter(d Deps) (*gin.Engine, error) {
	if d.Context == nil {
		d.Context = context.Background()
	}
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.Queries == nil {
		d.Queries = database.New(d.DB)
	}

	authConfig, err := middleware.LoadAdminAuthConfig(d.Logger)
	if err != nil {
		return nil, err
	}
	csrf, err := middleware.CrossOriginProtection()
	if err != nil {
		return nil, err
	}

	r := gin.New()
	if err := r.SetTrustedProxies(d.TrustedProxies); err != nil {
		return nil, err
	}
	r.Use(gin.Recovery(), middleware.RequestID(), middleware.RequestLogger(d.Logger))

	htmlRender, err := api.NewUIRenderer(web.Templates)
	if err != nil {
		return nil, err
	}
	r.HTMLRender = htmlRender
	r.StaticFS("/static", http.FS(web.Static))

	sizeLimit := middleware.RequestSizeLimit(middleware.LoadRequestSizeLimitConfig())
	rateLimiter := middleware.NewRateLimiter(middleware.LoadRateLimitConfig())
	rateLimiter.StartJanitor(d.Context, time.Minute)

	queries, subCache := d.Queries, d.Cache
	invalidate := func(id string) {
		if subCache != nil {
			subCache.Del(id)
		}
	}
	domain := ops.New(d.DB, invalidate)
	allowPrivate := strings.EqualFold(os.Getenv("WEBHOOK_ALLOW_PRIVATE_IPS"), "true")
	health := &api.HealthHandler{DB: d.DB, Cache: subCache, Version: d.Version, StartTime: d.StartTime, Logger: d.Logger}

	probes := r.Group("/")
	api.RegisterProbeRoutes(probes, health)

	dataPlane := r.Group("/")
	dataPlane.Use(sizeLimit, middleware.RateLimit(rateLimiter))
	api.RegisterWebhookRoutesGroup(dataPlane, &api.WebhookHandler{Queries: queries, Cache: subCache, Ops: domain})

	adminAPI := r.Group("/")
	adminAPI.Use(middleware.AdminAuth(authConfig), csrf, sizeLimit, middleware.RequireJSONBody())
	api.RegisterSubscriptionRoutesGroup(adminAPI, &api.SubscriptionHandler{Queries: queries, Ops: domain, AllowPrivateTargets: allowPrivate})
	api.RegisterAnalyticsRoutesGroup(adminAPI, &api.AnalyticsHandler{Queries: queries, Ops: domain})
	api.RegisterScheduledRoutesGroup(adminAPI, &api.ScheduledHandler{Queries: queries})
	api.RegisterDLQAPIRoutesGroup(adminAPI, &api.DLQAPIHandler{Queries: queries, Ops: domain})
	api.RegisterMetricsRoutesGroup(adminAPI, &api.MetricsHandler{Queries: queries})
	api.RegisterBacklogRoutesGroup(adminAPI, &api.BacklogHandler{Queries: queries})
	api.RegisterAdminHealthRoutes(adminAPI, health)

	adminUI := r.Group("/")
	adminUI.Use(middleware.AdminAuthHTML(authConfig), api.UISecurityHeaders(), csrf, sizeLimit)
	ui := api.NewUI(d.DB, subCache, d.Logger)
	ui.AllowPrivateTargets = allowPrivate
	api.RegisterAdminUI(adminUI, ui)

	return r, nil
}
