package lower_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/lower"
	"github.com/matjam/plutonium/internal/ssa"
)

// TestCallFacts checks the lowering oracle's view of a program: each kind of
// call is found at its source position with the source functions it reaches,
// through the wrappers and adapters the lowerer makes.
func TestCallFacts(t *testing.T) {
	const src = `package main

type I interface{ M(p *int) }

type T struct{ n int }

func (t *T) M(p *int) { t.n = *p }

type Outer struct{ T }

func seq(yield func(int) bool) { yield(1) }

func apply(f func(int) int, x int) int { return f(x) }

func double(x int) int { return 2 * x }

var (
	ti T
	to Outer
)

func main() {
	var i I = &ti
	x := 3
	i.M(&x)
	var o I = &to
	o.M(&x)
	k := 1
	println(apply(double, 2), apply(func(v int) int { return v + k }, 1))
	for v := range seq {
		println(v)
	}
}
`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	lib, err := filepath.Abs(filepath.Join("..", "..", "lib"))
	if err != nil {
		t.Fatal(err)
	}
	prog, err := load.Load(&load.Config{Tags: map[string]bool{"linux": true, "amd64": true, "hosted": true}, Stdlib: os.DirFS(lib)}, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	cf, err := lower.LowerFacts(&lower.Config{}, prog)
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	funcs := map[string]*ssa.Func{}
	for _, f := range cf.Funcs() {
		funcs[f.Name] = f
	}
	if cf.Func(funcs["main.main"].Sym) != funcs["main.main"] {
		t.Error("Func does not find main.main by its symbol")
	}
	// reach returns, for each call of f on the given source line, the names of
	// the source functions it reaches, and the kinds of the calls.
	reach := func(f *ssa.Func, line int) (map[string]bool, map[lower.CallKind]bool) {
		names, kinds := map[string]bool{}, map[lower.CallKind]bool{}
		for _, c := range cf.Calls(f) {
			if !c.Pos.IsValid() || cf.Position(c.Pos).Line != line {
				continue
			}
			kinds[c.Kind] = true
			for _, tgt := range c.Targets {
				for _, s := range cf.SourceTargets(tgt) {
					o := cf.Origin(s)
					switch {
					case o.Lit != nil:
						names["literal"] = true
					case o.Range != nil:
						names["range body"] = true
					case o.Obj != nil:
						names[o.Obj.Name()] = true
					default:
						names[s.Name] = true
					}
				}
			}
		}
		return names, kinds
	}
	lineOf := func(text string) int {
		for i, l := range strings.Split(src, "\n") {
			if strings.Contains(l, text) {
				return i + 1
			}
		}
		t.Fatalf("no line %q", text)
		return 0
	}
	main := funcs["main.main"]
	if names, kinds := reach(main, lineOf("i.M(&x)")); !names["M"] || !kinds[lower.CallIface] {
		t.Errorf("i.M: reaches %v by %v, want M through an interface", names, kinds)
	}
	// The itab of *Outer holds a promoted wrapper, seen through to T's M.
	if names, _ := reach(main, lineOf("o.M(&x)")); !names["M"] {
		t.Errorf("o.M: reaches %v, want M", names)
	}
	if names, kinds := reach(funcs["main.apply"], lineOf("return f(x)")); !names["double"] || !names["literal"] || !kinds[lower.CallFuncVal] {
		t.Errorf("f(x): reaches %v by %v, want double and the literal through a func value", names, kinds)
	}
	if names, _ := reach(funcs["main.seq"], lineOf("yield(1)")); !names["range body"] {
		t.Errorf("yield(1): reaches %v, want the range body", names)
	}
	if names, kinds := reach(main, lineOf("for v := range seq")); !names["seq"] || !kinds[lower.CallStatic] {
		t.Errorf("range seq: reaches %v by %v, want seq, statically", names, kinds)
	}
	if o := cf.Origin(main.Sym); o.Obj == nil || o.Decl == nil || !o.Source() || o.Extern {
		t.Errorf("Origin(main.main) = %+v, want a declared function", o)
	}
	if got := lower.MethodName(prog.Packages[len(prog.Packages)-1].Types.Scope().Lookup("I").Type(), 0); got != "M" {
		t.Errorf("MethodName(I, 0) = %q, want M", got)
	}
}
