package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/api/middleware"
	"github.com/gin-gonic/gin"
)

// ErrorResponse is the error envelope for every JSON endpoint:
// {"error": "<human message>", "code": "<MACHINE_CODE>", "field": "<optional>", "request_id": "..."}.
// Middleware (auth, CSRF, size, rate limit) emits the same shape.
type ErrorResponse struct {
	Error     string `json:"error"`
	Code      string `json:"code,omitempty"`
	Field     string `json:"field,omitempty"`
	RequestID string `json:"request_id"`
}

// SuccessResponse is the success envelope: {"data": ..., "request_id": "..."}.
type SuccessResponse struct {
	Data      any    `json:"data,omitempty"`
	RequestID string `json:"request_id"`
}

// Error codes for categorizing errors.
const (
	ErrCodeValidation       = "VALIDATION_ERROR"
	ErrCodeNotFound         = "NOT_FOUND"
	ErrCodeUnauthorized     = middleware.CodeUnauthorized
	ErrCodeForbidden        = middleware.CodeForbidden
	ErrCodeInternal         = "INTERNAL_ERROR"
	ErrCodeBadRequest       = "BAD_REQUEST"
	ErrCodeConflict         = "CONFLICT"
	ErrCodePayloadTooLarge  = middleware.CodePayloadTooLarge
	ErrCodeUnsupportedMedia = middleware.CodeUnsupportedMediaType
	ErrCodeUnavailable      = "SERVICE_UNAVAILABLE"
)

// RespondError sends the error envelope.
func RespondError(c *gin.Context, status int, errMsg string, code string) {
	c.JSON(status, ErrorResponse{Error: errMsg, Code: code, RequestID: middleware.GetRequestID(c)})
}

// RespondErrorWithField sends a 4xx validation error naming the offending field.
func RespondErrorWithField(c *gin.Context, status int, errMsg string, field string) {
	c.JSON(status, ErrorResponse{Error: errMsg, Code: ErrCodeValidation, Field: field, RequestID: middleware.GetRequestID(c)})
}

// RespondSuccess sends the success envelope.
func RespondSuccess(c *gin.Context, status int, data any) {
	c.JSON(status, SuccessResponse{Data: data, RequestID: middleware.GetRequestID(c)})
}

// RespondNotFound sends a 404 for the named resource.
func RespondNotFound(c *gin.Context, resource string) {
	RespondError(c, http.StatusNotFound, resource+" not found", ErrCodeNotFound)
}

// RespondValidationError sends a 400 for a validation failure.
func RespondValidationError(c *gin.Context, message string) {
	RespondError(c, http.StatusBadRequest, message, ErrCodeValidation)
}

// RespondInternalError sends a generic 500; details never reach the client.
func RespondInternalError(c *gin.Context) {
	RespondError(c, http.StatusInternalServerError, "internal server error", ErrCodeInternal)
}

// RespondUnauthorized sends a 401.
func RespondUnauthorized(c *gin.Context, message string) {
	RespondError(c, http.StatusUnauthorized, message, ErrCodeUnauthorized)
}

// logError records err with the request ID; the client only sees a generic message.
func logError(c *gin.Context, msg string, err error) {
	slog.ErrorContext(c.Request.Context(), msg,
		"request_id", middleware.GetRequestID(c),
		"method", c.Request.Method,
		"path", c.FullPath(),
		"err", err)
}

// respondInternal logs err and sends a generic 500.
func respondInternal(c *gin.Context, msg string, err error) {
	logError(c, msg, err)
	RespondInternalError(c)
}

// respondDBError maps sql.ErrNoRows to 404 and anything else to a logged 500.
func respondDBError(c *gin.Context, resource string, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		RespondNotFound(c, resource)
		return
	}
	respondInternal(c, "load "+resource, err)
}

// respondUnavailable logs err and sends 503 with Retry-After, for transient
// failures a producer should retry (e.g. the database is unreachable).
func respondUnavailable(c *gin.Context, msg string, err error) {
	logError(c, msg, err)
	c.Header("Retry-After", "5")
	RespondError(c, http.StatusServiceUnavailable, "service temporarily unavailable", ErrCodeUnavailable)
}

