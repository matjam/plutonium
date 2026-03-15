package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/matjam/plutonium/internal/ast"
	"github.com/matjam/plutonium/internal/parser"
	"github.com/matjam/plutonium/internal/token"
)

// ensureStdlibRoot sets PLUTONIUM_ROOT if not already set, by locating the
// repository root relative to this test file. This allows tests to find the
// pluto/ stdlib directory without requiring the env var to be pre-configured.
func ensureStdlibRoot(t *testing.T) {
	t.Helper()
	if os.Getenv("PLUTONIUM_ROOT") != "" {
		return
	}
	// Locate repo root from this source file's path.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return
	}
	// thisFile is cmd/pluto/deps_test.go — go up two directories.
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	candidate := filepath.Join(repoRoot, "pluto")
	if info, err := os.Stat(candidate); err == nil && info.IsDir() {
		t.Setenv("PLUTONIUM_ROOT", repoRoot)
	}
}

func TestCollectDependencies(t *testing.T) {
	ensureStdlibRoot(t)
	// Parse a file that imports pluto/os
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.pu", []byte(`
package main
import "pluto/os"
func main() { os.Exit(0) }
`), 0)
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}

	stdlibRoot := resolveStdlibRoot()
	if stdlibRoot == "" {
		t.Skip("stdlib not found")
	}

	tags := map[string]bool{"amd64": true, "linux": true}
	deps, err := collectDependencies([]*ast.File{f}, stdlibRoot, tags, fset)
	if err != nil {
		t.Fatalf("collectDependencies: %v", err)
	}

	// Should include pluto/os and its transitive deps (pluto/syscall)
	found := make(map[string]bool)
	for _, d := range deps {
		found[d.ImportPath] = true
	}
	if !found["pluto/os"] {
		t.Error("expected pluto/os in dependencies")
	}
	if !found["pluto/syscall"] {
		t.Error("expected pluto/syscall in dependencies (transitive)")
	}

	// Verify topological order: pluto/syscall should appear before pluto/os
	idxSyscall := -1
	idxOS := -1
	for i, d := range deps {
		if d.ImportPath == "pluto/syscall" {
			idxSyscall = i
		}
		if d.ImportPath == "pluto/os" {
			idxOS = i
		}
	}
	if idxSyscall >= 0 && idxOS >= 0 && idxSyscall >= idxOS {
		t.Errorf("expected pluto/syscall (idx %d) before pluto/os (idx %d) in topological order",
			idxSyscall, idxOS)
	}
}

func TestCollectDependenciesNoImports(t *testing.T) {
	ensureStdlibRoot(t)
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.pu", []byte(`
package main
func main() {}
`), 0)
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}

	stdlibRoot := resolveStdlibRoot()
	if stdlibRoot == "" {
		t.Skip("stdlib not found")
	}

	tags := map[string]bool{"amd64": true, "linux": true}
	deps, err := collectDependencies([]*ast.File{f}, stdlibRoot, tags, fset)
	if err != nil {
		t.Fatalf("collectDependencies: %v", err)
	}
	if len(deps) != 0 {
		t.Errorf("expected no dependencies, got %d", len(deps))
	}
}

func TestCollectDependenciesUnsafeSkipped(t *testing.T) {
	ensureStdlibRoot(t)
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.pu", []byte(`
package main
import "unsafe"
func main() {}
`), 0)
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}

	stdlibRoot := resolveStdlibRoot()
	if stdlibRoot == "" {
		t.Skip("stdlib not found")
	}

	tags := map[string]bool{"amd64": true, "linux": true}
	deps, err := collectDependencies([]*ast.File{f}, stdlibRoot, tags, fset)
	if err != nil {
		t.Fatalf("collectDependencies: %v", err)
	}
	if len(deps) != 0 {
		t.Errorf("expected no dependencies (unsafe is pseudo-package), got %d", len(deps))
	}
}

func TestStripQuotes(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{`"pluto/os"`, "pluto/os"},
		{`""`, ""},
		{`"x"`, "x"},
		{`noquotes`, "noquotes"},
		{`"`, `"`},
	}
	for _, tt := range tests {
		got := stripQuotes(tt.input)
		if got != tt.want {
			t.Errorf("stripQuotes(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestCollectDependenciesFilesPresent(t *testing.T) {
	ensureStdlibRoot(t)
	// Verify each discovered dependency has source files
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.pu", []byte(`
package main
import "pluto/os"
func main() { os.Exit(0) }
`), 0)
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}

	stdlibRoot := resolveStdlibRoot()
	if stdlibRoot == "" {
		t.Skip("stdlib not found")
	}

	tags := map[string]bool{"amd64": true, "linux": true}
	deps, err := collectDependencies([]*ast.File{f}, stdlibRoot, tags, fset)
	if err != nil {
		t.Fatalf("collectDependencies: %v", err)
	}

	for _, dep := range deps {
		if len(dep.Files) == 0 {
			t.Errorf("dependency %s has no source files", dep.ImportPath)
		}
		if dep.Dir == "" {
			t.Errorf("dependency %s has empty directory", dep.ImportPath)
		}
	}
}
