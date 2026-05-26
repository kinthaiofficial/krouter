package routing

import (
	"sync"
	"time"
)

const sessionTTL = 75 * time.Minute

type sessionEntry struct {
	state    SessionState
	lastSeen time.Time
}

// MemSessionStore is an in-memory SessionSource with a 75-minute TTL.
// Stale entries are evicted by a background goroutine. Call Close to stop it.
type MemSessionStore struct {
	mu      sync.Mutex
	entries map[string]*sessionEntry
	done    chan struct{}
}

// NewMemSessionStore creates a MemSessionStore and starts its cleanup goroutine.
func NewMemSessionStore() *MemSessionStore {
	m := &MemSessionStore{
		entries: make(map[string]*sessionEntry),
		done:    make(chan struct{}),
	}
	go m.evictLoop()
	return m
}

// Get returns the current SessionState for key. ok is false when none exists.
func (m *MemSessionStore) Get(key string) (SessionState, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[key]
	if !ok {
		return SessionState{}, false
	}
	return e.state, true
}

// Update atomically applies fn to the state for key.
func (m *MemSessionStore) Update(key string, fn func(*SessionState)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[key]
	if !ok {
		e = &sessionEntry{}
		m.entries[key] = e
	}
	fn(&e.state)
	e.lastSeen = time.Now()
}

// Close stops the background eviction goroutine.
func (m *MemSessionStore) Close() {
	close(m.done)
}

func (m *MemSessionStore) evictLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.evict()
		case <-m.done:
			return
		}
	}
}

func (m *MemSessionStore) evict() {
	cutoff := time.Now().Add(-sessionTTL)
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, e := range m.entries {
		if e.lastSeen.Before(cutoff) {
			delete(m.entries, k)
		}
	}
}
