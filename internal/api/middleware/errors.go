package middleware

import "github.com/gin-gonic/gin"

// Error codes shared by middleware and handlers.
const (
	CodeUnauthorized         = "UNAUTHORIZED"
	CodeForbidden            = "FORBIDDEN"
	CodePayloadTooLarge      = "PAYLOAD_TOO_LARGE"
	CodeUnsupportedMediaType = "UNSUPPORTED_MEDIA_TYPE"
	CodeRateLimited          = "RATE_LIMIT_EXCEEDED"
)

// AbortWithError stops the chain and writes the standard JSON error envelope:
// {"error": message, "code": code, "request_id": id}. It matches api.ErrorResponse.
func AbortWithError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{
		"error":      message,
		"code":       code,
		"request_id": GetRequestID(c),
	})
}
