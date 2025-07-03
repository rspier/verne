package cache

import (
	"sync"
	"time"
)

// CacheEntry stores the cached value and its expiration time.
type CacheEntry struct {
	Value     interface{}
	ExpiresAt time.Time
}

// Cache is a simple in-memory cache with time-based expiration.
type Cache struct {
	items map[string]CacheEntry
	mutex sync.RWMutex
}

// NewCache creates a new Cache instance.
func NewCache() *Cache {
	return &Cache{
		items: make(map[string]CacheEntry),
	}
}

// Get retrieves an item from the cache.
// It returns the item's value and true if found and not expired.
// Otherwise, it returns nil and false. Expired items are deleted on access.
func (c *Cache) Get(key string) (interface{}, bool) {
	c.mutex.RLock()
	entry, found := c.items[key]
	c.mutex.RUnlock()

	if !found {
		return nil, false
	}

	if time.Now().After(entry.ExpiresAt) {
		// Item has expired, delete it (write lock needed for deletion)
		c.mutex.Lock()
		delete(c.items, key) // Safe even if another goroutine deleted it in the meantime
		c.mutex.Unlock()
		return nil, false
	}

	return entry.Value, true
}

// Set adds an item to the cache with a specified duration.
// If duration is 0 or negative, the item is considered to have no expiration (or expires immediately, depending on interpretation).
// For simplicity, a duration <= 0 means it's not cached effectively or treated as already expired.
// It's better to use positive durations.
func (c *Cache) Set(key string, value interface{}, duration time.Duration) {
	if duration <= 0 { // Do not cache if duration is not positive
		return
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.items[key] = CacheEntry{
		Value:     value,
		ExpiresAt: time.Now().Add(duration),
	}
}

// Delete removes an item from the cache.
func (c *Cache) Delete(key string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	delete(c.items, key)
}

// Flush removes all items from the cache.
func (c *Cache) Flush() {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.items = make(map[string]CacheEntry)
}
