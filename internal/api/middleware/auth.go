package middleware

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

// ErrAdminCredentialsIncomplete is returned when only one of the two admin credentials is set.
var ErrAdminCredentialsIncomplete = errors.New(
	"admin auth: set both ADMIN_USERNAME and ADMIN_PASSWORD to require a login, or neither to leave the admin console open")

// AdminAuthConfig holds admin authentication configuration.
// The zero value requires credentials; Disabled must be chosen explicitly.
type AdminAuthConfig struct {
	Disabled bool
	Username string
	Password string
}

// LoadAdminAuthConfig reads admin auth settings. Auth is opt-in so the demo
// console can be browsed without a login:
//   - neither ADMIN_USERNAME nor ADMIN_PASSWORD set: admin routes are open (logged).
//   - both set: HTTP Basic Auth on every admin API and UI route.
//   - only one set: ErrAdminCredentialsIncomplete. A half-entered credential is a
//     mistake, and quietly serving an open console would hide it.
//
// ADMIN_AUTH_ENABLED and ADMIN_AUTH_DISABLED from earlier versions are ignored.
func LoadAdminAuthConfig(logger *slog.Logger) (AdminAuthConfig, error) {
	if logger == nil {
		logger = slog.Default()
	}
	for _, legacy := range []string{"ADMIN_AUTH_ENABLED", "ADMIN_AUTH_DISABLED"} {
		if _, ok := os.LookupEnv(legacy); ok {
			logger.Warn(legacy + " is no longer read and has no effect; remove it. Admin auth is on exactly when ADMIN_USERNAME and ADMIN_PASSWORD are set")
		}
	}

	cfg := AdminAuthConfig{
		Username: os.Getenv("ADMIN_USERNAME"),
		Password: os.Getenv("ADMIN_PASSWORD"),
	}
	switch {
	case cfg.Username == "" && cfg.Password == "":
		logger.Warn("admin authentication is off: the admin API and UI are open to anyone who can reach this server. Set ADMIN_USERNAME and ADMIN_PASSWORD to require a login")
		return AdminAuthConfig{Disabled: true}, nil
	case cfg.Username == "" || cfg.Password == "":
		return AdminAuthConfig{}, ErrAdminCredentialsIncomplete
	}
	logger.Info("admin authentication enabled")
	return cfg, nil
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
