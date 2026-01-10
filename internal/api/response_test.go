package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/api/middleware"
	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestRespondError(t *testing.T) {
	r := gin.New()
	r.Use(middleware.RequestID())
	r.GET("/test", func(c *gin.Context) {
		RespondError(c, http.StatusBadRequest, "test error", ErrCodeBadRequest)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}

	var resp ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Error != "test error" {
		t.Errorf("expected error 'test error', got %s", resp.Error)
	}
	if resp.Code != ErrCodeBadRequest {
		t.Errorf("expected code %s, got %s", ErrCodeBadRequest, resp.Code)
	}
	if resp.RequestID == "" {
		t.Error("expected request ID to be set")
	}
}

func TestRespondErrorWithField(t *testing.T) {
	r := gin.New()
	r.Use(middleware.RequestID())
	r.GET("/test", func(c *gin.Context) {
		RespondErrorWithField(c, http.StatusBadRequest, "invalid URL", "target_url")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}

	var resp ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Error != "invalid URL" {
		t.Errorf("expected error 'invalid URL', got %s", resp.Error)
	}
	if resp.Field != "target_url" {
		t.Errorf("expected field 'target_url', got %s", resp.Field)
	}
	if resp.Code != ErrCodeValidation {
		t.Errorf("expected code %s, got %s", ErrCodeValidation, resp.Code)
	}
	if resp.RequestID == "" {
		t.Error("expected request ID to be set")
	}
}

func TestRespondSuccess(t *testing.T) {
	r := gin.New()
	r.Use(middleware.RequestID())
	r.GET("/test", func(c *gin.Context) {
		RespondSuccess(c, http.StatusOK, gin.H{"foo": "bar"})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp SuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.RequestID == "" {
		t.Error("expected request ID to be set")
	}
	if resp.Data == nil {
		t.Error("expected data to be set")
	}
}

func TestRespondNotFound(t *testing.T) {
	r := gin.New()
	r.Use(middleware.RequestID())
	r.GET("/test", func(c *gin.Context) {
		RespondNotFound(c, "subscription")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}

	var resp ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Error != "subscription not found" {
		t.Errorf("expected error 'subscription not found', got %s", resp.Error)
	}
	if resp.Code != ErrCodeNotFound {
		t.Errorf("expected code %s, got %s", ErrCodeNotFound, resp.Code)
	}
}

func TestRespondInternalError_NoLeak(t *testing.T) {
	r := gin.New()
	r.Use(middleware.RequestID())
	r.GET("/test", func(c *gin.Context) {
		RespondInternalError(c)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}

	var resp ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Verify generic message, no stack traces or internal details
	if resp.Error != "internal server error" {
		t.Errorf("expected generic error message, got %s", resp.Error)
	}
	if resp.Code != ErrCodeInternal {
		t.Errorf("expected code %s, got %s", ErrCodeInternal, resp.Code)
	}
	if resp.RequestID == "" {
		t.Error("expected request ID to be set")
	}
}

func TestRespondValidationError(t *testing.T) {
	r := gin.New()
	r.Use(middleware.RequestID())
	r.GET("/test", func(c *gin.Context) {
		RespondValidationError(c, "field is required")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}

	var resp ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Error != "field is required" {
		t.Errorf("expected error 'field is required', got %s", resp.Error)
	}
	if resp.Code != ErrCodeValidation {
		t.Errorf("expected code %s, got %s", ErrCodeValidation, resp.Code)
	}
}
