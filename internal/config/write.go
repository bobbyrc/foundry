package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// Version identifies one exact file content: lowercase hex SHA-256. The web UI sends the version
// it read back with an edit, so a write over a file that changed in the meantime can be refused.
func Version(text []byte) string {
	sum := sha256.Sum256(text)
	return hex.EncodeToString(sum[:])
}

// WriteFileAtomic replaces path's contents via a temp file in the same directory and a rename, keeping the file mode, and writes through a symlink to its target.
func WriteFileAtomic(path string, data []byte) error {
	// A symlinked config (for example one managed by a dotfile tool) is written through to its
	// target; renaming over the link itself would replace the link with a plain file.
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(target)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	_, werr := tmp.Write(data)
	if werr == nil {
		werr = tmp.Chmod(info.Mode().Perm())
	}
	if werr == nil {
		werr = tmp.Sync()
	}
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return fmt.Errorf("write %s: %w", path, werr)
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
