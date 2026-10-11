package lower_test

import (
	"strings"
	"testing"
)

// goClosureDecls are declarations the go statement closure tests share.
const goClosureDecls = `package main

type counter struct{ n int }

func (c *counter) add() { c.n++ }

func (c counter) get() int { return c.n }

type task func()

func (t *task) set(f func()) { *t = f }

func run(f func()) { f() }

func runAll(fs ...func()) {}

`

// TestGoFrameClosure checks which func values of a go statement the
// compiler can tell are closure records in the starting frame: those are
// compile errors, and the rest are left to the check when the go statement
// runs.
func TestGoFrameClosure(t *testing.T) {
	const code = `(run "pluto explain E2594"`
	cases := []struct {
		name, body string
		want       string // a fragment of the error, or "" for none
	}{
		{"var literal", "n := 1\nf := func() { n++ }\ntask f()", "cannot start f with go: it holds a function literal that captures n"},
		{"var decl", "n := 1\nvar f = func() { n++ }\ntask f()", "cannot start f with go"},
		{"typed var decl", "n := 1\nvar f func() = func() { n++ }\ntask run(f)", "cannot pass f to a puroutine"},
		{"chain", "var c counter\nf := c.add\ng := f\ntask g()", "cannot start g with go: it holds the method value c.add"},
		{"value receiver", "var c counter\ntask run(func() { _ = c.get() })", "cannot pass a function literal that captures c"},
		{"variadic", "var c counter\ntask runAll(c.add)", "cannot pass the method value c.add"},
		{"parenthesized", "var c counter\ntask run((c.add))", "cannot pass the method value c.add"},
		{"defined in if", "n := 1\nif f := func() { n++ }; n > 0 {\n\ttask run(f)\n}", "cannot pass f"},

		{"no captures", "f := func() {}\ntask f()\ntask run(f)", ""},
		{"function", "task run(run2)", ""},
		{"assigned again", "n := 1\nf := func() {}\nf = func() { n++ }\ntask f()", ""},
		{"address taken", "n := 1\nf := func() { n++ }\np := &f\n*p = func() {}\ntask f()", ""},
		{"method on var", "n := 1\nf := task(func() { n++ })\nf.set(func() {})\ntask run(f)", ""},
		{"range assigns", "n := 1\nf := func() { n++ }\nfor _, f = range []func(){nil} {\n}\ntask run(f)", ""},
		{"multi value", "f, ok := pair()\ntask run(f)\n_ = ok", ""},
		{"var without value", "n := 1\nvar f func()\nf = func() { n++ }\ntask run(f)", ""},
		{"parameter", "start(func() {})", ""},
		{"field", "var s struct{ f func() }\nn := 1\ns.f = func() { n++ }\ntask run(s.f)", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := goClosureDecls +
				"func run2() {}\n\nfunc pair() (func(), bool) { return nil, true }\n\n" +
				"func start(f func()) { task run(f) }\n\n" +
				"func main() {\n\t" + strings.ReplaceAll(c.body, "\n", "\n\t") + "\n}\n"
			got := lowerModeError(t, src, hosted)
			if c.want == "" {
				if got != "" {
					t.Errorf("reported:\n%s", got)
				}
				return
			}
			if !containsText(got, c.want) || !strings.Contains(got, code) {
				t.Errorf("got:\n%s\nwant a report containing %q", got, c.want)
			}
		})
	}
}

