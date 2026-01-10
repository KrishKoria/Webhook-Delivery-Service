package middleware

import (
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestTokenBucket_Allow(t *testing.T) {
	bucket := NewTokenBucket(5, 2) // 5 max tokens, 2 tokens/sec

	// Should allow 5 requests immediately (burst)
	for i := 0; i < 5; i++ {
		if !bucket.Allow() {
			t.Errorf("Expected request %d to be allowed", i+1)
		}
	}

	// 6th request should be blocked (tokens exhausted)
	if bucket.Allow() {
		t.Error("Expected 6th request to be blocked")
	}
}

func TestTokenBucket_Refill(t *testing.T) {
	bucket := NewTokenBucket(2, 10) // 2 max tokens, 10 tokens/sec

	// Exhaust tokens
	bucket.Allow()
	bucket.Allow()
	if bucket.Allow() {
		t.Error("Expected request to be blocked after exhausting tokens")
	}

	// Wait for refill (100ms should give us 1 token at 10/sec)
	time.Sleep(110 * time.Millisecond)

	// Should be allowed now
	if !bucket.Allow() {
		t.Error("Expected request to be allowed after refill")
	}
}

func TestRateLimiter_PerSubscription(t *testing.T) {
	config := RateLimitConfig{
		RequestsPerSecond: 100,
		BurstSize:         2,
		Enabled:           true,
	}
	limiter := NewRateLimiter(config)

	// Subscription A should have its own bucket
	if !limiter.Allow("sub-a") {
		t.Error("Expected first request for sub-a to be allowed")
	}
	if !limiter.Allow("sub-a") {
		t.Error("Expected second request for sub-a to be allowed")
	}
	if limiter.Allow("sub-a") {
		t.Error("Expected third request for sub-a to be blocked")
	}

	// Subscription B should have a separate bucket
	if !limiter.Allow("sub-b") {
		t.Error("Expected first request for sub-b to be allowed")
	}
	if !limiter.Allow("sub-b") {
		t.Error("Expected second request for sub-b to be allowed")
	}
}

func TestRateLimiter_Disabled(t *testing.T) {
	config := RateLimitConfig{
		RequestsPerSecond: 1,
		BurstSize:         1,
		Enabled:           false,
	}
	limiter := NewRateLimiter(config)

	// Should always allow when disabled
	for i := 0; i < 100; i++ {
		if !limiter.Allow("sub-a") {
			t.Error("Expected all requests to be allowed when rate limiting is disabled")
		}
	}
}

func TestRateLimiter_Concurrent(t *testing.T) {
	config := RateLimitConfig{
		RequestsPerSecond: 1000,
		BurstSize:         100,
		Enabled:           true,
	}
	limiter := NewRateLimiter(config)

	var wg sync.WaitGroup
	allowed := make(chan bool, 200)

	// Launch 200 concurrent requests
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			allowed <- limiter.Allow("sub-concurrent")
		}()
	}

	wg.Wait()
	close(allowed)

	// Count allowed requests - should be around burst size (100)
	count := 0
	for a := range allowed {
		if a {
			count++
		}
	}

	// Allow some variance due to timing
	if count < 90 || count > 110 {
		t.Errorf("Expected around 100 allowed requests, got %d", count)
	}
}

func TestRateLimitMiddleware_Returns429(t *testing.T) {
	config := RateLimitConfig{
		RequestsPerSecond: 1,
		BurstSize:         1,
		Enabled:           true,
	}
	limiter := NewRateLimiter(config)

	router := gin.New()
	router.Use(RateLimit(limiter))
	router.POST("/ingest/:subscription_id", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// First request should succeed
	req1, _ := http.NewRequest("POST", "/ingest/test-sub", nil)
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Errorf("Expected first request to return 200, got %d", w1.Code)
	}

	// Second request should be rate limited
	req2, _ := http.NewRequest("POST", "/ingest/test-sub", nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusTooManyRequests {
		t.Errorf("Expected second request to return 429, got %d", w2.Code)
	}
}

func TestRateLimitMiddleware_NoSubscriptionID(t *testing.T) {
	config := RateLimitConfig{
		RequestsPerSecond: 1,
		BurstSize:         1,
		Enabled:           true,
	}
	limiter := NewRateLimiter(config)

	router := gin.New()
	router.Use(RateLimit(limiter))
	router.POST("/other", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// Should not rate limit routes without subscription_id
	for i := 0; i < 10; i++ {
		req, _ := http.NewRequest("POST", "/other", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("Expected request %d to return 200, got %d", i+1, w.Code)
		}
	}
}

func TestLoadRateLimitConfig_Defaults(t *testing.T) {
	os.Unsetenv("INGEST_RATE_LIMIT_RPS")
	os.Unsetenv("INGEST_RATE_LIMIT_BURST")
	os.Unsetenv("INGEST_RATE_LIMIT_ENABLED")

	config := LoadRateLimitConfig()

	if config.RequestsPerSecond != DefaultRequestsPerSecond {
		t.Errorf("Expected default RPS %.1f, got %.1f", DefaultRequestsPerSecond, config.RequestsPerSecond)
	}
	if config.BurstSize != DefaultBurstSize {
		t.Errorf("Expected default burst %d, got %d", DefaultBurstSize, config.BurstSize)
	}
	if !config.Enabled {
		t.Error("Expected Enabled to be true by default")
	}
}

func TestLoadRateLimitConfig_Custom(t *testing.T) {
	os.Setenv("INGEST_RATE_LIMIT_RPS", "50.5")
	os.Setenv("INGEST_RATE_LIMIT_BURST", "100")
	defer os.Unsetenv("INGEST_RATE_LIMIT_RPS")
	defer os.Unsetenv("INGEST_RATE_LIMIT_BURST")

	config := LoadRateLimitConfig()

	if config.RequestsPerSecond != 50.5 {
		t.Errorf("Expected RPS 50.5, got %.1f", config.RequestsPerSecond)
	}
	if config.BurstSize != 100 {
		t.Errorf("Expected burst 100, got %d", config.BurstSize)
	}
}

func TestLoadRateLimitConfig_Disabled(t *testing.T) {
	os.Setenv("INGEST_RATE_LIMIT_ENABLED", "false")
	defer os.Unsetenv("INGEST_RATE_LIMIT_ENABLED")

	config := LoadRateLimitConfig()

	if config.Enabled {
		t.Error("Expected Enabled to be false")
	}
}
