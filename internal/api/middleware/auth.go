package middleware

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

// ErrAdminCredentialsMissing is returned when admin auth is required but not configured.
var ErrAdminCredentialsMissing = errors.New(
	"admin auth: set ADMIN_USERNAME and ADMIN_PASSWORD, or ADMIN_AUTH_DISABLED=true for local development")

// AdminAuthConfig holds admin authentication configuration.
// The zero value requires credentials; Disabled must be chosen explicitly.
type AdminAuthConfig struct {
	Disabled bool
	Username string
	Password string
}

// LoadAdminAuthConfig reads admin auth settings and fails closed:
//   - ADMIN_USERNAME + ADMIN_PASSWORD enable Basic Auth (the default and only safe mode).
//   - ADMIN_AUTH_DISABLED=true turns auth off, with a loud warning.
//   - Deprecated: ADMIN_AUTH_ENABLED=false is treated like ADMIN_AUTH_DISABLED=true;
//     ADMIN_AUTH_ENABLED=true is ignored because auth is always on.
//
// It returns ErrAdminCredentialsMissing when auth is on but credentials are missing.
func LoadAdminAuthConfig(logger *slog.Logger) (AdminAuthConfig, error) {
	if logger == nil {
		logger = slog.Default()
	}
	disabled := envTrue("ADMIN_AUTH_DISABLED")
	if v, ok := os.LookupEnv("ADMIN_AUTH_ENABLED"); ok {
		if envFalseValue(v) {
			logger.Warn("ADMIN_AUTH_ENABLED=false is deprecated; use ADMIN_AUTH_DISABLED=true")
			disabled = true
		} else {
			logger.Warn("ADMIN_AUTH_ENABLED is deprecated and ignored; admin auth is always on unless ADMIN_AUTH_DISABLED=true")
		}
	}
	if disabled {
		logger.Warn("ADMIN AUTH IS DISABLED: every admin API and UI route is open to anyone who can reach this server. Never run like this outside local development.")
		return AdminAuthConfig{Disabled: true}, nil
	}

	cfg := AdminAuthConfig{
		Username: os.Getenv("ADMIN_USERNAME"),
		Password: os.Getenv("ADMIN_PASSWORD"),
	}
	if cfg.Username == "" || cfg.Password == "" {
		return AdminAuthConfig{}, ErrAdminCredentialsMissing
	}
	logger.Info("admin authentication enabled")
	return cfg, nil
}

func envTrue(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes":
		return true
	}
	return false
}

func envFalseValue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "0", "false", "no":
		return true
	}
	return false
}

// authorized reports whether the request carries the configured credentials.
// Both values are hashed first so the comparison is constant-time regardless of length.
func (cfg AdminAuthConfig) authorized(r *http.Request) (ok, present bool) {
	if cfg.Disabled {
		return true, true
	}
	user, pass, present := r.BasicAuth()
	if !present {
		return false, false
	}
	uh, ph := sha256.Sum256([]byte(user)), sha256.Sum256([]byte(pass))
	wu, wp := sha256.Sum256([]byte(cfg.Username)), sha256.Sum256([]byte(cfg.Password))
	userOK := subtle.ConstantTimeCompare(uh[:], wu[:]) == 1
	passOK := subtle.ConstantTimeCompare(ph[:], wp[:]) == 1
	return userOK && passOK, true
}

const authChallenge = `Basic realm="Admin Area", charset="UTF-8"`

// AdminAuth enforces Basic Auth on JSON routes; failures get a JSON 401 envelope.
func AdminAuth(cfg AdminAuthConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		ok, present := cfg.authorized(c.Request)
		if ok {
			c.Next()
			return
		}
		c.Header("WWW-Authenticate", authChallenge)
		msg := "invalid credentials"
		if !present {
			msg = "authentication required"
		}
		AbortWithError(c, http.StatusUnauthorized, CodeUnauthorized, msg)
	}
}

// AdminAuthHTML enforces Basic Auth on UI routes. A bare 401 with a challenge
// makes browsers show their login dialog.
func AdminAuthHTML(cfg AdminAuthConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if ok, _ := cfg.authorized(c.Request); ok {
			c.Next()
			return
		}
		c.Header("WWW-Authenticate", authChallenge)
		c.AbortWithStatus(http.StatusUnauthorized)
	}
}
