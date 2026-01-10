package middleware

import (
	"log"
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

// DefaultMaxBodySize is 1MB
const DefaultMaxBodySize int64 = 1 * 1024 * 1024 // 1MB

// LoadRequestSizeLimitConfig loads request size limit configuration from environment variables.
// ENV variables:
//   - INGEST_MAX_BODY_SIZE: Maximum body size in bytes (default: 1048576 = 1MB)
//   - INGEST_SIZE_LIMIT_ENABLED: Enable/disable size limiting (default: true)
func LoadRequestSizeLimitConfig() RequestSizeLimitConfig {
	config := RequestSizeLimitConfig{
		MaxBodySize: DefaultMaxBodySize,
		Enabled:     true,
	}

	// Parse max body size
	if maxSizeStr := os.Getenv("INGEST_MAX_BODY_SIZE"); maxSizeStr != "" {
		if maxSize, err := strconv.ParseInt(maxSizeStr, 10, 64); err == nil && maxSize > 0 {
			config.MaxBodySize = maxSize
		} else {
			log.Printf("WARNING: Invalid INGEST_MAX_BODY_SIZE value '%s', using default %d", maxSizeStr, DefaultMaxBodySize)
		}
	}

	// Parse enabled flag
	if enabledStr := os.Getenv("INGEST_SIZE_LIMIT_ENABLED"); enabledStr != "" {
		config.Enabled = enabledStr == "true" || enabledStr == "1"
	}

	if config.Enabled {
		log.Printf("Request size limiting ENABLED: max body size = %d bytes (%.2f KB)", config.MaxBodySize, float64(config.MaxBodySize)/1024)
	} else {
		log.Printf("WARNING: Request size limiting is DISABLED")
	}

	return config
}

// RequestSizeLimit returns a middleware that limits request body size.
// Returns HTTP 413 Payload Too Large if the Content-Length header exceeds the limit,
// or if the body read exceeds the limit.
func RequestSizeLimit(config RequestSizeLimitConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !config.Enabled {
			c.Next()
			return
		}

		// First check Content-Length header (quick rejection for honest clients)
		if c.Request.ContentLength > config.MaxBodySize {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
				"error": "request body too large",
				"code":  "PAYLOAD_TOO_LARGE",
				"max_bytes": config.MaxBodySize,
			})
			return
		}

		// Wrap the body reader to enforce limit during streaming read
		// This catches cases where Content-Length is missing or lies
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, config.MaxBodySize)

		c.Next()
	}
}
