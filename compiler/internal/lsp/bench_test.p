// Ported from internal/lsp/bench_test.go at the go-cutover tag.

package lsp

import (
	"pluto/path/filepath"
	"pluto/testing"

	"compiler/compat"
)

// BenchmarkCheckAfterEdit measures what an edit costs before diagnostics
// can be published: rechecking the edited package (the amd64 backend, about
// 5000 lines in 11 files) with its imports cached.
func BenchmarkCheckAfterEdit(b *testing.B) {
	lib := stdlibDir()
	if lib == "" {
		b.Skip("PLUTOROOT is not set")
		return
	}
	dir := filepath.Join(filepath.Dir(lib), "compiler", "internal", "arch", "amd64")
	path := filepath.Join(dir, "isel.p")
	src := compat.ReadFile(path) or {
		b.Skip(err)
		return
	}
	modes, _ := modesFor(Settings{})
	w := newWorkspace(lib, modes)
	w.overlay[path] = src
	if _, p, err := w.fileOf(path); err != nil || len(p.errors) > 0 {
		b.Fatalf("checking %s: %v", dir, err)
		return
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.overlay[path] = compat.Clone(src)
		w.invalidate(dir)
		if _, _, err := w.fileOf(path); err != nil {
			b.Fatal(err)
			return
		}
	}
}
