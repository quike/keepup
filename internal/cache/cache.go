// Package cache fingerprints a group's declared inputs and persists the
// result so unchanged groups can be skipped on subsequent runs.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/quike/keepup/internal/config"
	"github.com/quike/keepup/internal/result"
)

// Entry is a persisted cache record for one group.
type Entry struct {
	Fingerprint string               `json:"fingerprint"`
	Result      result.RunResult     `json:"result"`
	Commands    []config.CommandSpec `json:"commands"`
	UpdatedAt   time.Time            `json:"updatedAt"`
}

// Store loads and saves cache entries keyed by group name.
type Store interface {
	Load(group string) (*Entry, bool)
	Save(group string, e *Entry) error
}

// Compute returns a content fingerprint for the given cache spec. The
// fingerprint changes when the method, any command/param/form in the group's
// command list, or any matched input file changes. For shell-form entries the
// fingerprint also changes when the shell program changes. A glob that matches
// nothing contributes nothing, so adding the first matching file naturally
// changes the fingerprint.
func Compute(spec *config.Cache, shell string, commands []config.CommandSpec) (string, error) {
	h := sha256.New()
	// Salt with the full command list and method so any changed command (or a
	// form change: argv vs shell) busts the cache even when inputs are
	// identical. v3 marks the multi-command fingerprint format.
	// The \x00/\x01/\x02 sentinel encoding assumes config values are free of
	// these control bytes (user's own config; a collision only mis-caches their
	// own group).
	fmt.Fprintf(h, "v3\x00%s\x00", spec.Method)
	for _, c := range commands {
		fmt.Fprintf(h, "%s\x00%t\x00", c.Command, c.IsShell)
		if c.IsShell {
			fmt.Fprintf(h, "%s\x00", shell)
		}
		for _, p := range c.Params {
			fmt.Fprintf(h, "%s\x01", p)
		}
		fmt.Fprintf(h, "\x02")
	}

	files, err := resolveGlobs(spec.Reads)
	if err != nil {
		return "", err
	}
	for _, f := range files {
		if err := hashFile(h, spec.Method, f); err != nil {
			return "", err
		}
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// WritesPresent reports whether every declared output glob matches at least
// one existing path. A missing output invalidates a would-be cache hit.
func WritesPresent(spec *config.Cache) bool {
	for _, pattern := range spec.Writes {
		matches, err := resolveGlobs([]string{pattern})
		if err != nil || len(matches) == 0 {
			return false
		}
	}
	return true
}

func hashFile(h io.Writer, method config.CacheMethod, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat input %q: %w", path, err)
	}
	if info.IsDir() {
		// Directories contribute their path + mtime; their files are matched
		// independently by the globs.
		fmt.Fprintf(h, "dir\x00%s\x00%d\x00", path, info.ModTime().UnixNano())
		return nil
	}
	switch method {
	case config.CacheMtime:
		fmt.Fprintf(h, "%s\x00%d\x00%d\x00", path, info.ModTime().UnixNano(), info.Size())
	default: // CacheHash
		f, err := os.Open(path) //nolint:gosec // path comes from user-declared globs
		if err != nil {
			return fmt.Errorf("open input %q: %w", path, err)
		}
		defer f.Close()
		fmt.Fprintf(h, "%s\x00", path)
		if _, err := io.Copy(h, f); err != nil {
			return fmt.Errorf("read input %q: %w", path, err)
		}
	}
	return nil
}

// resolveGlobs expands each pattern, returning a sorted, de-duplicated list
// of matching paths. Patterns support "**" via doublestar.
func resolveGlobs(patterns []string) ([]string, error) {
	set := make(map[string]struct{})
	for _, pattern := range patterns {
		matches, err := doublestar.FilepathGlob(pattern)
		if err != nil {
			return nil, fmt.Errorf("bad glob %q: %w", pattern, err)
		}
		for _, m := range matches {
			set[m] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for m := range set {
		out = append(out, m)
	}
	sort.Strings(out)
	return out, nil
}
