package middleware

import (
	"crypto/subtle"
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

// AdminAuthConfig holds admin authentication configuration.
type AdminAuthConfig struct {
	Enabled  bool
	Username string
	Password string
}

// LoadAdminAuthConfig reads admin auth settings from environment variables.
// ADMIN_AUTH_ENABLED: "true" to enable Basic Auth (default: false)
// ADMIN_USERNAME: Basic Auth username (required if enabled)
// ADMIN_PASSWORD: Basic Auth password (required if enabled)
func LoadAdminAuthConfig() AdminAuthConfig {
	enabled := os.Getenv("ADMIN_AUTH_ENABLED") == "true"
	username := os.Getenv("ADMIN_USERNAME")
	password := os.Getenv("ADMIN_PASSWORD")

	if enabled {
		if username == "" || password == "" {
			log.Println("WARNING: ADMIN_AUTH_ENABLED=true but ADMIN_USERNAME or ADMIN_PASSWORD not set; auth is effectively disabled")
			enabled = false
		} else {
			log.Println("Admin authentication is ENABLED")
		}
	} else {
		log.Println("Admin authentication is DISABLED (unsafe for untrusted deployments)")
	}

	return AdminAuthConfig{
		Enabled:  enabled,
		Username: username,
		Password: password,
	}
}

// AdminAuth returns a Gin middleware that enforces Basic Auth for admin routes
// when authentication is enabled. If auth is disabled, requests pass through.
func AdminAuth(config AdminAuthConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		// If auth is not enabled, allow all requests
		if !config.Enabled {
			c.Next()
			return
		}

		// Extract Basic Auth credentials
		username, password, hasAuth := c.Request.BasicAuth()
		if !hasAuth {
			c.Header("WWW-Authenticate", `Basic realm="Admin Area"`)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "authentication required",
			})
			return
		}

		// Constant-time comparison to prevent timing attacks
		usernameMatch := subtle.ConstantTimeCompare([]byte(username), []byte(config.Username)) == 1
		passwordMatch := subtle.ConstantTimeCompare([]byte(password), []byte(config.Password)) == 1

		if !usernameMatch || !passwordMatch {
			c.Header("WWW-Authenticate", `Basic realm="Admin Area"`)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid credentials",
			})
			return
		}

		// Authentication successful
		c.Next()
	}
}

// AdminAuthHTML returns a Gin middleware for UI routes that returns an HTML-friendly
// 401 response (browser will show login dialog).
func AdminAuthHTML(config AdminAuthConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !config.Enabled {
			c.Next()
			return
		}

		username, password, hasAuth := c.Request.BasicAuth()
		if !hasAuth {
			c.Header("WWW-Authenticate", `Basic realm="Admin Area"`)
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		usernameMatch := subtle.ConstantTimeCompare([]byte(username), []byte(config.Username)) == 1
		passwordMatch := subtle.ConstantTimeCompare([]byte(password), []byte(config.Password)) == 1

		if !usernameMatch || !passwordMatch {
			c.Header("WWW-Authenticate", `Basic realm="Admin Area"`)
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		c.Next()
	}
}
