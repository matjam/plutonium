package build_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/testguard"
)

// TestTaskStartFuncValues checks the rule of language spec sections 12.12
// and 18.3: a func value that may be a closure of a frame (a function
// literal that captures variables, a method value, or a value that holds one)
// must not reach a task start: a task statement, wg.Task, rt.Go,
// time.AfterFunc or a function that passes a func value on to one of them.
// The check is the escape analysis of views and happens at compile time, in
// both directions: the programs that start tasks with named functions,
// literals without captures and func values loaded from heap or package
// memory build, and the rest are rejected, whatever route the closure takes
// (a variable assigned twice, a field, an element, a helper function, a
// callback). No run-time check remains.
func TestTaskStartFuncValues(t *testing.T) {
	testguard.Parallel(t)
	const decls = `package main

import (
	"pluto/rt"
	"pluto/sync"
	"pluto/time"
)

type S struct{ f func() }

type Heap owner struct{ f func() }

type Counter struct{ n int }

func (c *Counter) add() { c.n++ }

func work() {}

func start(f func()) {
	task f()
}

func viaHelper(f func()) {
	start(f)
}

func viaTwo(f func()) {
	viaHelper(f)
}

func startField(s S) {
	task s.f()
}

func startPtr(s *S) {
	task s.f()
}

func startAll(fs []func()) {
	for _, f := range fs {
		task f()
	}
}

func each(n int, f func(int)) {
	for i := 0; i < n; i++ {
		task f(i)
	}
}

func later(f func()) {
	time.AfterFunc(time.Second, f)
}

func spawnGo(f func()) {
	rt.Go(f, rt.MinStackSize)
}

func group(f func()) {
	var wg sync.WaitGroup
	wg.Task(f)
	wg.Wait()
}

func startAny(fs ...func()) {
	for _, f := range fs {
		task f()
	}
}

type Runner struct{}

func (Runner) Run(f func()) { task f() }

type Starter interface{ Run(f func()) }

func id(f func()) func() { return f }

func startGeneric[F ~func()](f F) { task f() }

func make1() func() {
	n := 1
	return func() { n++ }
}

var global func() = work

var _ = rt.MinStackSize
var _ = time.Second

`
	reject := []struct{ name, body, code string }{
		{"variable assigned twice", "n := 1\nf := func() {}\nf = func() { n++ }\ntask f()", "E3004"},
		{"field", "n := 1\ns := S{f: func() { n++ }}\ntask s.f()", "E3004"},
		{"element", "n := 1\nfs := []func(){func() { n++ }}\ntask fs[0]()", "E3004"},
		{"method value", "var c Counter\nf := c.add\nf = c.add\ntask f()", "E3004"},
		{"helper", "n := 1\nf := func() {}\nf = func() { n++ }\nstart(f)", "E3003"},
		{"helper twice", "n := 1\nf := func() {}\nf = func() { n++ }\nviaTwo(f)", "E3003"},
		{"struct field to helper", "n := 1\ns := S{f: func() { n++ }}\nstart(s.f)", "E3003"},
		{"struct to helper", "n := 1\ns := S{f: func() { n++ }}\nstartField(s)", "E3003"},
		{"struct pointer to helper", "n := 1\ns := S{f: func() { n++ }}\nstartPtr(&s)", "E3003"},
		{"slice to helper", "n := 1\nstartAll([]func(){func() { n++ }})", "E3003"},
		{"callback", "n := 1\neach(3, func(i int) { n += i })", "E2594"},
		{"callback variable", "n := 1\ng := func(i int) {}\ng = func(i int) { n += i }\neach(3, g)", "E3003"},
		{"returned closure", "f := make1()\ntask f()", "E2594"},
		{"returned closure to helper", "f := make1()\nstart(f)", "E2594"},
		{"variadic", "n := 1\nstartAny(func() { n++ })", "E2594"},
		{"variadic variable", "n := 1\nf := func() {}\nf = func() { n++ }\nstartAny(work, f)", "E3003"},
		{"method", "n := 1\nf := func() {}\nf = func() { n++ }\nRunner{}.Run(f)", "E3003"},
		{"interface method", "n := 1\nf := func() {}\nf = func() { n++ }\nvar r Starter = Runner{}\nr.Run(f)", "E3003"},
		{"returned by a helper", "n := 1\nf := func() {}\nf = func() { n++ }\ng := id(f)\ntask g()", "E3004"},
		{"pointer to a func variable", "n := 1\nf := func() {}\nf = func() { n++ }\np := &f\ng := *p\ntask g()", "E3004"},
		{"pointer to a func variable to helper", "n := 1\nf := func() {}\nf = func() { n++ }\np := &f\nstart(*p)", "E3003"},
		{"map value", "n := 1\nm := map[int]func(){1: func() { n++ }}\ntask m[1]()", "E3002"},
		{"generic", "n := 1\nf := func() {}\nf = func() { n++ }\nstartGeneric(f)", "E3003"},
		{"wait group", "n := 1\nvar wg sync.WaitGroup\nf := func() {}\nf = func() { n++ }\nwg.Task(f)\nwg.Wait()", "E3003"},
		{"wait group helper", "n := 1\nf := func() {}\nf = func() { n++ }\ngroup(f)", "E3003"},
		{"rt go", "n := 1\nf := func() {}\nf = func() { n++ }\nrt.Go(f, rt.MinStackSize)", "E3003"},
		{"rt go helper", "n := 1\nf := func() {}\nf = func() { n++ }\nspawnGo(f)", "E3003"},
		{"after func", "n := 1\nf := func() {}\nf = func() { n++ }\ntime.AfterFunc(time.Second, f)", "E3003"},
		{"after func helper", "n := 1\nf := func() {}\nf = func() { n++ }\nlater(f)", "E3003"},
		{"literal to wait group", "n := 1\nvar wg sync.WaitGroup\nwg.Task(func() { n++ })\nwg.Wait()", "E2594"},
		{"literal to helper", "n := 1\nstart(func() { n++ })", "E2594"},
		{"literal to task", "n := 1\ntask func() { n++ }()", "E2590"},
	}
	accept := []struct{ name, body string }{
		{"named function", "task work()"},
		{"pointer to a func variable of a named function", "f := work\np := &f\ng := *p\ntask g()\nstart(*p)"},
		{"literal without captures", "task func() {}()"},
		{"variable assigned twice", "f := func() {}\nf = work\ntask f()"},
		{"field", "s := S{f: work}\ntask s.f()"},
		{"element", "fs := []func(){work, func() {}}\ntask fs[0]()"},
		{"helper", "start(work)\nviaTwo(func() {})"},
		{"struct to helper", "startField(S{f: work})\nstartPtr(&S{f: work})"},
		{"slice to helper", "startAll([]func(){work})"},
		{"callback", "each(3, func(i int) {})"},
		{"package variable", "task global()\nstart(global)"},
		{"loaded from heap", "h := new(Heap)\nh.f = work\ntask h.f()\nstart(h.f)"},
		{"wait group", "var wg sync.WaitGroup\nwg.Task(work)\nwg.Task(global)\nwg.Wait()\ngroup(work)"},
		{"rt go", "rt.Go(work, rt.MinStackSize)\nrt.Go(global, rt.MinStackSize)\nspawnGo(work)"},
		{"after func", "time.AfterFunc(time.Second, work)\nlater(global)"},
		{"closure called, not started", "n := 1\nf := func() { n++ }\nf()\nmake1()()"},
		{"variadic", "startAny(work, global)\nstartAny()"},
		{"method", "Runner{}.Run(work)\nvar r Starter = Runner{}\nr.Run(global)"},
		{"identity", "g := id(work)\ntask g()"},
		{"generic", "startGeneric(work)"},
		{"scope", "n := 1\nvar s sync.Scope\ns.Task(func() { n++ })"},
	}
	for _, c := range reject {
		t.Run("reject/"+c.name, func(t *testing.T) {
			testguard.Parallel(t)
			err := buildError(t, decls+"func main() {\n\t"+strings.ReplaceAll(c.body, "\n", "\n\t")+"\n}\n")
			if err == nil {
				t.Fatal("built, want a compile error")
			}
			var list diag.List
			if !errors.As(err, &list) {
				var d *diag.Diagnostic
				if !errors.As(err, &d) {
					t.Fatalf("not a diagnostic: %v", err)
				}
				list = diag.List{d}
			}
			for _, d := range list {
				if string(d.Code) == c.code {
					return
				}
			}
			t.Errorf("no %s among:\n%v", c.code, err)
		})
	}
	for _, c := range accept {
		t.Run("accept/"+c.name, func(t *testing.T) {
			testguard.Parallel(t)
			if err := buildError(t, decls+"func main() {\n\t"+strings.ReplaceAll(c.body, "\n", "\n\t")+"\n}\n"); err != nil {
				t.Errorf("rejected:\n%v", err)
			}
		})
	}
}
