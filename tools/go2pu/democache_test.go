package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// The demo cache keeps the executables demoBinary builds across test runs,
// under the user's cache directory, so that running the tests again with
// the same compiler on the same translations (a second run, another agent's
// run, go test ./... and then go test ./tools/go2pu) does not build the
// translated compilers again.
//
// An entry is used only when its key is certain. The key hashes
//
//   - this test binary, which holds the compiler (internal/build and the
//     packages it imports, with the standard library embedded);
//   - every file of selfhost/, the module the demos are built from;
//   - the external tools a build runs (as, ld, and the C compiler of $CC or
//     cc), by their contents;
//   - the environment variables the compiler reads (PLUTO_*, except the
//     PLUTO_TEST_* settings of internal/testguard, and CC);
//   - the demo, the level and whether generation checks are on.
//
// If any of these cannot be read, the cache is not used. PLUTO_TEST_CACHE=0
// turns it off; PLUTO_TEST_CACHE=<dir> puts it in another directory.
// Entries unused for cacheMaxAge are removed.

const cacheMaxAge = 7 * 24 * time.Hour

var (
	cacheOnce sync.Once
	cacheDir  string // "" if the cache is off
	cacheBase string // the hash of everything but the demo and its flags
)

// demoCache returns the cache directory and the base of the keys, or "" if
// the cache is off or its key uncertain.
func demoCache() (dir, base string) {
	cacheOnce.Do(func() {
		dir, err := cacheLocation()
		if err != nil || dir == "" {
			return
		}
		base, err := cacheKeyBase()
		if err != nil {
			fmt.Fprintf(os.Stderr, "go2pu: demo cache off: %v\n", err)
			return
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return
		}
		pruneCache(dir, time.Now().Add(-cacheMaxAge))
		cacheDir, cacheBase = dir, base
	})
	return cacheDir, cacheBase
}

// cacheLocation returns the cache's directory, "" if it is off.
func cacheLocation() (string, error) {
	switch v := os.Getenv("PLUTO_TEST_CACHE"); v {
	case "0", "off":
		return "", nil
	case "":
		d, err := os.UserCacheDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(d, "plutonium", "go2pu-demos"), nil
	default:
		return v, nil
	}
}

// cacheKeyBase hashes what every key shares.
func cacheKeyBase() (string, error) {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "go2pu demo cache 1\n")
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if err := hashFile(h, "test binary", exe); err != nil {
		return "", err
	}
	if err := hashTree(h, selfhostDir); err != nil {
		return "", err
	}
	cc := "cc"
	if f := strings.Fields(os.Getenv("CC")); len(f) > 0 {
		cc = f[0]
	}
	for _, tool := range []string{"as", "ld", cc} {
		path, err := exec.LookPath(tool)
		if err != nil {
			if tool == cc {
				// No C compiler: a build that needs one fails
				// without the cache as it would with it.
				_, _ = fmt.Fprintf(h, "tool %s missing\n", tool)
				continue
			}
			return "", err
		}
		if err := hashFile(h, "tool "+tool, path); err != nil {
			return "", err
		}
	}
	var env []string
	for _, kv := range os.Environ() {
		if (strings.HasPrefix(kv, "PLUTO") && !strings.HasPrefix(kv, "PLUTO_TEST_")) || strings.HasPrefix(kv, "CC=") {
			env = append(env, kv)
		}
	}
	sort.Strings(env)
	for _, kv := range env {
		_, _ = fmt.Fprintf(h, "env %q\n", kv)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// cacheKey is the key of a build of the demo of k.
func cacheKey(base string, k demoKey) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s\n%s\n%d\n%v\n", base, k.name, k.level, k.noGenCheck)))
	return hex.EncodeToString(h[:])
}

// hashFile writes a label and the contents of a file (through symbolic
// links) to h.
func hashFile(h io.Writer, label, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, _ = fmt.Fprintf(h, "%s\n", label)
	n, err := io.Copy(h, f)
	_, _ = fmt.Fprintf(h, "\n%d bytes\n", n)
	return err
}

// hashTree writes the names, modes and contents of the files under root to
// h, in order.
func hashTree(h io.Writer, root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			_, _ = fmt.Fprintf(h, "dir %q\n", filepath.ToSlash(rel))
			return nil
		case d.Type().IsRegular():
			return hashFile(h, fmt.Sprintf("file %q", filepath.ToSlash(rel)), path)
		default:
			// A link or a device: what it reads is not certain.
			return fmt.Errorf("%s: not a regular file", path)
		}
	})
}

// cacheGet returns the cached executable of key, if there is one.
func cacheGet(dir, key string) (string, bool) {
	path := filepath.Join(dir, key)
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() {
		return "", false
	}
	now := time.Now()
	_ = os.Chtimes(path, now, now) // keep it from being pruned
	return path, true
}

// cachePut stores the executable bin under key, writing a temporary file and
// renaming it so that a reader never sees part of one.
func cachePut(dir, key, bin string) error {
	src, err := os.Open(bin)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	tmp, err := os.CreateTemp(dir, "tmp-")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := io.Copy(tmp, src); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, key))
}

// pruneCache removes the entries (and temporary files left by a run that
// stopped) last used before cutoff.
func pruneCache(dir string, cutoff time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		info, err := e.Info()
		if err == nil && info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}
