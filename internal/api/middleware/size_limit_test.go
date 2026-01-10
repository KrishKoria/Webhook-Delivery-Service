package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestRequestSizeLimit_WithinLimit(t *testing.T) {
	config := RequestSizeLimitConfig{
		MaxBodySize: 1024, // 1KB
		Enabled:     true,
	}

	router := gin.New()
	router.Use(RequestSizeLimit(config))
	router.POST("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	body := bytes.Repeat([]byte("a"), 500) // 500 bytes, under limit
	req, _ := http.NewRequest("POST", "/test", bytes.NewReader(body))
	req.Header.Set("Content-Length", "500")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestRequestSizeLimit_ExceedsLimit_ContentLength(t *testing.T) {
	config := RequestSizeLimitConfig{
		MaxBodySize: 1024, // 1KB
		Enabled:     true,
	}

	router := gin.New()
	router.Use(RequestSizeLimit(config))
	router.POST("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	body := bytes.Repeat([]byte("a"), 2000) // 2KB, over limit
	req, _ := http.NewRequest("POST", "/test", bytes.NewReader(body))
	req.Header.Set("Content-Length", "2000")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("Expected status 413, got %d", w.Code)
	}
}

func TestRequestSizeLimit_Disabled(t *testing.T) {
	config := RequestSizeLimitConfig{
		MaxBodySize: 100, // Very small
		Enabled:     false,
	}

	router := gin.New()
	router.Use(RequestSizeLimit(config))
	router.POST("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	body := bytes.Repeat([]byte("a"), 1000) // Way over limit but disabled
	req, _ := http.NewRequest("POST", "/test", bytes.NewReader(body))
	req.Header.Set("Content-Length", "1000")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200 (limit disabled), got %d", w.Code)
	}
}

func TestRequestSizeLimit_ExactLimit(t *testing.T) {
	config := RequestSizeLimitConfig{
		MaxBodySize: 100,
		Enabled:     true,
	}

	router := gin.New()
	router.Use(RequestSizeLimit(config))
	router.POST("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	body := bytes.Repeat([]byte("a"), 100) // Exactly at limit
	req, _ := http.NewRequest("POST", "/test", bytes.NewReader(body))
	req.Header.Set("Content-Length", "100")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200 (at limit), got %d", w.Code)
	}
}

func TestRequestSizeLimit_JustOverLimit(t *testing.T) {
	config := RequestSizeLimitConfig{
		MaxBodySize: 100,
		Enabled:     true,
	}

	router := gin.New()
	router.Use(RequestSizeLimit(config))
	router.POST("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	body := bytes.Repeat([]byte("a"), 101) // One byte over limit
	req, _ := http.NewRequest("POST", "/test", bytes.NewReader(body))
	req.Header.Set("Content-Length", "101")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("Expected status 413 (just over limit), got %d", w.Code)
	}
}

func TestLoadRequestSizeLimitConfig_Defaults(t *testing.T) {
	// Clear environment
	os.Unsetenv("INGEST_MAX_BODY_SIZE")
	os.Unsetenv("INGEST_SIZE_LIMIT_ENABLED")

	config := LoadRequestSizeLimitConfig()

	if config.MaxBodySize != DefaultMaxBodySize {
		t.Errorf("Expected default MaxBodySize %d, got %d", DefaultMaxBodySize, config.MaxBodySize)
	}
	if !config.Enabled {
		t.Error("Expected Enabled to be true by default")
	}
}

func TestLoadRequestSizeLimitConfig_CustomSize(t *testing.T) {
	os.Setenv("INGEST_MAX_BODY_SIZE", "2048")
	defer os.Unsetenv("INGEST_MAX_BODY_SIZE")

	config := LoadRequestSizeLimitConfig()

	if config.MaxBodySize != 2048 {
		t.Errorf("Expected MaxBodySize 2048, got %d", config.MaxBodySize)
	}
}

func TestLoadRequestSizeLimitConfig_Disabled(t *testing.T) {
	os.Setenv("INGEST_SIZE_LIMIT_ENABLED", "false")
	defer os.Unsetenv("INGEST_SIZE_LIMIT_ENABLED")

	config := LoadRequestSizeLimitConfig()

	if config.Enabled {
		t.Error("Expected Enabled to be false")
	}
}

func TestLoadRequestSizeLimitConfig_InvalidSize(t *testing.T) {
	os.Setenv("INGEST_MAX_BODY_SIZE", "invalid")
	defer os.Unsetenv("INGEST_MAX_BODY_SIZE")

	config := LoadRequestSizeLimitConfig()

	// Should fall back to default
	if config.MaxBodySize != DefaultMaxBodySize {
		t.Errorf("Expected default MaxBodySize %d for invalid input, got %d", DefaultMaxBodySize, config.MaxBodySize)
	}
}
