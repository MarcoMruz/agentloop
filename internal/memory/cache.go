package memory

import (
	"sync"
	"time"
)

type cacheEntry struct {
	value     string
	expiresAt time.Time
}

type PromptCache struct {
	mu    sync.RWMutex
	store map[string]cacheEntry
}

func NewPromptCache() *PromptCache {
	return &PromptCache{store: make(map[string]cacheEntry)}
}

func (pc *PromptCache) Get(key string) string {
	pc.mu.RLock()
	defer pc.mu.RUnlock()
	entry, ok := pc.store[key]
	if !ok || time.Now().After(entry.expiresAt) { return "" }
	return entry.value
}

func (pc *PromptCache) Set(key string, value string, ttlSeconds int) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	pc.store[key] = cacheEntry{
		value:     value,
		expiresAt: time.Now().Add(time.Duration(ttlSeconds) * time.Second),
	}
}

func (pc *PromptCache) Delete(key string) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	delete(pc.store, key)
}
