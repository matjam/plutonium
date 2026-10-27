// Package testenv gives the compiler's unit tests what Go's testing package
// and the Go build gave them: the standard library (the Go compiler embedded
// it; plutonium.Stdlib), the repository root (go test ran in the package
// directory; pluto test runs the test binary in a work directory of its
// own), and temporary directories (t.TempDir, which pluto/testing lacks).
package testenv

import (
	"pluto/os"
	"pluto/path/filepath"
	"pluto/sort"
	"pluto/strings"
	"pluto/testing"

	"compiler/compat"
	"compiler/compat/fs"
)

// LibDir returns the directory of the standard library: $PLUTOROOT, which
// pluto test is run with, or "lib" when it is not set.
func LibDir() string {
	if root := compat.Getenv("PLUTOROOT"); root != "" {
		if abs, err := filepath.Abs(root); err == nil {
			return abs
		}
		return root
	}
	return "lib"
}

// Stdlib returns the standard library, as the compiler reads it.
func Stdlib() fs.FS { return fs.DirFS(LibDir()) }

// Root returns the repository root: the directory above the standard
// library. Paths a Go test gave relative to its package directory are
// joined to it.
func Root() string { return filepath.Dir(LibDir()) }

// TempDir makes a new temporary directory, failing the test if it cannot.
// The caller removes it with Remove (pluto/testing has no Cleanup).
func TempDir(t *testing.T) string {
	dir, err := os.MkdirTemp("", "pluto-unit-")
	if err != nil {
		t.Fatalf("TempDir: %s", err.Error())
	}
	return dir
}

// Remove removes a directory TempDir made, with everything in it.
func Remove(dir string) { _ = os.RemoveAll(dir) }

// Glob is filepath.Glob, which pluto/path/filepath lacks: the names that
// match pattern, sorted within each directory, or nil. A malformed pattern
// matches nothing.
func Glob(pattern string) []string {
	dir, file := filepath.Dir(pattern), filepath.Base(pattern)
	dirs := []string{dir}
	if hasMeta(dir) {
		dirs = Glob(dir)
	}
	var out []string
	for _, d := range dirs {
		if !hasMeta(file) {
			name := filepath.Join(d, file)
			if _, err := os.Lstat(name); err == nil {
				out = compat.Append(out, name)
			}
			continue
		}
		var names []string
		ents := os.ReadDir(d) or continue
		for i := 0; i < ents.Len(); i++ {
			n := compat.Concat(ents.At(i).NameLossy())
			if ok, _ := filepath.Match(file, n); ok {
				names = compat.Append(names, n)
			}
		}
		sort.Strings(names)
		for _, n := range names {
			out = compat.Append(out, filepath.Join(d, n))
		}
	}
	return out
}

func hasMeta(p string) bool { return strings.ContainsAny(p, `*?[\`) }

// WriteFile writes data to name, failing the test if it cannot.
func WriteFile(t *testing.T, name string, data string) {
	if err := os.WriteFile(name, compat.Bytes(data), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %s", name, err.Error())
	}
}
