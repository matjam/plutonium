package eir_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/types2"
)

// loadSrc loads src as a main package.
func loadSrc(t *testing.T, src string) *load.Program {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return loadDir(t, dir)
}

func loadDir(t *testing.T, dir string) *load.Program {
	t.Helper()
	cfg := &load.Config{
		Tags:   map[string]bool{"amd64": true, "linux": true, "hosted": true},
		Stdlib: plutonium.Stdlib(),
		Sizes:  types2.SizesFor("gc", "amd64"),
	}
	prog, err := load.Load(cfg, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return prog
}

// dumpMain builds src and returns the dumps of the functions of package
// main.
func dumpMain(t *testing.T, src string) string {
	t.Helper()
	prog := loadSrc(t, src)
	p, err := eir.Build(prog)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var b strings.Builder
	for _, f := range p.Funcs {
		if f.Pkg == prog.Main.Types {
			p.Dump(&b, prog.Fset, f)
		}
	}
	return b.String()
}

func TestBuildSmoke(t *testing.T) {
	out := dumpMain(t, `package main

type T struct{ p *int }

func (t *T) Set(p *int) { t.p = p }

func f(x *int) *int {
	var t T
	t.Set(x)
	for i := 0; i < 3; i++ {
		if i == 1 {
			x = nil
		}
	}
	return x
}

func main() {
	n := 1
	defer println("bye")
	f(&n)
}
`)
	t.Log(out)
}
