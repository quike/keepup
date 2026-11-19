// Package configfile decides which keepup config file a command loads.
//
// Lookup order, first match wins:
//  1. an explicit path (the -c flag), taken as given;
//  2. the first of Names found in the current directory, then in each parent
//     up to the filesystem root;
//  3. the global file, ~/.config/keepup/keepup.yml.
package configfile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Names are the file names searched in each directory, in priority order.
var Names = []string{"keepup.yml", "keepup.yaml", ".keepup.yml"}

// ErrNotFound reports that discovery found no config file anywhere.
var ErrNotFound = errors.New("no keepup config found")

// Locator resolves the config file path. Its environment is injected so the
// lookup can be tested against an in-memory filesystem; NewLocator wires the
// real one.
type Locator struct {
	Stat func(name string) (fs.FileInfo, error)
	Cwd  func() (string, error)
	Home func() (string, error)
}

// NewLocator returns a Locator backed by the process environment.
func NewLocator() *Locator {
	return &Locator{Stat: os.Stat, Cwd: os.Getwd, Home: os.UserHomeDir}
}

// Locate returns the absolute path of the config file to load. A non-empty
// explicit path always wins and need not exist (loading reports that); a
// leading "~/" expands to the home directory and a relative path resolves
// against the current directory.
func (l *Locator) Locate(explicit string) (string, error) {
	if explicit != "" {
		return l.absolute(explicit)
	}
	cwd, err := l.Cwd()
	if err != nil {
		return "", fmt.Errorf("determine current directory: %w", err)
	}
	found, err := l.searchUpward(cwd)
	if err != nil || found != "" {
		return found, err
	}
	global, err := l.GlobalPath()
	if err != nil {
		return "", err
	}
	ok, err := l.isFile(global)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("%w: searched %s and its parents for %s, and %s does not exist (run `keepup init`)",
			ErrNotFound, cwd, strings.Join(Names, ", "), global)
	}
	return global, nil
}

// GlobalPath returns the per-user fallback config path.
func (l *Locator) GlobalPath() (string, error) {
	home, err := l.Home()
	if err != nil {
		return "", fmt.Errorf("determine home dir: %w", err)
	}
	return filepath.Join(home, ".config", "keepup", "keepup.yml"), nil
}

// searchUpward walks from dir to the filesystem root and returns the first
// config file it finds, or "" when there is none.
func (l *Locator) searchUpward(dir string) (string, error) {
	for {
		for _, name := range Names {
			candidate := filepath.Join(dir, name)
			ok, err := l.isFile(candidate)
			if err != nil {
				return "", err
			}
			if ok {
				return candidate, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}
		dir = parent
	}
}

// isFile reports whether path exists and is not a directory. Only a missing
// path is a quiet "no"; any other stat failure (e.g. permission denied) is
// surfaced so a broken directory cannot silently select a farther config.
func (l *Locator) isFile(path string) (bool, error) {
	info, err := l.Stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("stat %s: %w", path, err)
	}
	return !info.IsDir(), nil
}

func (l *Locator) absolute(path string) (string, error) {
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		home, err := l.Home()
		if err != nil {
			return "", fmt.Errorf("determine home dir: %w", err)
		}
		return filepath.Join(home, rest), nil
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	cwd, err := l.Cwd()
	if err != nil {
		return "", fmt.Errorf("determine current directory: %w", err)
	}
	return filepath.Join(cwd, path), nil
}
