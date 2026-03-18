package tools

import (
	"context"
	"sync"
)

// sessionCacheRegistry is the global registry of session caches.
var sessionCacheRegistry = &cacheRegistry{
	caches: make(map[string]*SessionCache),
}

type cacheRegistry struct {
	mu     sync.RWMutex
	caches map[string]*SessionCache
}

// RegisterSessionCache creates and registers a new cache for a session.
func RegisterSessionCache(sessionID string) *SessionCache {
	sessionCacheRegistry.mu.Lock()
	defer sessionCacheRegistry.mu.Unlock()

	cache := NewSessionCache(sessionID)
	sessionCacheRegistry.caches[sessionID] = cache
	return cache
}

// GetSessionCacheByID retrieves a session cache by session ID.
func GetSessionCacheByID(sessionID string) (*SessionCache, bool) {
	sessionCacheRegistry.mu.RLock()
	defer sessionCacheRegistry.mu.RUnlock()

	cache, ok := sessionCacheRegistry.caches[sessionID]
	return cache, ok
}

// UnregisterSessionCache removes and clears a session cache.
func UnregisterSessionCache(sessionID string) {
	sessionCacheRegistry.mu.Lock()
	defer sessionCacheRegistry.mu.Unlock()

	if cache, ok := sessionCacheRegistry.caches[sessionID]; ok {
		cache.Clear()
		delete(sessionCacheRegistry.caches, sessionID)
	}
}

// GetSessionCache retrieves the session cache from context.
func GetSessionCache(ctx context.Context) *SessionCache {
	if v := ctx.Value(SessionCacheContextKey); v != nil {
		if cache, ok := v.(*SessionCache); ok {
			return cache
		}
	}
	return nil
}
