package middleware

import (
	"context"
	"log/slog"
	"math"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// RateLimitConfig holds configuration for rate limiting.
type RateLimitConfig struct {
	// RequestsPerSecond is the sustained rate allowed per key.
	RequestsPerSecond float64
	// BurstSize is the maximum burst allowed per key.
	BurstSize int
	// Enabled controls whether rate limiting is active.
	Enabled bool
	// IdleTTL is how long an unused bucket survives before the janitor evicts it.
	IdleTTL time.Duration
	// MaxBuckets caps memory; when full, the least recently used bucket is evicted.
	MaxBuckets int
}

const (
	// DefaultRequestsPerSecond is 10 requests per second.
	DefaultRequestsPerSecond float64 = 10.0
	// DefaultBurstSize allows short bursts of traffic.
	DefaultBurstSize int = 20
	// DefaultBucketIdleTTL evicts buckets idle for longer than this.
	DefaultBucketIdleTTL = 10 * time.Minute
	// DefaultMaxBuckets is the hard cap on tracked keys.
	DefaultMaxBuckets = 10_000
)

// LoadRateLimitConfig loads rate limit configuration from environment variables:
//   - INGEST_RATE_LIMIT_RPS: requests per second per subscription (default 10)
//   - INGEST_RATE_LIMIT_BURST: burst size (default 20)
//   - INGEST_RATE_LIMIT_ENABLED: enable/disable (default true)
func LoadRateLimitConfig() RateLimitConfig {
	config := RateLimitConfig{
		RequestsPerSecond: DefaultRequestsPerSecond,
		BurstSize:         DefaultBurstSize,
		Enabled:           true,
		IdleTTL:           DefaultBucketIdleTTL,
		MaxBuckets:        DefaultMaxBuckets,
	}
	if s := os.Getenv("INGEST_RATE_LIMIT_RPS"); s != "" {
		if rps, err := strconv.ParseFloat(s, 64); err == nil && rps > 0 {
			config.RequestsPerSecond = rps
		} else {
			slog.Warn("invalid INGEST_RATE_LIMIT_RPS; using default", "value", s, "default", DefaultRequestsPerSecond)
		}
	}
	if s := os.Getenv("INGEST_RATE_LIMIT_BURST"); s != "" {
		if burst, err := strconv.Atoi(s); err == nil && burst > 0 {
			config.BurstSize = burst
		} else {
			slog.Warn("invalid INGEST_RATE_LIMIT_BURST; using default", "value", s, "default", DefaultBurstSize)
		}
	}
	if s := os.Getenv("INGEST_RATE_LIMIT_ENABLED"); s != "" {
		config.Enabled = s == "true" || s == "1"
	}
	if !config.Enabled {
		slog.Warn("ingest rate limiting is disabled")
	}
	return config
}

// TokenBucket implements a token bucket. It is not safe for concurrent use on its
// own; RateLimiter serializes access.
type TokenBucket struct {
	tokens     float64
	maxTokens  float64
	refillRate float64 // tokens per second
	lastRefill time.Time
	lastUsed   time.Time
}

// NewTokenBucket creates a full token bucket.
func NewTokenBucket(maxTokens, refillRate float64) *TokenBucket {
	return newTokenBucketAt(maxTokens, refillRate, time.Now())
}

func newTokenBucketAt(maxTokens, refillRate float64, now time.Time) *TokenBucket {
	return &TokenBucket{tokens: maxTokens, maxTokens: maxTokens, refillRate: refillRate, lastRefill: now, lastUsed: now}
}

// take consumes a token if available. Otherwise it reports how long until one is.
func (tb *TokenBucket) take(now time.Time) (bool, time.Duration) {
	// A caller's clock reading can predate lastRefill (it was taken before a lock);
	// never let negative elapsed time drain tokens.
	if elapsed := now.Sub(tb.lastRefill); elapsed > 0 {
		tb.tokens = math.Min(tb.maxTokens, tb.tokens+elapsed.Seconds()*tb.refillRate)
		tb.lastRefill = now
	}
	tb.lastUsed = now
	if tb.tokens >= 1 {
		tb.tokens--
		return true, 0
	}
	wait := time.Duration((1 - tb.tokens) / tb.refillRate * float64(time.Second))
	return false, wait
}

// Allow reports whether a request may proceed and consumes a token if so.
func (tb *TokenBucket) Allow() bool {
	ok, _ := tb.take(time.Now())
	return ok
}

// RateLimiter tracks one token bucket per key, bounded in count and idle time.
type RateLimiter struct {
	config  RateLimitConfig
	mu      sync.Mutex
	buckets map[string]*TokenBucket
}

// NewRateLimiter creates a rate limiter. Call StartJanitor to evict idle buckets.
func NewRateLimiter(config RateLimitConfig) *RateLimiter {
	if config.IdleTTL <= 0 {
		config.IdleTTL = DefaultBucketIdleTTL
	}
	if config.MaxBuckets <= 0 {
		config.MaxBuckets = DefaultMaxBuckets
	}
	return &RateLimiter{config: config, buckets: make(map[string]*TokenBucket)}
}

// Allow reports whether a request for key may proceed.
func (rl *RateLimiter) Allow(key string) bool {
	ok, _ := rl.allow(key)
	return ok
}

// allow returns whether key may proceed and, if not, how long until it may.
func (rl *RateLimiter) allow(key string) (bool, time.Duration) {
	if !rl.config.Enabled {
		return true, 0
	}
	now := time.Now()
	rl.mu.Lock()
	defer rl.mu.Unlock()
	b, ok := rl.buckets[key]
	if !ok {
		if len(rl.buckets) >= rl.config.MaxBuckets {
			rl.evictFullestLocked(now)
		}
		b = newTokenBucketAt(float64(rl.config.BurstSize), rl.config.RequestsPerSecond, now)
		rl.buckets[key] = b
	}
	return b.take(now)
}

// evictFullestLocked drops the bucket holding the most tokens (after refill),
// oldest first on ties. A re-created bucket starts full, so evicting the fullest
// one forgives the least rate-limit debt: flooding the limiter with random keys
// evicts the attacker's own near-full buckets, never a drained bucket that is
// actively limiting someone. O(n), only at the cap; n <= MaxBuckets.
func (rl *RateLimiter) evictFullestLocked(now time.Time) {
	var victim string
	var bestTokens float64
	var bestUsed time.Time
	for k, b := range rl.buckets {
		t := min(b.maxTokens, b.tokens+now.Sub(b.lastRefill).Seconds()*b.refillRate)
		if victim == "" || t > bestTokens || (t == bestTokens && b.lastUsed.Before(bestUsed)) {
			victim, bestTokens, bestUsed = k, t, b.lastUsed
		}
	}
	delete(rl.buckets, victim)
}

// Len returns the number of tracked buckets.
func (rl *RateLimiter) Len() int {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return len(rl.buckets)
}

// Sweep evicts buckets idle since before now-IdleTTL and returns how many it removed.
func (rl *RateLimiter) Sweep(now time.Time) int {
	cutoff := now.Add(-rl.config.IdleTTL)
	rl.mu.Lock()
	defer rl.mu.Unlock()
	n := 0
	for k, b := range rl.buckets {
		if b.lastUsed.Before(cutoff) {
			delete(rl.buckets, k)
			n++
		}
	}
	return n
}

// StartJanitor sweeps idle buckets every interval until ctx is done.
// A non-positive interval defaults to IdleTTL/2.
func (rl *RateLimiter) StartJanitor(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = rl.config.IdleTTL / 2
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				rl.Sweep(now)
			}
		}
	}()
}

// RateLimit rate limits requests by the "subscription_id" URL parameter and sets
// Retry-After (whole seconds, at least 1) on 429 responses.
func RateLimit(limiter *RateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.Param("subscription_id")
		if limiter == nil || key == "" {
			c.Next()
			return
		}
		ok, wait := limiter.allow(key)
		if !ok {
			secs := max(1, int(math.Ceil(wait.Seconds())))
			c.Header("Retry-After", strconv.Itoa(secs))
			AbortWithError(c, http.StatusTooManyRequests, CodeRateLimited, "rate limit exceeded")
			return
		}
		c.Next()
	}
}
