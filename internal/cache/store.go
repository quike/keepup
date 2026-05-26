package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileStore persists one JSON entry per group under a directory.
type FileStore struct {
	dir string
}

// NewFileStore returns a store rooted at dir. The directory is created lazily
// on the first Save.
func NewFileStore(dir string) *FileStore { return &FileStore{dir: dir} }

// Load returns the stored entry for a group, or (nil, false) if absent or
// unreadable.
func (s *FileStore) Load(group string) (*Entry, bool) {
	data, err := os.ReadFile(s.path(group))
	if err != nil {
		return nil, false
	}
	var e Entry
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, false
	}
	return &e, true
}

// Save writes the entry for a group, creating the cache directory if needed.
func (s *FileStore) Save(group string, e *Entry) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("create cache dir %q: %w", s.dir, err)
	}
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return fmt.Errorf("encode cache entry: %w", err)
	}
	if err := os.WriteFile(s.path(group), data, 0o600); err != nil {
		return fmt.Errorf("write cache entry: %w", err)
	}
	return nil
}

// path returns the on-disk file for a group, sanitizing the name so it is
// always a single safe filename component.
func (s *FileStore) path(group string) string {
	return filepath.Join(s.dir, sanitize(group)+".json")
}

func sanitize(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}
