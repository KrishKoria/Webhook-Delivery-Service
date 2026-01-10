package api

import (
	"context"
	"database/sql"
	"net/http"
	"runtime"
	"time"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/cache"
	"github.com/gin-gonic/gin"
)

// HealthHandler handles health and readiness endpoints.
type HealthHandler struct {
	DB       *sql.DB
	Cache    *cache.RedisSubscriptionCache
	Version  string
	StartTime time.Time
}

// HealthResponse represents the response for health endpoints.
type HealthResponse struct {
	Status    string            `json:"status"`
	Timestamp string            `json:"timestamp"`
	Version   string            `json:"version,omitempty"`
	Uptime    string            `json:"uptime,omitempty"`
	Checks    map[string]Check  `json:"checks,omitempty"`
}

// Check represents a single health check result.
type Check struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	Latency string `json:"latency,omitempty"`
}

// SystemInfo represents system runtime information.
type SystemInfo struct {
	GoVersion    string `json:"go_version"`
	NumCPU       int    `json:"num_cpu"`
	NumGoroutine int    `json:"num_goroutine"`
	MemAlloc     uint64 `json:"mem_alloc_bytes"`
	MemSys       uint64 `json:"mem_sys_bytes"`
}

// RegisterHealthRoutes registers health endpoints on a gin.Engine.
func RegisterHealthRoutes(r *gin.Engine, h *HealthHandler) {
	r.GET("/healthz", h.Healthz)
	r.GET("/readyz", h.Readyz)
	r.GET("/livez", h.Livez)
	r.GET("/api/health", h.DetailedHealth)
	r.GET("/api/health/system", h.SystemHealth)
}

// RegisterHealthRoutesGroup registers health endpoints on a RouterGroup.
func RegisterHealthRoutesGroup(rg *gin.RouterGroup, h *HealthHandler) {
	rg.GET("/healthz", h.Healthz)
	rg.GET("/readyz", h.Readyz)
	rg.GET("/livez", h.Livez)
	rg.GET("/api/health", h.DetailedHealth)
	rg.GET("/api/health/system", h.SystemHealth)
}

// Healthz is a simple health check endpoint (Kubernetes-style).
// Returns 200 OK if the service is running.
func (h *HealthHandler) Healthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Livez is a liveness probe endpoint.
// Returns 200 OK if the service process is alive.
func (h *HealthHandler) Livez(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":    "ok",
		"timestamp": time.Now().Format(time.RFC3339),
	})
}

// Readyz is a readiness probe endpoint.
// Returns 200 OK only if all critical dependencies are ready.
func (h *HealthHandler) Readyz(c *gin.Context) {
	checks := h.runChecks(c)
	
	allHealthy := true
	for _, check := range checks {
		if check.Status != "ok" {
			allHealthy = false
			break
		}
	}

	resp := HealthResponse{
		Timestamp: time.Now().Format(time.RFC3339),
		Checks:    checks,
	}

	if allHealthy {
		resp.Status = "ok"
		c.JSON(http.StatusOK, resp)
	} else {
		resp.Status = "unavailable"
		c.JSON(http.StatusServiceUnavailable, resp)
	}
}

// DetailedHealth returns comprehensive health information.
func (h *HealthHandler) DetailedHealth(c *gin.Context) {
	checks := h.runChecks(c)
	
	allHealthy := true
	for _, check := range checks {
		if check.Status != "ok" {
			allHealthy = false
			break
		}
	}

	uptime := ""
	if !h.StartTime.IsZero() {
		uptime = time.Since(h.StartTime).Round(time.Second).String()
	}

	resp := HealthResponse{
		Timestamp: time.Now().Format(time.RFC3339),
		Version:   h.Version,
		Uptime:    uptime,
		Checks:    checks,
	}

	if allHealthy {
		resp.Status = "healthy"
		c.JSON(http.StatusOK, resp)
	} else {
		resp.Status = "degraded"
		c.JSON(http.StatusOK, resp) // Return 200 with degraded status for observability
	}
}

// SystemHealth returns system runtime information.
func (h *HealthHandler) SystemHealth(c *gin.Context) {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	info := SystemInfo{
		GoVersion:    runtime.Version(),
		NumCPU:       runtime.NumCPU(),
		NumGoroutine: runtime.NumGoroutine(),
		MemAlloc:     memStats.Alloc,
		MemSys:       memStats.Sys,
	}

	c.JSON(http.StatusOK, gin.H{
		"status":    "ok",
		"timestamp": time.Now().Format(time.RFC3339),
		"system":    info,
	})
}

// runChecks performs all dependency health checks.
func (h *HealthHandler) runChecks(c context.Context) map[string]Check {
	checks := make(map[string]Check)

	// Database check
	checks["database"] = h.checkDatabase(c)

	// Cache check (if configured)
	if h.Cache != nil {
		checks["cache"] = h.checkCache(c)
	}

	return checks
}

// checkDatabase verifies database connectivity.
func (h *HealthHandler) checkDatabase(c context.Context) Check {
	if h.DB == nil {
		return Check{
			Status:  "error",
			Message: "database not configured",
		}
	}

	start := time.Now()
	ctx, cancel := context.WithTimeout(c, 5*time.Second)
	defer cancel()

	err := h.DB.PingContext(ctx)
	latency := time.Since(start)

	if err != nil {
		return Check{
			Status:  "error",
			Message: err.Error(),
			Latency: latency.String(),
		}
	}

	return Check{
		Status:  "ok",
		Message: "connected",
		Latency: latency.String(),
	}
}

// checkCache verifies Redis cache connectivity.
func (h *HealthHandler) checkCache(c context.Context) Check {
	if h.Cache == nil {
		return Check{
			Status:  "ok",
			Message: "cache not configured (optional)",
		}
	}

	start := time.Now()
	ctx, cancel := context.WithTimeout(c, 5*time.Second)
	defer cancel()

	// Try to ping the cache
	err := h.Cache.Ping(ctx)
	latency := time.Since(start)

	if err != nil {
		return Check{
			Status:  "warning",
			Message: "cache unavailable: " + err.Error(),
			Latency: latency.String(),
		}
	}

	return Check{
		Status:  "ok",
		Message: "connected",
		Latency: latency.String(),
	}
}
