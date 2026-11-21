package middleware

import (
	"log/slog"
	"net/http"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"
)

// RequestSizeLimitConfig holds configuration for request size limiting.
type RequestSizeLimitConfig struct {
	MaxBodySize int64 // Maximum request body size in bytes
	Enabled     bool
}

// DefaultMaxBodySize is 1MB.
const DefaultMaxBodySize int64 = 1 << 20

// LoadRequestSizeLimitConfig loads request size limit configuration from environment variables.
// The same limit applies to ingest and admin routes.
//   - INGEST_MAX_BODY_SIZE: maximum body size in bytes (default 1048576 = 1MB)
//   - INGEST_SIZE_LIMIT_ENABLED: enable/disable size limiting (default true)
func LoadRequestSizeLimitConfig() RequestSizeLimitConfig {
	config := RequestSizeLimitConfig{MaxBodySize: DefaultMaxBodySize, Enabled: true}
	if s := os.Getenv("INGEST_MAX_BODY_SIZE"); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 0 {
			config.MaxBodySize = n
		} else {
			slog.Warn("invalid INGEST_MAX_BODY_SIZE; using default", "value", s, "default", DefaultMaxBodySize)
		}
	}
	if s := os.Getenv("INGEST_SIZE_LIMIT_ENABLED"); s != "" {
		config.Enabled = s == "true" || s == "1"
	}
	if !config.Enabled {
		slog.Warn("request size limiting is disabled")
	}
	return config
}

// RequestSizeLimit rejects bodies over the limit with 413. A declared
// Content-Length is checked up front; otherwise the body is wrapped in
// http.MaxBytesReader and handlers map *http.MaxBytesError to 413.
func RequestSizeLimit(config RequestSizeLimitConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !config.Enabled {
			c.Next()
			return
		}
		if c.Request.ContentLength > config.MaxBodySize {
			AbortWithError(c, http.StatusRequestEntityTooLarge, CodePayloadTooLarge, "request body too large")
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, config.MaxBodySize)
		c.Next()
	}
}
