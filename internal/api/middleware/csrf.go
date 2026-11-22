package middleware

import (
	"fmt"
	"mime"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

// CrossOriginProtection rejects cross-origin, state-changing browser requests (CSRF)
// using Go's http.CrossOriginProtection, which reads Sec-Fetch-Site and Origin.
// No tokens are needed: same-origin htmx/form posts and non-browser clients pass.
// Extra origins (e.g. a separate admin frontend) can be trusted via
// CSRF_TRUSTED_ORIGINS, a comma-separated list like "https://admin.example.com".
func CrossOriginProtection() (gin.HandlerFunc, error) {
	cop := http.NewCrossOriginProtection()
	for o := range strings.SplitSeq(os.Getenv("CSRF_TRUSTED_ORIGINS"), ",") {
		if o = strings.TrimSpace(o); o == "" {
			continue
		}
		if err := cop.AddTrustedOrigin(o); err != nil {
			return nil, fmt.Errorf("CSRF_TRUSTED_ORIGINS: %w", err)
		}
	}
	return func(c *gin.Context) {
		if err := cop.Check(c.Request); err != nil {
			AbortWithError(c, http.StatusForbidden, CodeForbidden, "cross-origin request rejected")
			return
		}
		c.Next()
	}, nil
}

// RequireJSONBody returns 415 for requests that carry a body with a media type
// other than application/json. Bodyless requests (e.g. POST /pause) pass.
func RequireJSONBody() gin.HandlerFunc {
	return func(c *gin.Context) {
		if hasBody(c.Request) && !IsJSONContentType(c.GetHeader("Content-Type")) {
			AbortWithError(c, http.StatusUnsupportedMediaType, CodeUnsupportedMediaType,
				"Content-Type must be application/json")
			return
		}
		c.Next()
	}
}

// IsJSONContentType reports whether a Content-Type header names application/json.
func IsJSONContentType(header string) bool {
	mt, _, err := mime.ParseMediaType(header)
	return err == nil && mt == "application/json"
}

func hasBody(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return r.ContentLength > 0 || (r.ContentLength < 0 && r.Body != nil && r.Body != http.NoBody)
}
