// Package cache is a Redis read-through cache for subscriptions.
//
// Keys:
//   - whd:sub:<id>    JSON-encoded database.Subscription, with a TTL
//   - whd:subver:<id> invalidation counter, bumped by Del
//
// Invalidation is race-free: Del bumps the version before deleting the value,
// and GetOrLoad captures the version before reading the database and only
// writes back (inside a WATCH on the version key) if it is unchanged. A reader
// that loaded a row before a concurrent update can therefore never re-populate
// the cache with that stale row.
//
// Every method is safe on a nil *RedisSubscriptionCache (it behaves as an
// always-missing cache), and every Redis call is bounded by a 200ms timeout so
// a slow Redis degrades to database reads instead of stalling deliveries.
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/redis/go-redis/v9"
)

const (
	keyPrefix     = "whd:sub:"
	versionPrefix = "whd:subver:"
	// opTimeout bounds every Redis round-trip.
	opTimeout = 200 * time.Millisecond
	// versionTTL keeps invalidation counters from accumulating forever. It only
	// needs to outlive a single GetOrLoad, so a day is generous.
	versionTTL = 24 * time.Hour
)

// RedisSubscriptionCache caches subscriptions in Redis.
type RedisSubscriptionCache struct {
	client *redis.Client
	ttl    time.Duration
}

// NewRedisSubscriptionCache connects to redisURL and verifies the connection.
func NewRedisSubscriptionCache(redisURL string, ttl time.Duration) (*RedisSubscriptionCache, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse Redis URL: %w", err)
	}
	client := redis.NewClient(opts)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("connect to Redis: %w", err)
	}
	slog.Info("redis connected")
	return &RedisSubscriptionCache{client: client, ttl: ttl}, nil
}

func subKey(id string) string     { return keyPrefix + id }
func versionKey(id string) string { return versionPrefix + id }

func (c *RedisSubscriptionCache) enabled() bool { return c != nil && c.client != nil }

// Get returns the cached subscription, if present.
func (c *RedisSubscriptionCache) Get(id string) (database.Subscription, bool) {
	return c.get(context.Background(), id)
}

func (c *RedisSubscriptionCache) get(ctx context.Context, id string) (database.Subscription, bool) {
	if !c.enabled() {
		return database.Subscription{}, false
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	val, err := c.client.Get(ctx, subKey(id)).Bytes()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			slog.Warn("cache get failed", "subscription_id", id, "err", err)
		}
		return database.Subscription{}, false
	}
	var sub database.Subscription
	if err := json.Unmarshal(val, &sub); err != nil {
		slog.Warn("cache entry undecodable", "subscription_id", id, "err", err)
		return database.Subscription{}, false
	}
	return sub, true
}

// Set stores sub unconditionally. Prefer GetOrLoad, which cannot write stale data.
func (c *RedisSubscriptionCache) Set(id string, sub database.Subscription) {
	if !c.enabled() {
		return
	}
	b, err := json.Marshal(sub)
	if err != nil {
		slog.Warn("cache encode failed", "subscription_id", id, "err", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	if err := c.client.Set(ctx, subKey(id), b, c.ttl).Err(); err != nil {
		slog.Warn("cache set failed", "subscription_id", id, "err", err)
	}
}

// Del invalidates id: it bumps the version (so in-flight GetOrLoad calls will
// not write back) and then deletes the cached value.
func (c *RedisSubscriptionCache) Del(id string) {
	if !c.enabled() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	_, err := c.client.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.Incr(ctx, versionKey(id))
		p.Expire(ctx, versionKey(id), versionTTL)
		p.Del(ctx, subKey(id))
		return nil
	})
	if err != nil {
		slog.Warn("cache invalidate failed", "subscription_id", id, "err", err)
	}
}

// Ping checks the Redis connection health.
func (c *RedisSubscriptionCache) Ping(ctx context.Context) error {
	if !c.enabled() {
		return errors.New("cache not initialized")
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	return c.client.Ping(ctx).Err()
}

// errVersionChanged aborts a write-back after a concurrent invalidation.
var errVersionChanged = errors.New("subscription version changed")

// GetOrLoad returns the cached subscription or calls load and caches its result.
// Errors from load are returned unchanged (and nothing is cached). Redis
// failures are never returned: the cache just falls back to load.
func (c *RedisSubscriptionCache) GetOrLoad(ctx context.Context, id string, load func(ctx context.Context) (database.Subscription, error)) (database.Subscription, error) {
	if !c.enabled() {
		return load(ctx)
	}
	if sub, ok := c.get(ctx, id); ok {
		return sub, nil
	}

	verCtx, cancel := context.WithTimeout(ctx, opTimeout)
	version, err := c.client.Get(verCtx, versionKey(id)).Result()
	cancel()
	versionKnown := true
	if errors.Is(err, redis.Nil) {
		version = ""
	} else if err != nil {
		versionKnown = false // cannot prove freshness, so do not write back
	}

	sub, err := load(ctx)
	if err != nil || !versionKnown {
		return sub, err
	}
	c.setIfVersion(ctx, id, version, sub)
	return sub, nil
}

// setIfVersion writes sub only if the version key still equals version.
func (c *RedisSubscriptionCache) setIfVersion(ctx context.Context, id, version string, sub database.Subscription) {
	b, err := json.Marshal(sub)
	if err != nil {
		slog.Warn("cache encode failed", "subscription_id", id, "err", err)
		return
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	vk := versionKey(id)
	err = c.client.Watch(ctx, func(tx *redis.Tx) error {
		cur, err := tx.Get(ctx, vk).Result()
		if errors.Is(err, redis.Nil) {
			cur = ""
		} else if err != nil {
			return err
		}
		if cur != version {
			return errVersionChanged
		}
		_, err = tx.TxPipelined(ctx, func(p redis.Pipeliner) error {
			p.Set(ctx, subKey(id), b, c.ttl)
			return nil
		})
		return err
	}, vk)
	switch {
	case err == nil, errors.Is(err, errVersionChanged), errors.Is(err, redis.TxFailedErr):
		// Written, or skipped because an invalidation raced us; both are correct.
	default:
		slog.Warn("cache write-back failed", "subscription_id", id, "err", err)
	}
}
