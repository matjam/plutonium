// Ported from internal/escape2/eir/corpus_test.go at the go-cutover tag.
// The Go file was the external package eir_test. The corpus moved with the
// Go removal: cmd/pluto/testdata/diag is tests/diag, and the review
// programs are tests/testdata/escape/review2, review3 and review4.

package eir

import (
	"pluto/os"
	"pluto/path/filepath"
	"pluto/strings"
	"pluto/testing"

	"compiler/compat"
	"compiler/internal/escape2/contract"
	"compiler/internal/load"
	"compiler/internal/scratch"
	"compiler/internal/testenv"
	"compiler/internal/types2"
	"compiler/pu"
)

// A corpusProgram is one program of the test corpus: the corpus of
// tools/escapedigest (the examples, testdata/run, lib, plutonium and trace,
// the kernel and C ABI programs, and cmd/pluto/testdata/diag) and the
// review programs of internal/escape (testdata/review2, review3).
type corpusProgram struct {
	name                 string
	dir                  string // a directory with the program, or
	file                 string // a single .p file, built as main.p
	freestanding, kernel bool
	rejected             bool // a review program: the escape analysis rejects it
	diag                 bool // a diagnostics program, which may not type-check
}

// repoRoot returns the repository's root directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	return testenv.Root()
}

type kernelProgram struct {
	name                 string
	freestanding, kernel bool
}

func corpus(t *testing.T) []corpusProgram {
	root := repoRoot(t)
	var out []corpusProgram
	examples := testenv.Glob(filepath.Join(root, "examples", "*", "main.p"))
	for _, f := range examples {
		out = compat.Append(out, corpusProgram{name: compat.Concat("examples/", filepath.Base(filepath.Dir(f))), dir: filepath.Dir(f)})
	}
	for _, k := range []kernelProgram{{"sched", false, true}, {"smoke", false, true}, {"gsuser", false, true}, {"irq", true, false}} {
		out = compat.Append(out, corpusProgram{name: compat.Concat("kernel/", k.name), dir: filepath.Join(root, "testdata", "kernel", k.name), freestanding: k.freestanding, kernel: k.kernel})
	}
	out = compat.Append(out, corpusProgram{name: "cabi", dir: filepath.Join(root, "testdata", "cabi")})
	for _, d := range []string{"run", "lib", "plutonium", "trace"} {
		files := testenv.Glob(filepath.Join(root, "testdata", d, "*.p"))
		for _, f := range files {
			out = compat.Append(out, corpusProgram{name: compat.Concat(d, "/", strings.TrimSuffix(filepath.Base(f), ".p")), file: f})
		}
	}
	diags := testenv.Glob(filepath.Join(root, "tests", "diag", "*", "main.p"))
	for _, f := range diags {
		out = compat.Append(out, corpusProgram{name: compat.Concat("diag/", filepath.Base(filepath.Dir(f))), dir: filepath.Dir(f), diag: true})
	}
	for _, d := range []string{"review2", "review3", "review4"} {
		files := testenv.Glob(filepath.Join(root, "tests", "testdata", "escape", d, "*.p"))
		for _, f := range files {
			out = compat.Append(out, corpusProgram{name: compat.Concat(d, "/", strings.TrimSuffix(filepath.Base(f), ".p")), file: f, rejected: true, diag: true}) // some of review4 do not type-check
		}
	}
	return out
}

// load loads a corpus program as the build does, with generation checks
// in a hosted build (internal/build loadConfig). ok is false for a program
// that does not load (a diagnostics program that is rejected earlier).
func (cp corpusProgram) load(t *testing.T) (*load.Program, bool) {
	t.Helper()
	dir := cp.dir
	if cp.file != "" {
		dir = testenv.TempDir(t)
		defer testenv.Remove(dir)
		src, err := compat.ReadFile(cp.file)
		if err != nil {
			t.Fatal(err.Error())
		}
		if err := os.WriteFile(filepath.Join(dir, "main.p"), src, 0o644); err != nil {
			t.Fatal(err.Error())
		}
	}
	tags := load.BuildTags(cp.freestanding, cp.kernel, nil)
	if !cp.freestanding && !cp.kernel {
		tags["gencheck"] = true
	}
	cfg := pu.New(load.Config{Tags: tags, Stdlib: testenv.Stdlib(), Sizes: types2.SizesFor("gc", "amd64")})
	prog, err := load.Load(cfg, dir)
	if err != nil {
		if cp.diag {
			return nil, false
		}
		t.Fatalf("load: %s", err.Error())
	}
	return prog, true
}

