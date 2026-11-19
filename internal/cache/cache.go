// Package cache fingerprints a group's declared inputs and persists the
// result so unchanged groups can be skipped on subsequent runs.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/quike/keepup/internal/config"
	"github.com/quike/keepup/internal/pathglob"
	"github.com/quike/keepup/internal/result"
)

// Entry is a persisted cache record for one group.
type Entry struct {
	Fingerprint string           `json:"fingerprint"`
	Result      result.RunResult `json:"result"`
	// Commands records the expanded (post-template) command list that produced
	// Result — resolved values, not the config's template text. This field is
	// informational only: the Fingerprint (not Commands) decides cache hits.
	Commands  []config.CommandSpec `json:"commands"`
	UpdatedAt time.Time            `json:"updatedAt"`
}

// Store loads and saves cache entries keyed by group name.
type Store interface {
	Load(group string) (*Entry, bool)
	Save(group string, e *Entry) error
}

// Inputs is everything a group's fingerprint is computed from.
type Inputs struct {
	Spec *config.Cache
	// Root is the directory relative reads globs resolve against (empty means
	// the process working directory). Matches are hashed under their
	// root-relative path, so the fingerprint does not depend on where the
	// project lives.
	Root string
	// Shell is the group's shell:, folded in for shell-form entries only.
	Shell string
	// GlobalEnv and GroupEnv are the declared env: layers each command's own
	// env: is merged over, with the same precedence the runner uses.
	GlobalEnv map[string]string
	GroupEnv  map[string]string
	Commands  []config.CommandSpec
	// CacheDir is where entries are stored. Paths under it never count as
	// inputs, so a broad reads glob cannot feed the cache its own entries.
	CacheDir string
}

// Compute returns a content fingerprint for a group's inputs. The fingerprint
// changes when the method, anything about a command in the group's list
// except silent, the declared env any command runs with, or any matched input
// file outside the cache dir changes. For shell-form entries the fingerprint
// also changes when the shell program changes. A glob that matches nothing
// contributes nothing, so adding the first matching file naturally changes
// the fingerprint.
func Compute(in Inputs) (string, error) {
	h := sha256.New()
	// Salt with the full command list and method so any changed command (or a
	// form change: argv vs shell) busts the cache even when inputs are
	// identical. v4 marks the format that folds in the merged global, group,
	// and command env; bumping it makes every older entry miss once.
	// The \x00/\x01/\x02 sentinel encoding assumes config values are free of
	// these control bytes (user's own config; a collision only mis-caches their
	// own group).
	fmt.Fprintf(h, "v4\x00%s\x00", in.Spec.Method)
	for _, c := range in.Commands {
		hashCommand(h, c, in.Shell, config.DeclaredEnv(in.GlobalEnv, in.GroupEnv, c.Env))
	}

	files, err := resolveGlobs(in.Root, in.Spec.Reads)
	if err != nil {
		return "", err
	}
	files, err = excludeDir(in.Root, files, in.CacheDir)
	if err != nil {
		return "", err
	}
	for _, f := range files {
		if err := hashFile(h, in.Spec.Method, in.Root, f); err != nil {
			return "", err
		}
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// hashCommand folds one command entry, and the env it runs with, into h.
func hashCommand(h io.Writer, c config.CommandSpec, shell string, env map[string]string) {
	fmt.Fprintf(h, "%s\x00%t\x00", c.Command, c.IsShell)
	if c.IsShell {
		fmt.Fprintf(h, "%s\x00", shell)
	}
	for _, p := range c.Params {
		fmt.Fprintf(h, "%s\x01", p)
	}
	// Emitted only when set, so adding a new optional key never changes the
	// fingerprint of a config that does not use it.
	if c.Dir != "" {
		fmt.Fprintf(h, "%s\x03", c.Dir)
	}
	for _, k := range slices.Sorted(maps.Keys(env)) {
		fmt.Fprintf(h, "%s\x04%s\x03", k, env[k])
	}
	// Distinct markers: the two policies change whether the group
	// succeeds, so they must never hash alike.
	if c.ContinueOnError {
		fmt.Fprintf(h, "coe\x03")
	}
	if c.Always {
		fmt.Fprintf(h, "alw\x03")
	}
	fmt.Fprintf(h, "\x02")
}

// WritesPresent reports whether every declared output glob matches at least
// one existing path. A missing output invalidates a would-be cache hit.
// Relative globs resolve against root, as in Compute.
func WritesPresent(root string, spec *config.Cache) bool {
	for _, pattern := range spec.Writes {
		matches, err := resolveGlobs(root, []string{pattern})
		if err != nil || len(matches) == 0 {
			return false
		}
	}
	return true
}

// hashFile hashes one matched input. path is the label written into the hash;
// the file itself is read at path joined onto root.
func hashFile(h io.Writer, method config.CacheMethod, root, path string) error {
	file := onDisk(root, path)
	info, err := os.Stat(file)
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
		f, err := os.Open(file) //nolint:gosec // path comes from user-declared globs
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
// of matching paths (see pathglob.Glob: relative patterns yield root-relative
// matches, absolute patterns absolute ones).
func resolveGlobs(root string, patterns []string) ([]string, error) {
	set := make(map[string]struct{})
	for _, pattern := range patterns {
		matches, err := pathglob.Glob(root, pattern)
		if err != nil {
			return nil, err
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

// excludeDir drops every match whose on-disk location is at or below dir.
// Locations are compared in absolute, cleaned form, so root-relative and
// absolute matches are both checked against a relative or absolute dir. An
// empty dir excludes nothing.
func excludeDir(root string, matches []string, dir string) ([]string, error) {
	if dir == "" {
		return matches, nil
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve cache dir %q: %w", dir, err)
	}
	kept := make([]string, 0, len(matches))
	for _, m := range matches {
		absPath, err := filepath.Abs(onDisk(root, m))
		if err != nil {
			return nil, fmt.Errorf("resolve input %q: %w", m, err)
		}
		if !isWithin(absPath, absDir) {
			kept = append(kept, m)
		}
	}
	return kept, nil
}

// isWithin reports whether path is dir itself or lies below it. Both paths
// must be absolute and clean.
func isWithin(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// onDisk returns where a resolveGlobs match lives: root-relative matches are
// joined onto root (a literal path, never a pattern); absolute ones stand.
func onDisk(root, match string) string {
	if filepath.IsAbs(match) {
		return match
	}
	return filepath.Join(root, match)
}
