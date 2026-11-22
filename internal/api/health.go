package api

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"runtime"
	"time"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/cache"
	"github.com/gin-gonic/gin"
)

// readyTimeout bounds each dependency check so a hung database cannot hang the probe.
const readyTimeout = 2 * time.Second

// HealthHandler serves liveness, readiness and admin health endpoints.
type HealthHandler struct {
	DB        *sql.DB
	Cache     *cache.RedisSubscriptionCache
	Version   string
	StartTime time.Time
	Logger    *slog.Logger
}

// HealthResponse is the body of /readyz and /api/health.
// Status is "ok", "degraded" (an optional dependency such as Redis is down)
// or "unavailable" (the database is down).
type HealthResponse struct {
	Status    string           `json:"status"`
	Timestamp time.Time        `json:"timestamp"`
	Version   string           `json:"version,omitempty"`
	Uptime    string           `json:"uptime,omitempty"`
	Checks    map[string]Check `json:"checks"`
}

// Check is a single dependency result. Error details are logged, not returned.
type Check struct {
	Status    string `json:"status"` // ok | error | disabled
	LatencyMS int64  `json:"latency_ms"`
}

// SystemInfo is the Go runtime snapshot for /api/health/system.
type SystemInfo struct {
	GoVersion    string `json:"go_version"`
	NumCPU       int    `json:"num_cpu"`
	NumGoroutine int    `json:"num_goroutine"`
	MemAlloc     uint64 `json:"mem_alloc_bytes"`
	MemSys       uint64 `json:"mem_sys_bytes"`
}

// RegisterProbeRoutes registers the unauthenticated probes. /healthz and /livez
// are liveness (the process is serving); /readyz checks dependencies.
func RegisterProbeRoutes(rg *gin.RouterGroup, h *HealthHandler) {
	rg.GET("/healthz", h.Livez)
	rg.GET("/livez", h.Livez)
	rg.GET("/readyz", h.Readyz)
}

// RegisterAdminHealthRoutes registers the detailed health endpoints on the admin API group.
func RegisterAdminHealthRoutes(rg *gin.RouterGroup, h *HealthHandler) {
	rg.GET("/api/health", h.DetailedHealth)
	rg.GET("/api/health/system", h.SystemHealth)
}

// Livez reports that the process is up. It never touches dependencies, so a
// slow database cannot get the instance restarted.
func (h *HealthHandler) Livez(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Readyz returns 200 when the database answers (status "ok", or "degraded" when
// only Redis is down) and 503 when it does not.
func (h *HealthHandler) Readyz(c *gin.Context) {
	resp := h.evaluate(c.Request.Context())
	status := http.StatusOK
	if resp.Status == "unavailable" {
		status = http.StatusServiceUnavailable
	}
	c.JSON(status, resp)
}

// DetailedHealth is Readyz plus version and uptime. It always answers 200 so
// dashboards can read the body.
func (h *HealthHandler) DetailedHealth(c *gin.Context) {
	resp := h.evaluate(c.Request.Context())
	resp.Version = h.Version
	if !h.StartTime.IsZero() {
		resp.Uptime = time.Since(h.StartTime).Round(time.Second).String()
	}
	c.JSON(http.StatusOK, resp)
}

// SystemHealth returns Go runtime information.
func (h *HealthHandler) SystemHealth(c *gin.Context) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	RespondSuccess(c, http.StatusOK, SystemInfo{
		GoVersion:    runtime.Version(),
		NumCPU:       runtime.NumCPU(),
		NumGoroutine: runtime.NumGoroutine(),
		MemAlloc:     m.Alloc,
		MemSys:       m.Sys,
	})
}

func (h *HealthHandler) evaluate(ctx context.Context) HealthResponse {
	resp := HealthResponse{Status: "ok", Timestamp: time.Now().UTC(), Checks: map[string]Check{}}

	db := h.check(ctx, "database", func(ctx context.Context) error {
		if h.DB == nil {
			return errNotConfigured
		}
		return h.DB.PingContext(ctx)
	})
	resp.Checks["database"] = db

	cacheCheck := Check{Status: "disabled"}
	if h.Cache != nil {
		cacheCheck = h.check(ctx, "cache", h.Cache.Ping)
	}
	resp.Checks["cache"] = cacheCheck

	switch {
	case db.Status != "ok":
		resp.Status = "unavailable"
	case cacheCheck.Status == "error":
		resp.Status = "degraded"
	}
	return resp
}

func (h *HealthHandler) check(ctx context.Context, name string, ping func(context.Context) error) Check {
	ctx, cancel := context.WithTimeout(ctx, readyTimeout)
	defer cancel()
	start := time.Now()
	err := ping(ctx)
	c := Check{Status: "ok", LatencyMS: time.Since(start).Milliseconds()}
	if err != nil {
		c.Status = "error"
		h.logger().Warn("health check failed", "check", name, "err", err)
	}
	return c
}

func (h *HealthHandler) logger() *slog.Logger {
	if h.Logger != nil {
		return h.Logger
	}
	return slog.Default()
}

type healthError string

func (e healthError) Error() string { return string(e) }

const errNotConfigured = healthError("not configured")
