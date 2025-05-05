package cache

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/redis/go-redis/v9"
)

type RedisSubscriptionCache struct {
    client *redis.Client
    ttl    time.Duration
}

func NewRedisSubscriptionCache(redisURL string, ttl time.Duration) *RedisSubscriptionCache {
    opts, err := redis.ParseURL(redisURL)
    if err != nil {
        log.Printf("Invalid REDIS_URL: %v", err)
        return &RedisSubscriptionCache{client: nil, ttl: ttl}
    }
    client := redis.NewClient(opts)
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()
    if err := client.Ping(ctx).Err(); err != nil {
        log.Printf("Redis connection failed: %v", err)
    } else {
        log.Println("Redis connected successfully")
    }
    return &RedisSubscriptionCache{client: client, ttl: ttl}
}

func (c *RedisSubscriptionCache) Get(id string) (database.Subscription, bool) {
    ctx := context.Background()
    val, err := c.client.Get(ctx, id).Result()
    if err != nil {
        return database.Subscription{}, false
    }
    var sub database.Subscription
    _ = json.Unmarshal([]byte(val), &sub)       
    return sub, true
}

func (c *RedisSubscriptionCache) Set(id string, sub database.Subscription) {
    ctx := context.Background()
    b, _ := json.Marshal(sub)
    c.client.Set(ctx, id, b, c.ttl)
}

func (c *RedisSubscriptionCache) Del(id string) {
    ctx := context.Background()
    c.client.Del(ctx, id)
}