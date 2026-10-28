package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteFileAtomicKeepsFileMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "onlet.yaml")
	require.NoError(t, os.WriteFile(path, []byte("old"), 0o600))
	require.NoError(t, os.Chmod(path, 0o600))

	require.NoError(t, WriteFileAtomic(path, []byte("new")))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "new", string(data))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestWriteFileAtomicWritesThroughSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "managed.yaml")
	require.NoError(t, os.WriteFile(target, []byte("old"), 0o640))
	link := filepath.Join(dir, "onlet.yaml")
	require.NoError(t, os.Symlink(target, link))

	require.NoError(t, WriteFileAtomic(link, []byte("new")))

	info, err := os.Lstat(link)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&os.ModeSymlink, "the link must stay a link")
	data, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "new", string(data))
	targetInfo, err := os.Stat(target)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o640), targetInfo.Mode().Perm())
}

func TestVersionIsSHA256Hex(t *testing.T) {
	assert.Equal(t, "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824", Version([]byte("hello")))
}
