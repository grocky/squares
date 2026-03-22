package api

import (
	"sync"
	"time"

	"github.com/grocky/squares/internal/models"
)

type poolCacheEntry struct {
	pool         models.Pool
	roundConfigs []models.RoundConfig
	roundAxes    []models.Axis
	cachedAt     time.Time
}

type poolCache struct {
	mu      sync.RWMutex
	entries map[string]poolCacheEntry
	ttl     time.Duration
}

func newPoolCache(ttl time.Duration) *poolCache {
	return &poolCache{
		entries: make(map[string]poolCacheEntry),
		ttl:     ttl,
	}
}

func (c *poolCache) get(poolID string) (poolCacheEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[poolID]
	if !ok {
		return poolCacheEntry{}, false
	}
	if time.Since(entry.cachedAt) > c.ttl {
		return poolCacheEntry{}, false
	}
	return entry, true
}

func (c *poolCache) set(poolID string, entry poolCacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry.cachedAt = time.Now()
	c.entries[poolID] = entry
}

func (c *poolCache) invalidate(poolID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, poolID)
}
