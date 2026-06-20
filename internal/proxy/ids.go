package proxy

import (
	"sync"
	"time"
)

const (
	OpenAIIDPrefix      = "chatcmpl-"
	OpenAICallPrefix    = "call_"
	AnthropicIDPrefix   = "msg_"
	AnthropicToolPrefix = "toolu_"
)

const thoughtSignatureTTL = 30 * time.Minute

type cacheEntry struct {
	value     string
	expiresAt time.Time
}

type ttlCache struct {
	mu    sync.RWMutex
	items map[string]cacheEntry
}

func newTTLCache() *ttlCache {
	c := &ttlCache{
		items: make(map[string]cacheEntry),
	}
	go c.cleanup()
	return c
}

func (c *ttlCache) Store(key, value string) {
	c.mu.Lock()
	c.items[key] = cacheEntry{
		value:     value,
		expiresAt: time.Now().Add(thoughtSignatureTTL),
	}
	c.mu.Unlock()
}

func (c *ttlCache) Load(key string) (string, bool) {
	c.mu.RLock()
	entry, ok := c.items[key]
	c.mu.RUnlock()
	if !ok {
		return "", false
	}
	if time.Now().After(entry.expiresAt) {
		c.mu.Lock()
		delete(c.items, key)
		c.mu.Unlock()
		return "", false
	}
	return entry.value, true
}

func (c *ttlCache) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		c.mu.Lock()
		for k, v := range c.items {
			if now.After(v.expiresAt) {
				delete(c.items, k)
			}
		}
		c.mu.Unlock()
	}
}

var thoughtSignatureCache = newTTLCache()
