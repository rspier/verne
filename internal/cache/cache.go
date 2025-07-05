package cache

import (
	"context" // Added for OTel
	"sync"
	"time"

	"go.opentelemetry.io/otel"           // Added for OTel
	"go.opentelemetry.io/otel/attribute" // Added for OTel
	"go.opentelemetry.io/otel/metric"    // Added for OTel metrics
	"go.opentelemetry.io/otel/trace"     // Added for trace.WithAttributes
	// semconv "go.opentelemetry.io/otel/semconv/v1.21.0" // For semantic conventions if needed
)

var (
	tracer      = otel.Tracer("nntp-web/internal/cache")
	meter       = otel.Meter("nntp-web/internal/cache")
	cacheHits   metric.Int64Counter
	cacheMisses metric.Int64Counter
)

func init() {
	var err error
	cacheHits, err = meter.Int64Counter(
		"cache.hits",
		metric.WithDescription("Number of cache hits."),
		metric.WithUnit("{hits}"),
	)
	if err != nil {
		otel.Handle(err) // Use global error handler
	}

	cacheMisses, err = meter.Int64Counter(
		"cache.misses",
		metric.WithDescription("Number of cache misses."),
		metric.WithUnit("{misses}"),
	)
	if err != nil {
		otel.Handle(err)
	}
}

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
// It now accepts a context for tracing.
func (c *Cache) Get(ctx context.Context, key string) (interface{}, bool) {
	ctx, span := tracer.Start(ctx, "Cache.Get", trace.WithAttributes(attribute.String("cache.key", key)))
	defer span.End()

	c.mutex.RLock()
	entry, found := c.items[key]
	c.mutex.RUnlock()

	if !found {
		if cacheMisses != nil {
			cacheMisses.Add(ctx, 1, metric.WithAttributes(attribute.String("cache.key", key)))
		}
		span.SetAttributes(attribute.Bool("cache.hit", false))
		return nil, false
	}

	if time.Now().After(entry.ExpiresAt) {
		// Item has expired, delete it (write lock needed for deletion)
		c.mutex.Lock()
		delete(c.items, key) // Safe even if another goroutine deleted it in the meantime
		c.mutex.Unlock()

		if cacheMisses != nil { // Expired is also a miss
			cacheMisses.Add(ctx, 1, metric.WithAttributes(attribute.String("cache.key", key), attribute.Bool("cache.expired", true)))
		}
		span.SetAttributes(attribute.Bool("cache.hit", false), attribute.Bool("cache.expired", true))
		return nil, false
	}

	if cacheHits != nil {
		cacheHits.Add(ctx, 1, metric.WithAttributes(attribute.String("cache.key", key)))
	}
	span.SetAttributes(attribute.Bool("cache.hit", true))
	return entry.Value, true
}

// Set adds an item to the cache with a specified duration.
// If duration is 0 or negative, the item is considered to have no expiration (or expires immediately, depending on interpretation).
// For simplicity, a duration <= 0 means it's not cached effectively or treated as already expired.
// It's better to use positive durations.
// It now accepts a context for tracing.
func (c *Cache) Set(ctx context.Context, key string, value interface{}, duration time.Duration) {
	_, span := tracer.Start(ctx, "Cache.Set",
		trace.WithAttributes(
			attribute.String("cache.key", key),
			attribute.Stringer("cache.duration", duration),
		),
	)
	defer span.End()

	if duration <= 0 { // Do not cache if duration is not positive
		span.SetAttributes(attribute.Bool("cache.stored", false), attribute.String("cache.store_reason", "non_positive_duration"))
		return
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.items[key] = CacheEntry{
		Value:     value,
		ExpiresAt: time.Now().Add(duration),
	}
	span.SetAttributes(attribute.Bool("cache.stored", true))
}

// Delete removes an item from the cache.
// It now accepts a context for tracing.
func (c *Cache) Delete(ctx context.Context, key string) {
	_, span := tracer.Start(ctx, "Cache.Delete", trace.WithAttributes(attribute.String("cache.key", key)))
	defer span.End()

	c.mutex.Lock()
	defer c.mutex.Unlock()
	delete(c.items, key)
}

// Flush removes all items from the cache.
// It now accepts a context for tracing.
func (c *Cache) Flush(ctx context.Context) {
	_, span := tracer.Start(ctx, "Cache.Flush")
	defer span.End()

	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.items = make(map[string]CacheEntry)
}
