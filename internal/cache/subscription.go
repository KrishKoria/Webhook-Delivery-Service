package cache

import (
	"sync"
	"time"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
)

type cachedSubscription struct {
    Subscription database.Subscription
    ExpiresAt    time.Time
}

type SubscriptionCache struct {
    mu    sync.RWMutex
    items map[string]cachedSubscription
    ttl   time.Duration
}

func NewSubscriptionCache(ttl time.Duration) *SubscriptionCache {
    return &SubscriptionCache{
        items: make(map[string]cachedSubscription),
        ttl:   ttl,
    }
}

func (c *SubscriptionCache) Get(id string) (database.Subscription, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    item, ok := c.items[id]
    if !ok || time.Now().After(item.ExpiresAt) {
        return database.Subscription{}, false
    }
    return item.Subscription, true
}

func (c *SubscriptionCache) Set(id string, sub database.Subscription) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.items[id] = cachedSubscription{
        Subscription: sub,
        ExpiresAt:    time.Now().Add(c.ttl),
    }
}