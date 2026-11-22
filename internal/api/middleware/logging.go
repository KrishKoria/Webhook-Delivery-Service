package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// RequestLogger logs one structured line per request, tagged with its request ID.
// Health probes are skipped to keep logs readable.
func RequestLogger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		path := c.Request.URL.Path
		if path == "/healthz" || path == "/livez" || path == "/readyz" {
			return
		}
		status := c.Writer.Status()
		level := slog.LevelInfo
		switch {
		case status >= 500:
			level = slog.LevelError
		case status >= 400:
			level = slog.LevelWarn
		}
		attrs := []slog.Attr{
			slog.String("request_id", GetRequestID(c)),
			slog.String("method", c.Request.Method),
			slog.String("path", path),
			slog.Int("status", status),
			slog.Duration("latency", time.Since(start)),
			slog.String("client_ip", c.ClientIP()),
		}
		if len(c.Errors) > 0 {
			attrs = append(attrs, slog.String("errors", c.Errors.String()))
		}
		logger.LogAttrs(c.Request.Context(), level, "http request", attrs...)
	}
}
