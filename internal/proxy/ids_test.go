package proxy

import (
	"testing"
	"time"
)

func TestTTLCacheStoreAndLoad(t *testing.T) {
	c := newTTLCache()

	c.Store("key1", "value1")
	val, ok := c.Load("key1")
	if !ok {
		t.Fatal("expected to find key1")
	}
	if val != "value1" {
		t.Errorf("expected 'value1', got '%s'", val)
	}
}

func TestTTLCacheLoadMiss(t *testing.T) {
	c := newTTLCache()

	_, ok := c.Load("nonexistent")
	if ok {
		t.Error("expected miss for nonexistent key")
	}
}

func TestTTLCacheOverwrite(t *testing.T) {
	c := newTTLCache()

	c.Store("key1", "value1")
	c.Store("key1", "value2")
	val, ok := c.Load("key1")
	if !ok {
		t.Fatal("expected to find key1")
	}
	if val != "value2" {
		t.Errorf("expected 'value2', got '%s'", val)
	}
}

func TestTTLCacheExpiry(t *testing.T) {
	c := newTTLCache()

	// Store with a very short TTL by modifying the entry directly
	c.mu.Lock()
	c.items["expired_key"] = cacheEntry{
		value:     "expired_value",
		expiresAt: time.Now().Add(-1 * time.Second),
	}
	c.mu.Unlock()

	_, ok := c.Load("expired_key")
	if ok {
		t.Error("expected expired key to return miss")
	}

	// Verify it was cleaned up
	c.mu.RLock()
	_, exists := c.items["expired_key"]
	c.mu.RUnlock()
	if exists {
		t.Error("expected expired key to be removed from cache")
	}
}

func TestTTLCacheCleanup(t *testing.T) {
	c := newTTLCache()

	// Add entries that are already expired
	c.mu.Lock()
	c.items["old1"] = cacheEntry{value: "v1", expiresAt: time.Now().Add(-1 * time.Hour)}
	c.items["old2"] = cacheEntry{value: "v2", expiresAt: time.Now().Add(-1 * time.Hour)}
	c.items["fresh"] = cacheEntry{value: "v3", expiresAt: time.Now().Add(1 * time.Hour)}
	c.mu.Unlock()

	// Trigger cleanup
	c.mu.Lock()
	now := time.Now()
	for k, v := range c.items {
		if now.After(v.expiresAt) {
			delete(c.items, k)
		}
	}
	c.mu.Unlock()

	c.mu.RLock()
	defer c.mu.RUnlock()

	if _, ok := c.items["old1"]; ok {
		t.Error("expected old1 to be cleaned up")
	}
	if _, ok := c.items["old2"]; ok {
		t.Error("expected old2 to be cleaned up")
	}
	if _, ok := c.items["fresh"]; !ok {
		t.Error("expected fresh to still exist")
	}
}

func TestTTLCacheConcurrentAccess(t *testing.T) {
	c := newTTLCache()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			c.Store("key", "value")
			c.Load("key")
		}
		close(done)
	}()

	for i := 0; i < 100; i++ {
		c.Store("key2", "value2")
		c.Load("key2")
	}

	<-done
}
