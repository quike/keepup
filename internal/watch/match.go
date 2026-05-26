// Package watch re-runs a callback when files matching a set of globs change.
//
// The matching and directory-resolution helpers are pure and unit-tested; the
// event loop consumes an injectable Source so it can be exercised without real
// filesystem timing.
package watch

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// Matches reports whether path satisfies any of the glob patterns. Patterns
// support "**" via doublestar and are matched with OS path separators.
func Matches(patterns []string, path string) bool {
	clean := filepath.Clean(path)
	for _, p := range patterns {
		if ok, err := doublestar.PathMatch(filepath.Clean(p), clean); err == nil && ok {
			return true
		}
	}
	return false
}

// globBase returns the longest leading path segment of a pattern that contains
// no glob metacharacters — the directory from which a recursive watch should
// start. For "src/**/*.go" it returns "src"; for "*.go" it returns ".". Absolute
// patterns keep their leading separator. A wholly-literal pattern resolves to
// its containing directory.
func globBase(pattern string) string {
	cleaned := filepath.Clean(pattern)
	sep := string(filepath.Separator)
	abs := strings.HasPrefix(cleaned, sep)

	parts := strings.Split(strings.TrimPrefix(cleaned, sep), sep)
	base := make([]string, 0, len(parts))
	hasGlob := false
	for _, part := range parts {
		if strings.ContainsAny(part, "*?[{") {
			hasGlob = true
			break
		}
		base = append(base, part)
	}

	joined := strings.Join(base, sep)
	if abs {
		joined = filepath.Clean(sep + joined)
	} else if joined == "" {
		joined = "."
	}

	if !hasGlob {
		// Wholly-literal pattern → watch its directory.
		return dirOf(joined)
	}
	if joined == "" {
		return "."
	}
	return joined
}

func dirOf(p string) string {
	d := filepath.Dir(p)
	if d == "" {
		return "."
	}
	return d
}

// ResolveWatchDirs returns the de-duplicated, sorted set of directories that
// must be watched so every file matched by patterns is observed. Each glob's
// base directory is walked recursively (to cover "**"). Missing bases are
// skipped rather than erroring — they may be created later.
func ResolveWatchDirs(patterns []string) ([]string, error) {
	set := map[string]struct{}{}
	for _, p := range patterns {
		base := globBase(p)
		err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				// A non-existent base is fine; stop walking this branch.
				if path == base {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				set[path] = struct{}{}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	out := make([]string, 0, len(set))
	for d := range set {
		out = append(out, d)
	}
	sort.Strings(out)
	return out, nil
}
