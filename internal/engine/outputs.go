package engine

import (
	"maps"
	"sync"
)

// OutputStore is a goroutine-safe key/value store of group outputs.
type OutputStore interface {
	Get(name string) (string, bool)
	Set(name, value string)
	Snapshot() map[string]string
}

// MemoryOutputStore is the default in-memory implementation.
type MemoryOutputStore struct {
	mu   sync.RWMutex
	data map[string]string
}

// NewMemoryOutputStore returns an empty store.
func NewMemoryOutputStore() *MemoryOutputStore {
	return &MemoryOutputStore{data: make(map[string]string)}
}

func (s *MemoryOutputStore) Get(name string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[name]
	return v, ok
}

func (s *MemoryOutputStore) Set(name, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[name] = value
}

// Snapshot returns a copy of the current state for safe iteration.
func (s *MemoryOutputStore) Snapshot() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.data))
	maps.Copy(out, s.data)
	return out
}
