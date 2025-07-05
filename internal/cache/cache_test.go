package cache

import (
	"fmt" // Ensured fmt is imported
	"sync"
	"testing"
	"time"
)

func TestCache_GetSet(t *testing.T) {
	cache := NewCache()
	key := "testKey"
	value := "testValue"
	duration := 100 * time.Millisecond

	ctx := context.Background() // Define context for tests

	// Test Set and Get
	cache.Set(ctx, key, value, duration)
	retVal, found := cache.Get(ctx, key)
	if !found {
		t.Errorf("Cache.Get: key %s not found after Set", key)
	}
	if retVal != value {
		t.Errorf("Cache.Get: got %v, want %v", retVal, value)
	}

	// Test expiration
	time.Sleep(duration + 50*time.Millisecond) // Wait for item to expire
	retVal, found = cache.Get(key)
	if found {
		t.Errorf("Cache.Get: key %s found after expiration, value: %v", key, retVal)
	}
	if retVal != nil {
		t.Errorf("Cache.Get: expected nil for expired key, got %v", retVal)
	}

	// Test Get non-existent key
	_, found = cache.Get("nonExistentKey")
	if found {
		t.Errorf("Cache.Get: found non-existent key")
	}
}

func TestCache_SetWithZeroOrNegativeDuration(t *testing.T) {
	cache := NewCache()
	key := "testKeyZero"
	value := "testValueZero"

	cache.Set(key, value, 0)
	_, found := cache.Get(key)
	if found {
		t.Errorf("Cache.Set with zero duration should not cache item")
	}

	keyNeg := "testKeyNegative"
	valueNeg := "testValueNegative"
	cache.Set(keyNeg, valueNeg, -5*time.Second)
	_, found = cache.Get(keyNeg)
	if found {
		t.Errorf("Cache.Set with negative duration should not cache item")
	}
}

func TestCache_Delete(t *testing.T) {
	cache := NewCache()
	key := "deleteKey"
	value := "deleteValue"
	duration := 100 * time.Millisecond

	cache.Set(key, value, duration)
	_, found := cache.Get(key)
	if !found {
		t.Fatalf("Failed to set key for delete test")
	}

	cache.Delete(key)
	_, found = cache.Get(key)
	if found {
		t.Errorf("Cache.Delete: key %s found after delete", key)
	}
}

func TestCache_Flush(t *testing.T) {
	cache := NewCache()
	cache.Set("key1", "value1", 100*time.Millisecond)
	cache.Set("key2", "value2", 100*time.Millisecond)

	cache.Flush()
	if len(cache.items) != 0 {
		t.Errorf("Cache.Flush: expected cache to be empty, found %d items", len(cache.items))
	}
}

func TestCache_Concurrency(t *testing.T) {
	cache := NewCache()
	numGoroutines := 100
	numOpsPerGoroutine := 1000
	duration := 50 * time.Millisecond // Short duration to test expiry too

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(gID int) {
			defer wg.Done()
			for j := 0; j < numOpsPerGoroutine; j++ {
				key := fmt.Sprintf("key-%d-%d", gID, j) // fmt used here
				value := fmt.Sprintf("value-%d-%d", gID, j) // and here

				// Mix of Set and Get operations
				if j%2 == 0 {
					cache.Set(key, value, duration)
				} else {
					cache.Get(key) // Access to potentially trigger expiry deletion
				}
				if j % 100 == 0 { // Occasionally delete
					cache.Delete(fmt.Sprintf("key-%d-%d", gID, j/2)) // and here
				}
			}
		}(i)
	}
	wg.Wait()
	t.Logf("Concurrency test completed. Final cache size: %d", len(cache.items)) // and here
}
// End of file
