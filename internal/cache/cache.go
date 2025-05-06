package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/redis/go-redis/v9"
)

type RedisSubscriptionCache struct {
    client *redis.Client
    ttl    time.Duration
}

func NewRedisSubscriptionCache(redisURL string, ttl time.Duration) (*RedisSubscriptionCache, error) {
    opts, err := redis.ParseURL(redisURL)
    if err != nil {
        log.Printf("Invalid REDIS_URL: %v", err)
        return nil, fmt.Errorf("failed to parse Redis URL: %w", err)
    }
    client := redis.NewClient(opts)
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()
    if err := client.Ping(ctx).Err(); err != nil {
        log.Printf("Redis connection failed: %v", err)
        return nil, fmt.Errorf("failed to connect to Redis: %w", err)
    } else {
        log.Println("Redis connected successfully")
    }
    return &RedisSubscriptionCache{client: client, ttl: ttl}, nil
}

func (c *RedisSubscriptionCache) Get(id string) (database.Subscription, bool) {
    if c == nil || c.client == nil { 
        return database.Subscription{}, false
    }
    ctx := context.Background()
    val, err := c.client.Get(ctx, id).Result()
    if err != nil {
        if err == redis.Nil {
            log.Printf("Key %s does not exist in Redis", id)
        } 
        return database.Subscription{}, false
    }
    var sub database.Subscription
    if err := json.Unmarshal([]byte(val), &sub); err != nil {
        log.Printf("Error unmarshalling subscription from Redis for key %s: %v", id, err)
        return database.Subscription{}, false 
    }      
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