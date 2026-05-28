package engine

import (
	"maps"
	"sync"

	"github.com/quike/keepup/internal/result"
)

// OutputStore is a goroutine-safe key/value store of structured group results.
type OutputStore interface {
	Get(name string) (result.RunResult, bool)
	Set(name string, r result.RunResult)
	Snapshot() map[string]result.RunResult
}

// MemoryOutputStore is the default in-memory implementation.
type MemoryOutputStore struct {
	mu   sync.RWMutex
	data map[string]result.RunResult
}

// NewMemoryOutputStore returns an empty store.
func NewMemoryOutputStore() *MemoryOutputStore {
	return &MemoryOutputStore{data: make(map[string]result.RunResult)}
}

// Get returns the stored RunResult for name, if any.
func (s *MemoryOutputStore) Get(name string) (result.RunResult, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[name]
	return v, ok
}

// Set stores a RunResult under name.
//
//nolint:gocritic // value semantics are part of the OutputStore contract; pointer would alias caller storage
func (s *MemoryOutputStore) Set(name string, r result.RunResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[name] = r
}

// Snapshot returns a copy of the current state for safe iteration.
func (s *MemoryOutputStore) Snapshot() map[string]result.RunResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]result.RunResult, len(s.data))
	maps.Copy(out, s.data)
	return out
}
