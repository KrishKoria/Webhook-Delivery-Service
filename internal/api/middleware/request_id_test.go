package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequestID_GeneratesUUID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(RequestID())
	r.GET("/test", func(c *gin.Context) {
		requestID := GetRequestID(c)
		if requestID == "" {
			t.Error("expected request ID in context")
		}
		c.String(http.StatusOK, requestID)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	// Check header is set
	headerID := w.Header().Get(RequestIDHeader)
	if headerID == "" {
		t.Error("expected X-Request-ID header to be set")
	}

	// Check body matches header (handler returns the request ID)
	if w.Body.String() != headerID {
		t.Errorf("expected body to match header, got body=%s, header=%s", w.Body.String(), headerID)
	}
}

func TestRequestID_PreservesExistingID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(RequestID())
	r.GET("/test", func(c *gin.Context) {
		requestID := GetRequestID(c)
		c.String(http.StatusOK, requestID)
	})

	existingID := "existing-request-id-12345"
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set(RequestIDHeader, existingID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	// Check header preserves existing ID
	headerID := w.Header().Get(RequestIDHeader)
	if headerID != existingID {
		t.Errorf("expected preserved ID %s, got %s", existingID, headerID)
	}

	// Check body matches
	if w.Body.String() != existingID {
		t.Errorf("expected body %s, got %s", existingID, w.Body.String())
	}
}

func TestGetRequestID_NoContext(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	// No RequestID middleware
	r.GET("/test", func(c *gin.Context) {
		requestID := GetRequestID(c)
		if requestID != "" {
			t.Errorf("expected empty request ID, got %s", requestID)
		}
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestRequestID_UniquePerRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(RequestID())
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	// Make two requests and verify IDs are different
	req1 := httptest.NewRequest("GET", "/test", nil)
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)

	req2 := httptest.NewRequest("GET", "/test", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	id1 := w1.Header().Get(RequestIDHeader)
	id2 := w2.Header().Get(RequestIDHeader)

	if id1 == id2 {
		t.Errorf("expected unique IDs, got same ID for both requests: %s", id1)
	}
}
