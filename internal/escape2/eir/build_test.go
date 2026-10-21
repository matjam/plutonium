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
	also := os.Getenv("EIR_DUMP_FUNC") // and the functions whose names contain it
	for _, f := range p.Funcs {
		if f.Pkg == prog.Main.Types || (also != "" && strings.Contains(f.Name, also)) {
			p.Dump(&b, prog.Fset, f)
		}
	}
	return b.String()
}

func TestBuildDump(t *testing.T) {
	f := os.Getenv("EIR_DUMP")
	if f == "" {
		t.Skip("EIR_DUMP=file dumps the EIR of a program")
	}
	src, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(dumpMain(t, string(src)))
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

// TestBuildDeferInRangeBody checks that what a defer statement in a range
// body keeps (a closure record, an interface box) is in the frame of the
// function that runs the deferred call, not the body's, which is gone by
// then (testdata/run/rangefuncdefer.p; internal/lower lastingFrame).
func TestBuildDeferInRangeBody(t *testing.T) {
	out := dumpMain(t, `package main

func save(x int, v any) {}

func each(yield func(int) bool) { _ = yield(1) }

func f() {
	x := 1
	for i := range each {
		if i == 1 {
			defer func() { save(x, nil) }()
			defer save(i, [2]int{i, x})
			return
		}
	}
}

func main() { f() }
`)
	i := strings.Index(out, "func main.f\n")
	j := strings.Index(out, "func main.f range body")
	if i < 0 || j < 0 {
		t.Fatalf("no f or its range body in\n%s", out)
	}
	root := out[i:]
	if k := strings.Index(root[1:], "func "); k >= 0 {
		root = root[:k+1]
	}
	body := out[j:]
	if k := strings.Index(body[1:], "func "); k >= 0 {
		body = body[:k+1]
	}
	if !strings.Contains(root, " closure func()") || !strings.Contains(root, " box [2]int") {
		t.Errorf("the deferred call's closure record and box are not in f's frame:\n%s", root)
	}
	if strings.Contains(body, " closure func()") || strings.Contains(body, " box ") {
		t.Errorf("the range body holds what its deferred call keeps:\n%s", body)
	}
}

// TestBuildInterfaceValues checks the forms of interface values: a
// zero-sized value has no box (testdata/plutonium/tp_cons.p returns one);
// a type parameter's is the box or the value, which the instance decides
// (review4 ea01); and a type that is only a field of a converted type gets
// the itab adapter of an interface asserted to (fmt reaches a field's
// String through x.(Stringer)).
func TestBuildInterfaceValues(t *testing.T) {
	out := dumpMain(t, `package main

type Nil struct{}

type L []*int

func (l L) String() string { return "l" }

type Outer struct {
	L L
	K int
}

type Stringer interface{ String() string }

func empty() any { return Nil{} }

func generic[T any](x T) any { return x }

func str(a any) string {
	if s, ok := a.(Stringer); ok {
		return s.String()
	}
	return ""
}

func main() {
	_ = empty()
	_ = generic[int](1)
	n := 1
	_ = str(Outer{L{&n}, 1})
}
`)
	section := func(name string) string {
		i := strings.Index(out, "func "+name+"\n")
		if i < 0 {
			t.Fatalf("no %s in\n%s", name, out)
		}
		s := out[i+1:]
		if j := strings.Index(s, "func "); j >= 0 {
			s = s[:j]
		}
		return s
	}
	if s := section("main.empty"); strings.Contains(s, " box ") {
		t.Errorf("a zero-sized value is boxed:\n%s", s)
	}
	if s := section("main.generic"); !strings.Contains(s, "phi") {
		t.Errorf("a type parameter's interface value is not the box or the value:\n%s", s)
	}
	if !strings.Contains(out, "func main.L.String (interface adapter)") {
		t.Errorf("no adapter of main.L for Stringer:\n%s", out)
	}
}