// TestBuildCorpus builds EIR for every program of the corpus: no construct
// of the corpus is unknown to the desugarer.
//
// Each program is loaded and built in a region of its own, released when
// its subtest ends: nothing else frees a program's load and EIR (Go's GC
// did), and without the regions the test passed 24 GB about 250 programs
// into the corpus. t's messages are written out as they are made, so they
// may be made in the region.
func TestBuildCorpus(t *testing.T) {
	// The Go test skipped with -short; pluto/testing has no Short.
	if compat.Getenv("PLUTO_LONG_TESTS") == "" {
		t.Skip("builds the whole corpus; set PLUTO_LONG_TESTS=1")
	}
	for _, cp := range corpus(t) {
		t.Run(cp.name, func(t *testing.T) {
			mem := scratch.NewRegion()
			defer mem.Release()
			use := mem.Use()
			defer use.Restore()
			prog, ok := cp.load(t)
			if !ok {
				t.Skip("does not load")
			}
			p, err := Build(prog)
			if err != nil {
				t.Errorf("%s", err.Error())
				return
			}
			for _, m := range receiverValues(p) {
				t.Errorf("%s", m)
			}
		})
	}
}

// receiverValues returns the calls of a method with a pointer receiver
// (a declared method, a ring's, a trusted contract method) whose receiver
// argument is a value loaded from memory that is not a pointer: the
// review-5 class, where the ring's value was passed for its address. The
// lowering oracle cannot see it for the ring's methods, which the lowerer
// expands inline.
func receiverValues(p *Program) []string {
	var out []string
	for _, f := range p.Funcs {
		defs := map[Reg]*Instr{}
		for _, b := range f.Blocks {
			for i := range b.Instrs {
				if in := &b.Instrs[i]; in.Dst != 0 {
					defs[in.Dst] = in
				}
			}
		}
		for _, b := range f.Blocks {
			for i := range b.Instrs {
				in := &b.Instrs[i]
				if in.Op != OpCall || len(in.Call.Args) == 0 || in.Call.Args[0] == 0 {
					continue
				}
				c := in.Call
				var recv types2.Type
				switch {
				case c.Kind == CallContract && c.Contract == contract.RingMethod:
					// Every method of a ring has a pointer receiver.
					recv = types2.NewPointer(types2.Typ[types2.Int])
				case c.Kind == CallStatic && c.Callee != NoFunc && p.Funcs[c.Callee].Key.Obj != nil:
					if r := p.Funcs[c.Callee].Key.Obj.Type().(*types2.Signature).Recv(); r != nil {
						recv = r.Type()
					}
				}
				if recv == nil {
					continue
				}
				if _, ptr := types2.Unalias(recv).(*types2.Pointer); !ptr {
					continue
				}
				d := defs[c.Args[0]]
				if d == nil || d.Op != OpLoad || d.Type == nil {
					continue
				}
				switch d.Type.Underlying().(type) {
				case *types2.Pointer, *types2.Basic, *types2.Interface, *types2.TypeParam:
					continue // a pointer (or a word that holds one) loaded from a variable
				}
				if w, ok := d.Type.Underlying().(*types2.Wrapper); ok && w.IsPointer() {
					continue
				}
				out = compat.Append(out, compat.Sprintf("%s: a call of a method with a pointer receiver passes a loaded %s for its address", f.Name, d.Type))
			}
		}
	}
	return out
}
