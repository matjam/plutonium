package build_test

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
)

// frameStore matches a store to the stack frame, which the spill program
// must contain for the test to cover spilling.
var frameStore = regexp.MustCompile(`, -?\d+\(%r[sb]p\)`)

// TestDeterministic compiles programs several times and checks that the
// listing and the object file are byte-identical each time: self-hosting
// compares the output of two compiler stages, so nothing may depend on map
// iteration order. spills.p keeps many values live across calls; ifaces.p
// and generics.p exercise type descriptors, itabs and instantiation.
func TestDeterministic(t *testing.T) {
	const runs = 4
	for _, name := range []string{"spills", "ifaces", "generics"} {
		src, err := os.ReadFile(filepath.Join("..", "..", "testdata", "run", name+".p"))
		if err != nil {
			t.Fatal(err)
		}
		for _, level := range []int{0, 2} {
			t.Run(name+"/O"+string(rune('0'+level)), func(t *testing.T) {
				// One directory for every run: the trace table and DWARF
				// record the source path, so identical input includes it.
				dir := t.TempDir()
				var firstAsm, firstObj []byte
				for run := range runs {
					asm, obj := compileObject(t, dir, src, level)
					if run == 0 {
						firstAsm, firstObj = asm, obj
						if name == "spills" && !frameStore.Match(asm) {
							t.Fatal("listing has no stores to the stack frame: the program no longer spills")
						}
						continue
					}
					if !bytes.Equal(asm, firstAsm) {
						t.Fatalf("run %d: listing differs from run 0", run)
					}
					if !bytes.Equal(obj, firstObj) {
						t.Fatalf("run %d: object file differs from run 0", run)
					}
				}
			})
		}
	}
}

// compileObject compiles src as a main package in dir to an object file and
// returns its listing and the object's bytes.
func compileObject(t *testing.T, dir string, src []byte, optLevel int) (asm, obj []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "main.p"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "main.o")
	var listing bytes.Buffer
	cfg := &build.Config{Dir: dir, Output: out, Stdlib: plutonium.Stdlib(), OptLevel: optLevel,
		Listing: &listing, CompileOnly: true}
	if err := build.Build(cfg); err != nil {
		t.Fatalf("build: %v", err)
	}
	obj, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return listing.Bytes(), obj
}
