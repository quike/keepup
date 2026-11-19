// Package pathglob matches config globs relative to a root directory.
//
// The root is never spliced into a pattern: it is a literal directory, so a
// project living under a path with glob metacharacters (e.g. "~/src/[work]")
// matches exactly like any other. Patterns support "**" via doublestar.
// Absolute patterns ignore the root; an empty root means the process working
// directory.
package pathglob

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/bmatcuk/doublestar/v4"
)

// Glob returns the paths matching pattern. Matches of a relative pattern are
// relative to root (they may start with ".." when the pattern does); matches
// of an absolute pattern are absolute.
func Glob(root, pattern string) ([]string, error) {
	if filepath.IsAbs(pattern) {
		matches, err := doublestar.FilepathGlob(pattern)
		if err != nil {
			return nil, fmt.Errorf("bad glob %q: %w", pattern, err)
		}
		return matches, nil
	}
	// The literal prefix (which may climb out with "..") becomes part of the
	// directory searched, because an fs.FS cannot address paths above it.
	base, rest := doublestar.SplitPattern(filepath.ToSlash(pattern))
	dir := filepath.Join(root, filepath.FromSlash(base))
	if dir == "" {
		dir = "."
	}
	matches, err := doublestar.Glob(os.DirFS(dir), rest)
	if err != nil {
		return nil, fmt.Errorf("bad glob %q: %w", pattern, err)
	}
	for i, m := range matches {
		matches[i] = filepath.Join(filepath.FromSlash(base), filepath.FromSlash(m))
	}
	return matches, nil
}

// Match reports whether path (absolute, or relative to the working directory)
// satisfies pattern. A relative pattern is compared against path's location
// relative to root.
func Match(root, pattern, path string) bool {
	target := filepath.Clean(path)
	if !filepath.IsAbs(pattern) {
		rel, err := relativeTo(root, target)
		if err != nil {
			return false
		}
		target = rel
	}
	ok, err := doublestar.PathMatch(filepath.Clean(pattern), target)
	return err == nil && ok
}

// relativeTo expresses path relative to root, making both absolute first so a
// relative event path and an absolute root (or vice versa) compare correctly.
func relativeTo(root, path string) (string, error) {
	if root == "" {
		root = "."
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Rel(absRoot, absPath)
}
