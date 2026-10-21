package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/matjam/plutonium/internal/testguard"
)

// TestHashTree checks that the hash of a tree changes with a file's name or
// contents and with a new file, and that a symbolic link makes it fail.
func TestHashTree(t *testing.T) {
	testguard.Parallel(t)
	dir := t.TempDir()
	write := func(name, data string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sum := func() string {
		t.Helper()
		h := sha256.New()
		if err := hashTree(h, dir); err != nil {
			t.Fatal(err)
		}
		return hex.EncodeToString(h.Sum(nil))
	}
	write("a/main.p", "package main\n")
	write("pluto.mod", "module selfhost\n")
	first := sum()
	if again := sum(); again != first {
		t.Fatal("the hash of an unchanged tree changed")
	}
	seen := map[string]string{first: "first"}
	for _, step := range []struct {
		name string
		do   func()
	}{
		{"contents", func() { write("a/main.p", "package main\n\nfunc main() {}\n") }},
		{"new file", func() { write("a/b/x.p", "") }},
		{"rename", func() {
			if err := os.Rename(filepath.Join(dir, "a", "b", "x.p"), filepath.Join(dir, "a", "b", "y.p")); err != nil {
				t.Fatal(err)
			}
		}},
		// Moving bytes between two files must change the hash too.
		{"boundary", func() { write("a/b/y.p", "package"); write("a/main.p", " main\n\nfunc main() {}\n") }},
	} {
		step.do()
		s := sum()
		if prev, dup := seen[s]; dup {
			t.Errorf("after %s the hash is the one after %s", step.name, prev)
		}
		seen[s] = step.name
	}
	if err := os.Symlink("pluto.mod", filepath.Join(dir, "link")); err != nil {
		t.Skip(err)
	}
	if err := hashTree(sha256.New(), dir); err == nil {
		t.Error("a symbolic link in the tree did not make the hash fail")
	}
}

// TestCacheKey checks that every part of a build's description is in its
// key.
func TestCacheKey(t *testing.T) {
	testguard.Parallel(t)
	k := demoKey{"typecheck", 0, false}
	keys := map[string]bool{}
	for _, key := range []string{
		cacheKey("base", k),
		cacheKey("other", k),
		cacheKey("base", demoKey{"lowerdemo", 0, false}),
		cacheKey("base", demoKey{"typecheck", 2, false}),
		cacheKey("base", demoKey{"typecheck", 0, true}),
	} {
		if keys[key] {
			t.Errorf("two builds share the key %s", key)
		}
		keys[key] = true
	}
	if first, again := cacheKey("base", k), cacheKey("base", demoKey{"typecheck", 0, false}); first != again {
		t.Error("the key of a build is not stable")
	}
}

// TestCacheLocation checks PLUTO_TEST_CACHE.
func TestCacheLocation(t *testing.T) {
	for _, off := range []string{"0", "off"} {
		t.Setenv("PLUTO_TEST_CACHE", off)
		if dir, err := cacheLocation(); err != nil || dir != "" {
			t.Errorf("PLUTO_TEST_CACHE=%s: %q, %v", off, dir, err)
		}
	}
	t.Setenv("PLUTO_TEST_CACHE", "/some/dir")
	if dir, _ := cacheLocation(); dir != "/some/dir" {
		t.Errorf("PLUTO_TEST_CACHE=/some/dir: %q", dir)
	}
	t.Setenv("PLUTO_TEST_CACHE", "")
	if dir, err := cacheLocation(); err == nil && filepath.Base(dir) != "go2pu-demos" {
		t.Errorf("default: %q", dir)
	}
}

// TestCachePutGet checks that an entry comes back executable and whole, that
// a missing one does not, and that pruning removes only old entries.
func TestCachePutGet(t *testing.T) {
	testguard.Parallel(t)
	dir := t.TempDir()
	if _, ok := cacheGet(dir, "missing"); ok {
		t.Error("a missing entry was found")
	}
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.WriteFile(bin, []byte("\x7fELF..."), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := cachePut(dir, "k1", bin); err != nil {
		t.Fatal(err)
	}
	got, ok := cacheGet(dir, "k1")
	if !ok {
		t.Fatal("the entry was not found")
	}
	data, err := os.ReadFile(got)
	if err != nil || string(data) != "\x7fELF..." {
		t.Errorf("entry holds %q, %v", data, err)
	}
	if st, err := os.Stat(got); err != nil || st.Mode().Perm()&0o100 == 0 {
		t.Errorf("entry is not executable: %v", st.Mode())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("the cache holds %d files, want the entry alone", len(entries))
	}
	old := time.Now().Add(-2 * cacheMaxAge)
	if err := cachePut(dir, "k2", bin); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(dir, "k2"), old, old); err != nil {
		t.Fatal(err)
	}
	pruneCache(dir, time.Now().Add(-cacheMaxAge))
	if _, ok := cacheGet(dir, "k2"); ok {
		t.Error("an old entry survived pruning")
	}
	if _, ok := cacheGet(dir, "k1"); !ok {
		t.Error("a recent entry was pruned")
	}
}
