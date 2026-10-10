package lsp

import (
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkCheckAfterEdit measures what an edit costs before diagnostics
// can be published: rechecking the edited package (the self-hosted amd64
// backend, about 5000 lines in 10 files) with its imports cached.
func BenchmarkCheckAfterEdit(b *testing.B) {
	dir, err := filepath.Abs("../../selfhost/internal/arch/amd64")
	if err != nil {
		b.Fatal(err)
	}
	path := filepath.Join(dir, "isel.p")
	src, err := os.ReadFile(path)
	if err != nil {
		b.Skip(err)
	}
	modes, _ := modesFor(Settings{})
	w := newWorkspace(libDir(b), modes)
	w.overlay[path] = src
	if _, p, err := w.fileOf(path); err != nil || len(p.errors) > 0 {
		b.Fatalf("checking %s: %v %v", dir, err, p.errors)
	}
	b.ResetTimer()
	for b.Loop() {
		w.overlay[path] = append([]byte(nil), src...)
		w.invalidate(dir)
		if _, _, err := w.fileOf(path); err != nil {
			b.Fatal(err)
		}
	}
}
