package config

import "path/filepath"

// ResolvePath anchors a config-relative path to the config's directory.
// Absolute and empty paths are returned unchanged, as is every path of a
// config with no Dir (parsed from bytes), which then resolves against the
// process working directory.
func (c *Config) ResolvePath(p string) string {
	if p == "" || c.Dir == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(c.Dir, p)
}

// CommandDir returns the directory a command runs in: its own dir: when set,
// otherwise settings.working-dir, otherwise the config's directory. Both
// settings resolve against the config's directory. An empty result means the
// process working directory.
func (c *Config) CommandDir(dir string) string {
	if dir != "" {
		return c.ResolvePath(dir)
	}
	if c.Settings.WorkingDir != "" {
		return c.ResolvePath(c.Settings.WorkingDir)
	}
	return c.Dir
}

// CachePath returns the resolved cache directory, defaulting to
// DefaultCacheDir.
func (c *Config) CachePath() string {
	dir := c.Settings.CacheDir
	if dir == "" {
		dir = DefaultCacheDir
	}
	return c.ResolvePath(dir)
}
