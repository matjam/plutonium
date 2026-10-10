package lsp

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
)

// extractStdlib copies the standard library in fsys (whose top directory
// is pluto/) into a directory under cache named for its contents, and
// returns that directory. An existing complete copy is reused, so every
// server for the same compiler shares one. The files are read-only: they
// are there to be read when go to definition opens them.
func extractStdlib(fsys fs.FS, cache string) (string, error) {
	h := sha256.New()
	var files []string
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		files = append(files, p)
		h.Write([]byte(p))
		h.Write([]byte{0})
		h.Write(data)
		return nil
	})
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cache, "stdlib-"+hex.EncodeToString(h.Sum(nil))[:16])
	marker := filepath.Join(dir, ".complete")
	if _, err := os.Stat(marker); err == nil {
		return dir, nil
	}
	if err := os.MkdirAll(cache, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(cache, ".stdlib-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(tmp) }() // nothing left after the rename
	for _, p := range files {
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return "", err
		}
		dst := filepath.Join(tmp, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(dst, data, 0o444); err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(filepath.Join(tmp, ".complete"), nil, 0o444); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, dir); err != nil {
		// Another server may have finished the same copy first.
		if _, serr := os.Stat(marker); serr == nil {
			return dir, nil
		}
		return "", err
	}
	return dir, nil
}