// isTooLarge reports whether err came from http.MaxBytesReader.
func isTooLarge(err error) bool {
	var mbe *http.MaxBytesError
	return errors.As(err, &mbe)
}

func respondTooLarge(c *gin.Context) {
	RespondError(c, http.StatusRequestEntityTooLarge, "request body too large", ErrCodePayloadTooLarge)
}

// readBody reads the whole request body, answering 413 or 400 itself on failure.
func readBody(c *gin.Context) ([]byte, bool) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		if isTooLarge(err) {
			respondTooLarge(c)
		} else {
			RespondError(c, http.StatusBadRequest, "could not read request body", ErrCodeBadRequest)
		}
		return nil, false
	}
	return body, true
}

// bindJSON decodes a JSON body into dst. It answers 413/400 itself and returns
// false on failure. An empty body is an error unless optional is true.
func bindJSON(c *gin.Context, dst any, optional bool) bool {
	body, ok := readBody(c)
	if !ok {
		return false
	}
	if len(body) == 0 {
		if optional {
			return true
		}
		RespondValidationError(c, "request body is required")
		return false
	}
	if err := json.Unmarshal(body, dst); err != nil {
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) && typeErr.Field != "" {
			RespondErrorWithField(c, http.StatusBadRequest, "invalid type for "+typeErr.Field, typeErr.Field)
			return false
		}
		RespondValidationError(c, "request body must be valid JSON")
		return false
	}
	return true
}

// Page is the data payload for cursor-paginated lists. NextCursor is null on the last page.
type Page[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

const (
	defaultPageLimit = 50
	maxPageLimit     = 100
)

// pageParams parses ?limit (1..100, default 50; larger values are clamped) and
// ?cursor. It answers 400 itself and returns false on invalid input.
func pageParams(c *gin.Context) (limit int, cur cursor, ok bool) {
	limit = defaultPageLimit
	if s := c.Query("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			RespondErrorWithField(c, http.StatusBadRequest, "limit must be a positive integer", "limit")
			return 0, cursor{}, false
		}
		limit = min(n, maxPageLimit)
	}
	if s := c.Query("cursor"); s != "" {
		var err error
		if cur, err = decodeCursor(s); err != nil {
			RespondErrorWithField(c, http.StatusBadRequest, "invalid cursor", "cursor")
			return 0, cursor{}, false
		}
	}
	return limit, cur, true
}

// pageOf trims a limit+1 result set to limit and builds the next cursor from the last kept row.
func pageOf[R, T any](rows []R, limit int, key func(R) (time.Time, string), conv func(R) T) Page[T] {
	p := Page[T]{Items: make([]T, 0, min(len(rows), limit))}
	if len(rows) > limit {
		rows = rows[:limit]
		t, id := key(rows[len(rows)-1])
		next := encodeCursor(t, id)
		p.NextCursor = &next
	}
	for _, r := range rows {
		p.Items = append(p.Items, conv(r))
	}
	return p
}

// queryTime parses an optional RFC3339 query parameter into canonical DB text.
// It answers 400 itself and returns false on invalid input.
func queryTime(c *gin.Context, name string) (sql.NullString, bool) {
	s := c.Query(name)
	if s == "" {
		return sql.NullString{}, true
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		RespondErrorWithField(c, http.StatusBadRequest, name+" must be an RFC3339 timestamp", name)
		return sql.NullString{}, false
	}
	return sql.NullString{String: dbTime(t), Valid: true}, true
}

// queryString returns an optional query parameter as a NullString.
func queryString(c *gin.Context, name string) sql.NullString {
	s := c.Query(name)
	return sql.NullString{String: s, Valid: s != ""}
}

// --- DTO formatting helpers ---

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func nullTS(t sql.NullTime) *string {
	if !t.Valid {
		return nil
	}
	s := ts(t.Time)
	return &s
}

func nullStr(s sql.NullString) *string {
	if !s.Valid {
		return nil
	}
	return &s.String
}

func nullInt(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	return &n.Int64
}
