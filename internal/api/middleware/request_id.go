package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// RequestIDHeader is the header name for the request ID.
const RequestIDHeader = "X-Request-ID"

// RequestIDContextKey is the context key for the request ID.
const RequestIDContextKey = "request_id"

const maxRequestIDLen = 128

// RequestID returns a Gin middleware that attaches a request ID to each request.
// An upstream X-Request-ID is reused only when it is short and made of safe
// characters, so it cannot inject content into logs or response headers.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader(RequestIDHeader)
		if !validRequestID(requestID) {
			requestID = uuid.New().String()
		}
		c.Set(RequestIDContextKey, requestID)
		c.Header(RequestIDHeader, requestID)
		c.Next()
	}
}

func validRequestID(id string) bool {
	if id == "" || len(id) > maxRequestIDLen {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
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
