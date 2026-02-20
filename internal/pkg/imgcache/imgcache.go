package imgcache

import (
	"sync"
)

// Store provides a thread-safe in-memory cache for images (byte slices).
type Store struct {
	mu    sync.RWMutex
	cache map[string][]byte
}

// New creates a new in-memory image store.
func New() *Store {
	return &Store{
		cache: make(map[string][]byte),
	}
}

// Set stores an image with the given key.
func (s *Store) Set(key string, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache[key] = data
}

// Get retrieves an image by key. Returns nil and false if not found.
func (s *Store) Get(key string) ([]byte, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, ok := s.cache[key]
	return data, ok
}

// Delete removes an image from the cache by key.
func (s *Store) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.cache, key)
}
