package load

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/matjam/plutonium/internal/types2"
)

func TestLoadOrdinaryPackage(t *testing.T) {
	t.Parallel()
	cfg := &Config{Stdlib: fstest.MapFS{"pluto/sample/lib.p": {Data: []byte("package sample;func Library(){}")}}, Sizes: types2.SizesFor("gc", "amd64")}
	p, err := LoadPackage(cfg, "pluto/sample")
	if err != nil {
		t.Fatal(err)
	}
	if p.Main.Path != "pluto/sample" || p.Main.Types.Name() != "sample" || p.Root != "" || len(p.Packages) != 1 {
		t.Fatalf("unexpected library program: %+v", p)
	}
	if _, err := LoadPackage(cfg, "pluto/missing"); err == nil {
		t.Fatal("missing library accepted")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pluto.mod"), []byte("module example.com/package\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lib.p"), []byte("package library;func Library(){}"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err = LoadPackage(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.Main.Path != "example.com/package" || p.Root != dir || p.Main.Types.Name() != "library" {
		t.Fatalf("unexpected module program: %+v", p)
	}
	if _, err := LoadPackage(cfg, filepath.Join(dir, "missing")); err == nil {
		t.Fatal("missing directory accepted")
	}
}
