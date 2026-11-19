package configfile

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	home = "/home/u"
	cwd  = "/work/repo/sub"
)

// locatorOver builds a Locator whose filesystem is the given in-memory tree,
// rooted at "/", with fixed cwd and home directories.
func locatorOver(files ...string) *Locator {
	tree := fstest.MapFS{}
	for _, f := range files {
		tree[f[1:]] = &fstest.MapFile{}
	}
	return &Locator{
		Stat: func(name string) (fs.FileInfo, error) {
			rel := filepath.ToSlash(name)[1:]
			if rel == "" {
				rel = "."
			}
			return fs.Stat(tree, rel)
		},
		Cwd:  func() (string, error) { return cwd, nil },
		Home: func() (string, error) { return home, nil },
	}
}

func TestLocate_Discovery(t *testing.T) {
	cases := []struct {
		name  string
		files []string
		want  string
	}{
		{"current directory", []string{"/work/repo/sub/keepup.yml", "/work/repo/keepup.yml"}, "/work/repo/sub/keepup.yml"},
		{"parent directory", []string{"/work/repo/keepup.yml", "/home/u/.config/keepup/keepup.yml"}, "/work/repo/keepup.yml"},
		{"filesystem root", []string{"/keepup.yml"}, "/keepup.yml"},
		{"yaml extension", []string{"/work/repo/keepup.yaml"}, "/work/repo/keepup.yaml"},
		{"dotfile", []string{"/work/repo/.keepup.yml"}, "/work/repo/.keepup.yml"},
		{
			"name priority within a directory",
			[]string{"/work/repo/.keepup.yml", "/work/repo/keepup.yaml", "/work/repo/keepup.yml"},
			"/work/repo/keepup.yml",
		},
		{"yaml beats dotfile", []string{"/work/repo/.keepup.yml", "/work/repo/keepup.yaml"}, "/work/repo/keepup.yaml"},
		{"nearer dotfile beats farther keepup.yml", []string{"/work/repo/sub/.keepup.yml", "/work/keepup.yml"}, "/work/repo/sub/.keepup.yml"},
		{"global fallback", []string{"/home/u/.config/keepup/keepup.yml"}, "/home/u/.config/keepup/keepup.yml"},
		{
			"directory named like a config is skipped",
			[]string{"/work/repo/sub/keepup.yml/x", "/work/repo/keepup.yml"},
			"/work/repo/keepup.yml",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := locatorOver(tc.files...).Locate("")
			require.NoError(t, err)
			assert.Equal(t, filepath.FromSlash(tc.want), got)
		})
	}
}

func TestLocate_NothingFound(t *testing.T) {
	_, err := locatorOver().Locate("")
	require.ErrorIs(t, err, ErrNotFound)
	assert.Contains(t, err.Error(), cwd)
	assert.Contains(t, err.Error(), filepath.FromSlash("/home/u/.config/keepup/keepup.yml"))
}

func TestLocate_ExplicitWins(t *testing.T) {
	l := locatorOver("/work/repo/sub/keepup.yml")
	cases := []struct{ name, explicit, want string }{
		{"absolute", "/elsewhere/k.yml", "/elsewhere/k.yml"},
		{"relative to cwd", "../conf/k.yml", "/work/repo/conf/k.yml"},
		{"home expansion", "~/k.yml", "/home/u/k.yml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The explicit path need not exist: loading reports that error.
			got, err := l.Locate(tc.explicit)
			require.NoError(t, err)
			assert.Equal(t, filepath.FromSlash(tc.want), got)
		})
	}
}

func TestLocate_PropagatesEnvironmentErrors(t *testing.T) {
	boom := errors.New("boom")
	t.Run("cwd", func(t *testing.T) {
		l := locatorOver()
		l.Cwd = func() (string, error) { return "", boom }
		_, err := l.Locate("")
		require.ErrorIs(t, err, boom)
	})
	t.Run("home", func(t *testing.T) {
		l := locatorOver()
		l.Home = func() (string, error) { return "", boom }
		_, err := l.Locate("")
		require.ErrorIs(t, err, boom)
	})
	t.Run("stat failure other than not-exist", func(t *testing.T) {
		l := locatorOver()
		l.Stat = func(string) (fs.FileInfo, error) { return nil, boom }
		_, err := l.Locate("")
		require.ErrorIs(t, err, boom)
	})
}

func TestGlobalPath(t *testing.T) {
	got, err := locatorOver().GlobalPath()
	require.NoError(t, err)
	assert.Equal(t, filepath.FromSlash("/home/u/.config/keepup/keepup.yml"), got)
}
