package middleware

import (
	"log"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// RequestIDHeader is the header name for the request ID.
const RequestIDHeader = "X-Request-ID"

// RequestIDContextKey is the context key for the request ID.
const RequestIDContextKey = "request_id"

// RequestID returns a Gin middleware that generates and attaches a unique request ID
// to each request. The request ID is:
// 1. Added to the response headers
// 2. Stored in the Gin context for handlers and loggers to use
// 3. Logged for correlation
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check if request already has an ID (e.g., from upstream proxy)
		requestID := c.GetHeader(RequestIDHeader)
		if requestID == "" {
			requestID = uuid.New().String()
		}

		// Set in context for handlers to access
		c.Set(RequestIDContextKey, requestID)

		// Set in response header
		c.Header(RequestIDHeader, requestID)

		// Log the request with ID for correlation
		log.Printf("[%s] %s %s", requestID, c.Request.Method, c.Request.URL.Path)

		c.Next()
	}
}

// GetRequestID retrieves the request ID from the Gin context.
func GetRequestID(c *gin.Context) string {
	if id, exists := c.Get(RequestIDContextKey); exists {
		if requestID, ok := id.(string); ok {
			return requestID
		}
	}
	return ""
}
