// Package atomicfile replaces files so a crash never leaves one half-written
// or empty.
//
// It's hand-rolled because github.com/google/renameio exports nothing on
// Windows, which the app ships for, and its maybe package falls back to a
// plain write there. os.Rename replaces the file on every platform.
package atomicfile

import (
	"os"
	"path/filepath"
)

// Write writes data to a temporary file next to p, then renames it over p.
func Write(p string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(p), "."+filepath.Base(p)+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name()) // no-op after a successful rename
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(f.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(f.Name(), p)
}