// TestGoFuncCall checks calls of functions that start puroutines with
// their func arguments, the library's and the program's own, which the
// compiler finds by following the func parameters into go statements and
// into such calls: the arguments the compiler can tell are closure records
// in the frame are compile errors, as in a go statement, and the rest are
// left to the run-time check.
func TestGoFuncCall(t *testing.T) {
	const code = `(run "pluto explain E2594"`
	cases := []struct {
		name, body string
		want       string // a fragment of the error, or "" for none
	}{
		{"wait group literal", "var wg sync.WaitGroup\nn := 0\nwg.Go(func() { n++ })", "cannot pass a function literal that captures n"},
		{"wait group local", "var wg sync.WaitGroup\nn := 0\nf := func() { n++ }\nwg.Go(f)", "cannot pass f to wg.Go, which starts a puroutine with it"},
		{"pointer receiver", "wg := new(sync.WaitGroup)\nn := 0\nwg.Go(func() { n++ })", "to wg.Go, which starts a puroutine with it"},
		{"method value", "var c counter\nvar wg sync.WaitGroup\nwg.Go(c.add)", "cannot pass the method value c.add"},
		{"after func", "n := 0\ntime.AfterFunc(time.Second, func() { n++ })", "to time.AfterFunc, which starts a puroutine with it"},
		{"rt go", "n := 0\nrt.Go(func() { n++ }, rt.MinStackSize)", "to rt.Go, which starts a puroutine with it"},
		{"through a function", "n := 0\nstart(func() { n++ })", "to start, which starts a puroutine with it"},
		{"go statement", "n := 0\nspawn(1, func() { n++ })", "to spawn, which starts a puroutine with it"},
		{"generic", "n := 0\nspawnT(1, func() { n++ })", "to spawnT, which starts a puroutine with it"},
		{"through an after func", "n := 0\nlater(func() { n++ })", "to later, which starts a puroutine with it"},

		{"no captures", "var wg sync.WaitGroup\nwg.Go(func() {})\nwg.Wait()\ntime.AfterFunc(time.Second, func() {})", ""},
		{"function", "var wg sync.WaitGroup\nwg.Go(run2)\nwg.Wait()", ""},
		{"parameter", "start(func() {})", ""},
		{"other function", "n := 0\nrun(func() { n++ })", ""},
		{"other parameter", "n := 0\nspawn(1, run2, func() { n++ })", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := "package main\n\nimport (\n\t\"pluto/rt\"\n\t\"pluto/sync\"\n\t\"pluto/time\"\n)\n\n" +
				"type counter struct{ n int }\n\nfunc (c *counter) add() { c.n++ }\n\n" +
				"func run(f func()) { f() }\n\nfunc run2() {}\n\n" +
				"func start(f func()) {\n\tvar wg sync.WaitGroup\n\twg.Go(f)\n\twg.Wait()\n}\n\n" +
				"func spawn(n int, f func(), more ...func()) {\n\tfor _, g := range more {\n\t\tg()\n\t}\n\ttask f()\n}\n\n" +
				"func spawnT[T any](x T, f func()) { task run(f) }\n\n" +
				"func later(f func()) { time.AfterFunc(time.Second, f) }\n\n" +
				"var _ = rt.MinStackSize\n\nvar _ = time.Second\n\n" +
				"func main() {\n\t" + strings.ReplaceAll(c.body, "\n", "\n\t") + "\n}\n"
			got := lowerModeError(t, src, hosted)
			if c.want == "" {
				if got != "" {
					t.Errorf("reported:\n%s", got)
				}
				return
			}
			if !containsText(got, c.want) || !strings.Contains(got, code) {
				t.Errorf("got:\n%s\nwant a report containing %q", got, c.want)
			}
		})
	}
}

// TestGoCapturingLiteralFix checks the fix suggested for a go statement
// that starts a function literal capturing variables: the values are passed
// as arguments, except a variable that holds a sync type, which would be
// copied into a different one, so a pointer to it is passed. Types are
// printed as the source writes them.
func TestGoCapturingLiteralFix(t *testing.T) {
	cases := []struct {
		name, imports, body string
		want, notWant       []string
	}{
		{
			name:    "wait group",
			imports: "\"pluto/sync\"",
			body:    "var wg sync.WaitGroup\ntask func() { defer wg.Done() }()",
			want:    []string{"wg has type sync.WaitGroup, which must not be copied, so start the literal with a sync.Scope, which waits for it and lets it use wg: var s sync.Scope; s.Task(func() {...})", "var wg sync.WaitGroup // at package level", "task func(wg *sync.WaitGroup) {"},
			notWant: []string{"pluto/sync", "pass it as an argument"},
		},
		{
			name:    "alias",
			imports: "stdsync \"pluto/sync\"",
			body:    "var mu stdsync.Mutex\ntask func() { mu.Lock() }()",
			want:    []string{"mu has type stdsync.Mutex, which must not be copied", "var s stdsync.Scope", "var mu stdsync.Mutex // at package level"},
		},
		{
			name:    "struct holding a mutex",
			imports: "\"pluto/sync\"",
			body:    "var s struct{ mu sync.Mutex }\ntask func() { s.mu.Lock() }()",
			want:    []string{"holds a value of type sync.Mutex and must not be copied", "declare it at package level and pass a pointer"},
		},
		{
			name:    "atomic array",
			imports: "\"pluto/sync/atomic\"",
			body:    "var a [2]atomic.Int32\ntask func() { a[0].Add(1) }()",
			want:    []string{"holds a value of type atomic.Int32 and must not be copied"},
		},
		{
			name:    "copyable",
			imports: "\"pluto/sync\"",
			body:    "n := 1\nvar l sync.Locker\ntask func() { _ = n; _ = l }()",
			want:    []string{"pass them as arguments instead: go func(n int, l sync.Locker) {...}(n, l)"},
			notWant: []string{"at package level"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := "package main\n\nimport " + c.imports + "\n\nfunc main() {\n\t" + strings.ReplaceAll(c.body, "\n", "\n\t") + "\n}\n"
			got := lowerModeError(t, src, hosted)
			for _, w := range c.want {
				if !strings.Contains(got, w) {
					t.Errorf("got:\n%s\nwant %q", got, w)
				}
			}
			for _, w := range c.notWant {
				if strings.Contains(got, w) {
					t.Errorf("got:\n%s\nunwanted %q", got, w)
				}
			}
		})
	}
}
