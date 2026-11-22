package middleware

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func authRouter(mw gin.HandlerFunc) *gin.Engine {
	r := gin.New()
	r.Use(RequestID(), mw)
	r.GET("/test", func(c *gin.Context) { c.String(http.StatusOK, "success") })
	return r
}

func TestAdminAuth(t *testing.T) {
	enabled := AdminAuthConfig{Username: "admin", Password: "secret"}
	tests := []struct {
		name       string
		cfg        AdminAuthConfig
		user, pass string
		setAuth    bool
		wantStatus int
		wantMsg    string
	}{
		{"disabled passes", AdminAuthConfig{Disabled: true}, "", "", false, http.StatusOK, ""},
		{"no credentials", enabled, "", "", false, http.StatusUnauthorized, "authentication required"},
		{"wrong password", enabled, "admin", "nope", true, http.StatusUnauthorized, "invalid credentials"},
		{"wrong user", enabled, "root", "secret", true, http.StatusUnauthorized, "invalid credentials"},
		{"valid", enabled, "admin", "secret", true, http.StatusOK, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			if tt.setAuth {
				req.SetBasicAuth(tt.user, tt.pass)
			}
			w := httptest.NewRecorder()
			authRouter(AdminAuth(tt.cfg)).ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tt.wantStatus)
			}
			if tt.wantStatus != http.StatusUnauthorized {
				return
			}
			if w.Header().Get("WWW-Authenticate") == "" {
				t.Error("missing WWW-Authenticate challenge")
			}
			var body map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("401 body is not JSON: %v", err)
			}
			if body["error"] != tt.wantMsg || body["code"] != CodeUnauthorized || body["request_id"] == "" {
				t.Errorf("unexpected envelope: %v", body)
			}
		})
	}
}

func TestAdminAuthHTML(t *testing.T) {
	cfg := AdminAuthConfig{Username: "admin", Password: "secret"}

	w := httptest.NewRecorder()
	authRouter(AdminAuthHTML(cfg)).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/test", nil))
	if w.Code != http.StatusUnauthorized || w.Header().Get("WWW-Authenticate") == "" {
		t.Fatalf("expected 401 with challenge, got %d", w.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.SetBasicAuth("admin", "secret")
	w = httptest.NewRecorder()
	authRouter(AdminAuthHTML(cfg)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestLoadAdminAuthConfig(t *testing.T) {
	tests := []struct {
		name         string
		env          map[string]string
		wantErr      bool
		wantDisabled bool
	}{
		{name: "nothing set fails closed", wantErr: true},
		{name: "only username fails closed", env: map[string]string{"ADMIN_USERNAME": "a"}, wantErr: true},
		{name: "credentials enable auth", env: map[string]string{"ADMIN_USERNAME": "a", "ADMIN_PASSWORD": "b"}},
		{name: "explicitly disabled", env: map[string]string{"ADMIN_AUTH_DISABLED": "true"}, wantDisabled: true},
		// A leftover ADMIN_AUTH_ENABLED=false from the old deployment must not open the console.
		{name: "legacy ENABLED=false without creds fails closed", env: map[string]string{"ADMIN_AUTH_ENABLED": "false"}, wantErr: true},
		{name: "legacy ENABLED=false with creds keeps auth on", env: map[string]string{"ADMIN_AUTH_ENABLED": "false", "ADMIN_USERNAME": "a", "ADMIN_PASSWORD": "b"}},
		{name: "legacy ENABLED=true without creds fails closed", env: map[string]string{"ADMIN_AUTH_ENABLED": "true"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, k := range []string{"ADMIN_USERNAME", "ADMIN_PASSWORD", "ADMIN_AUTH_DISABLED", "ADMIN_AUTH_ENABLED"} {
				t.Setenv(k, "")
			}
			unsetEnv(t, "ADMIN_AUTH_ENABLED")
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			cfg, err := LoadAdminAuthConfig(nil)
			if tt.wantErr {
				if !errors.Is(err, ErrAdminCredentialsMissing) {
					t.Fatalf("err = %v, want ErrAdminCredentialsMissing", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.Disabled != tt.wantDisabled {
				t.Errorf("Disabled = %v, want %v", cfg.Disabled, tt.wantDisabled)
			}
		})
	}
}
