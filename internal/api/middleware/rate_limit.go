package middleware

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// RateLimitConfig holds configuration for rate limiting.
type RateLimitConfig struct {
	// RequestsPerSecond is the allowed requests per second per subscription
	RequestsPerSecond float64
	// BurstSize is the maximum burst size allowed
	BurstSize int
	// Enabled controls whether rate limiting is active
	Enabled bool
}

// DefaultRequestsPerSecond is 10 requests per second
const DefaultRequestsPerSecond float64 = 10.0

// DefaultBurstSize allows short bursts of traffic
const DefaultBurstSize int = 20

// LoadRateLimitConfig loads rate limit configuration from environment variables.
// ENV variables:
//   - INGEST_RATE_LIMIT_RPS: Requests per second per subscription (default: 10)
//   - INGEST_RATE_LIMIT_BURST: Burst size (default: 20)
//   - INGEST_RATE_LIMIT_ENABLED: Enable/disable rate limiting (default: true)
func LoadRateLimitConfig() RateLimitConfig {
	config := RateLimitConfig{
		RequestsPerSecond: DefaultRequestsPerSecond,
		BurstSize:         DefaultBurstSize,
		Enabled:           true,
	}

	// Parse RPS
	if rpsStr := os.Getenv("INGEST_RATE_LIMIT_RPS"); rpsStr != "" {
		if rps, err := strconv.ParseFloat(rpsStr, 64); err == nil && rps > 0 {
			config.RequestsPerSecond = rps
		} else {
			log.Printf("WARNING: Invalid INGEST_RATE_LIMIT_RPS value '%s', using default %.1f", rpsStr, DefaultRequestsPerSecond)
		}
	}

	// Parse burst size
	if burstStr := os.Getenv("INGEST_RATE_LIMIT_BURST"); burstStr != "" {
		if burst, err := strconv.Atoi(burstStr); err == nil && burst > 0 {
			config.BurstSize = burst
		} else {
			log.Printf("WARNING: Invalid INGEST_RATE_LIMIT_BURST value '%s', using default %d", burstStr, DefaultBurstSize)
		}
	}

	// Parse enabled flag
	if enabledStr := os.Getenv("INGEST_RATE_LIMIT_ENABLED"); enabledStr != "" {
		config.Enabled = enabledStr == "true" || enabledStr == "1"
	}

	if config.Enabled {
		log.Printf("Rate limiting ENABLED: %.1f requests/sec, burst=%d", config.RequestsPerSecond, config.BurstSize)
	} else {
		log.Printf("WARNING: Rate limiting is DISABLED")
	}

	return config
}

// TokenBucket implements a simple token bucket rate limiter.
type TokenBucket struct {
	tokens     float64
	maxTokens  float64
	refillRate float64 // tokens per second
	lastRefill time.Time
	mu         sync.Mutex
}

// NewTokenBucket creates a new token bucket with the given parameters.
func NewTokenBucket(maxTokens float64, refillRate float64) *TokenBucket {
	return &TokenBucket{
		tokens:     maxTokens,
		maxTokens:  maxTokens,
		refillRate: refillRate,
		lastRefill: time.Now(),
	}
}

// Allow checks if a request is allowed and consumes a token if so.
func (tb *TokenBucket) Allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	// Refill tokens based on time elapsed
	now := time.Now()
	elapsed := now.Sub(tb.lastRefill).Seconds()
	tb.tokens += elapsed * tb.refillRate
	if tb.tokens > tb.maxTokens {
		tb.tokens = tb.maxTokens
	}
	tb.lastRefill = now

	// Check if we can consume a token
	if tb.tokens >= 1.0 {
		tb.tokens -= 1.0
		return true
	}
	return false
}

// RateLimiter manages rate limiters per subscription.
type RateLimiter struct {
	config  RateLimitConfig
	buckets map[string]*TokenBucket
	mu      sync.RWMutex
}

// NewRateLimiter creates a new rate limiter with the given configuration.
func NewRateLimiter(config RateLimitConfig) *RateLimiter {
	return &RateLimiter{
		config:  config,
		buckets: make(map[string]*TokenBucket),
	}
}

// Allow checks if a request for the given subscription is allowed.
func (rl *RateLimiter) Allow(subscriptionID string) bool {
	if !rl.config.Enabled {
		return true
	}

	rl.mu.RLock()
	bucket, exists := rl.buckets[subscriptionID]
	rl.mu.RUnlock()

	if !exists {
		rl.mu.Lock()
		// Double-check after acquiring write lock
		if bucket, exists = rl.buckets[subscriptionID]; !exists {
			bucket = NewTokenBucket(float64(rl.config.BurstSize), rl.config.RequestsPerSecond)
			rl.buckets[subscriptionID] = bucket
		}
		rl.mu.Unlock()
	}

	return bucket.Allow()
}

// RateLimit returns a middleware that rate limits requests by subscription ID.
// The subscription ID is expected in the URL parameter "subscription_id".
func RateLimit(limiter *RateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		if limiter == nil || !limiter.config.Enabled {
			c.Next()
			return
		}

		subscriptionID := c.Param("subscription_id")
		if subscriptionID == "" {
			// No subscription ID, skip rate limiting
			c.Next()
			return
		}

		if !limiter.Allow(subscriptionID) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":           "rate limit exceeded",
				"code":            "RATE_LIMIT_EXCEEDED",
				"subscription_id": subscriptionID,
				"retry_after":     1, // Suggest retry after 1 second
			})
			return
		}

		c.Next()
	}
}
