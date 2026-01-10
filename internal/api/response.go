package api

import (
	"net/http"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/api/middleware"
	"github.com/gin-gonic/gin"
)

// ErrorResponse represents a consistent JSON error envelope for admin APIs.
type ErrorResponse struct {
	Error     string `json:"error"`
	Message   string `json:"message,omitempty"`
	Field     string `json:"field,omitempty"`
	RequestID string `json:"request_id"`
	Code      string `json:"code,omitempty"`
}

// SuccessResponse represents a consistent JSON response envelope with request ID.
type SuccessResponse struct {
	Data      interface{} `json:"data,omitempty"`
	Message   string      `json:"message,omitempty"`
	RequestID string      `json:"request_id"`
}

// Error codes for categorizing errors
const (
	ErrCodeValidation    = "VALIDATION_ERROR"
	ErrCodeNotFound      = "NOT_FOUND"
	ErrCodeUnauthorized  = "UNAUTHORIZED"
	ErrCodeForbidden     = "FORBIDDEN"
	ErrCodeInternal      = "INTERNAL_ERROR"
	ErrCodeBadRequest    = "BAD_REQUEST"
	ErrCodeConflict      = "CONFLICT"
)

// RespondError sends a consistent error response with the request ID.
func RespondError(c *gin.Context, status int, errMsg string, code string) {
	requestID := middleware.GetRequestID(c)
	c.JSON(status, ErrorResponse{
		Error:     errMsg,
		RequestID: requestID,
		Code:      code,
	})
}

// RespondErrorWithField sends a validation error response with field information.
func RespondErrorWithField(c *gin.Context, status int, errMsg string, field string) {
	requestID := middleware.GetRequestID(c)
	c.JSON(status, ErrorResponse{
		Error:     errMsg,
		Field:     field,
		RequestID: requestID,
		Code:      ErrCodeValidation,
	})
}

// RespondSuccess sends a consistent success response with the request ID.
func RespondSuccess(c *gin.Context, status int, data interface{}) {
	requestID := middleware.GetRequestID(c)
	c.JSON(status, SuccessResponse{
		Data:      data,
		RequestID: requestID,
	})
}

// RespondSuccessWithMessage sends a success response with a message.
func RespondSuccessWithMessage(c *gin.Context, status int, message string) {
	requestID := middleware.GetRequestID(c)
	c.JSON(status, SuccessResponse{
		Message:   message,
		RequestID: requestID,
	})
}

// RespondNotFound sends a 404 error with consistent envelope.
func RespondNotFound(c *gin.Context, resource string) {
	RespondError(c, http.StatusNotFound, resource+" not found", ErrCodeNotFound)
}

// RespondValidationError sends a 400 error for validation failures.
func RespondValidationError(c *gin.Context, message string) {
	RespondError(c, http.StatusBadRequest, message, ErrCodeValidation)
}

// RespondInternalError sends a 500 error with safe message (no leak).
func RespondInternalError(c *gin.Context) {
	RespondError(c, http.StatusInternalServerError, "internal server error", ErrCodeInternal)
}

// RespondUnauthorized sends a 401 error.
func RespondUnauthorized(c *gin.Context, message string) {
	RespondError(c, http.StatusUnauthorized, message, ErrCodeUnauthorized)
}
