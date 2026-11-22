package middleware

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// unsetEnv removes key for the duration of the test (t.Setenv restores it).
func unsetEnv(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
}

func TestCrossOriginProtection(t *testing.T) {
	unsetEnv(t, "CSRF_TRUSTED_ORIGINS")
	mw, err := CrossOriginProtection()
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.Use(RequestID(), mw)
	r.POST("/x", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	tests := []struct {
		name    string
		method  string
		headers map[string]string
		want    int
	}{
		{"non-browser client", http.MethodPost, nil, http.StatusNoContent},
		{"same-origin fetch", http.MethodPost, map[string]string{"Sec-Fetch-Site": "same-origin"}, http.StatusNoContent},
		{"cross-site fetch", http.MethodPost, map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
		{"foreign origin", http.MethodPost, map[string]string{"Origin": "https://evil.example"}, http.StatusForbidden},
		{"cross-site GET is safe", http.MethodGet, map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusNoContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "http://admin.local/x", nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tt.want {
				t.Fatalf("status = %d, want %d", w.Code, tt.want)
			}
			if tt.want == http.StatusForbidden && !strings.Contains(w.Body.String(), CodeForbidden) {
				t.Errorf("403 body lacks envelope: %s", w.Body.String())
			}
		})
	}
}

func TestCrossOriginProtection_TrustedOrigin(t *testing.T) {
	t.Setenv("CSRF_TRUSTED_ORIGINS", "https://admin.example.com")
	mw, err := CrossOriginProtection()
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.Use(mw)
	r.POST("/x", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	req := httptest.NewRequest(http.MethodPost, "http://api.local/x", nil)
	req.Header.Set("Origin", "https://admin.example.com")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("trusted origin rejected: %d", w.Code)
	}

	t.Setenv("CSRF_TRUSTED_ORIGINS", "not a url")
	if _, err := CrossOriginProtection(); err == nil {
		t.Fatal("expected invalid origin to fail")
	}
}

func TestRequireJSONBody(t *testing.T) {
	r := gin.New()
	r.Use(RequestID(), RequireJSONBody())
	r.POST("/x", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	tests := []struct {
		name, body, ctype string
		want              int
	}{
		{"json", `{}`, "application/json", http.StatusNoContent},
		{"json with charset", `{}`, "application/json; charset=utf-8", http.StatusNoContent},
		{"form", `a=b`, "application/x-www-form-urlencoded", http.StatusUnsupportedMediaType},
		{"missing type", `{}`, "", http.StatusUnsupportedMediaType},
		{"no body", ``, "", http.StatusNoContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(tt.body))
			if tt.ctype != "" {
				req.Header.Set("Content-Type", tt.ctype)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tt.want {
				t.Fatalf("status = %d, want %d", w.Code, tt.want)
			}
		})
	}
}
