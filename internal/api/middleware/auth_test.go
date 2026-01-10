package middleware

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestAdminAuth_Disabled(t *testing.T) {
	config := AdminAuthConfig{
		Enabled:  false,
		Username: "",
		Password: "",
	}

	r := gin.New()
	r.Use(AdminAuth(config))
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "success")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
	if w.Body.String() != "success" {
		t.Errorf("expected body 'success', got %s", w.Body.String())
	}
}

func TestAdminAuth_Enabled_NoCredentials(t *testing.T) {
	config := AdminAuthConfig{
		Enabled:  true,
		Username: "admin",
		Password: "secret",
	}

	r := gin.New()
	r.Use(AdminAuth(config))
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "success")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
	if w.Header().Get("WWW-Authenticate") == "" {
		t.Error("expected WWW-Authenticate header to be set")
	}
}

func TestAdminAuth_Enabled_InvalidCredentials(t *testing.T) {
	config := AdminAuthConfig{
		Enabled:  true,
		Username: "admin",
		Password: "secret",
	}

	r := gin.New()
	r.Use(AdminAuth(config))
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "success")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("wrong:wrong")))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}

func TestAdminAuth_Enabled_ValidCredentials(t *testing.T) {
	config := AdminAuthConfig{
		Enabled:  true,
		Username: "admin",
		Password: "secret",
	}

	r := gin.New()
	r.Use(AdminAuth(config))
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "success")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("admin:secret")))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
	if w.Body.String() != "success" {
		t.Errorf("expected body 'success', got %s", w.Body.String())
	}
}

func TestAdminAuth_Enabled_PartialWrongCredentials(t *testing.T) {
	config := AdminAuthConfig{
		Enabled:  true,
		Username: "admin",
		Password: "secret",
	}

	r := gin.New()
	r.Use(AdminAuth(config))
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "success")
	})

	tests := []struct {
		name     string
		username string
		password string
	}{
		{"wrong username", "wrong", "secret"},
		{"wrong password", "admin", "wrong"},
		{"empty username", "", "secret"},
		{"empty password", "admin", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(tt.username+":"+tt.password)))
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Errorf("expected status 401, got %d", w.Code)
			}
		})
	}
}

func TestAdminAuthHTML_Disabled(t *testing.T) {
	config := AdminAuthConfig{
		Enabled:  false,
		Username: "",
		Password: "",
	}

	r := gin.New()
	r.Use(AdminAuthHTML(config))
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "success")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestAdminAuthHTML_Enabled_NoCredentials(t *testing.T) {
	config := AdminAuthConfig{
		Enabled:  true,
		Username: "admin",
		Password: "secret",
	}

	r := gin.New()
	r.Use(AdminAuthHTML(config))
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "success")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
	if w.Header().Get("WWW-Authenticate") == "" {
		t.Error("expected WWW-Authenticate header to be set")
	}
}

func TestAdminAuthHTML_Enabled_ValidCredentials(t *testing.T) {
	config := AdminAuthConfig{
		Enabled:  true,
		Username: "admin",
		Password: "secret",
	}

	r := gin.New()
	r.Use(AdminAuthHTML(config))
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "success")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("admin:secret")))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestLoadAdminAuthConfig_Enabled_MissingCredentials(t *testing.T) {
	// Test is handled by the LoadAdminAuthConfig function logging a warning
	// and disabling auth when credentials are missing. This test verifies the behavior.
	t.Setenv("ADMIN_AUTH_ENABLED", "true")
	t.Setenv("ADMIN_USERNAME", "")
	t.Setenv("ADMIN_PASSWORD", "")

	config := LoadAdminAuthConfig()

	if config.Enabled {
		t.Error("expected auth to be disabled when credentials are missing")
	}
}

func TestLoadAdminAuthConfig_Enabled_WithCredentials(t *testing.T) {
	t.Setenv("ADMIN_AUTH_ENABLED", "true")
	t.Setenv("ADMIN_USERNAME", "testuser")
	t.Setenv("ADMIN_PASSWORD", "testpass")

	config := LoadAdminAuthConfig()

	if !config.Enabled {
		t.Error("expected auth to be enabled")
	}
	if config.Username != "testuser" {
		t.Errorf("expected username 'testuser', got %s", config.Username)
	}
	if config.Password != "testpass" {
		t.Errorf("expected password 'testpass', got %s", config.Password)
	}
}

func TestLoadAdminAuthConfig_Disabled(t *testing.T) {
	t.Setenv("ADMIN_AUTH_ENABLED", "false")

	config := LoadAdminAuthConfig()

	if config.Enabled {
		t.Error("expected auth to be disabled")
	}
}
