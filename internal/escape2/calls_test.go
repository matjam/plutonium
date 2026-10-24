package escape2

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/escape"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/moves"
	"github.com/matjam/plutonium/internal/syntax/ast"
)

const factsSrc = `package main

import "pluto/vec"

var global *int

func keep(p *int)            { global = p }
func ret(p *int) *int        { return p }
func into(dst **int, src *int) { *dst = src }
func look(p *int) int        { return *p }
func pack(ps ...*int)      { global = ps[0] }

type T struct{ n int }

func (t *T) Get() int { return t.n }

type Getter interface{ Get() int }

func main() {
	x, y := 1, 2
	keep(&x)
	_ = ret(&y)
	var d *int
	into(&d, &x)
	_ = look(&y)
	pack(&x, &y)
	var t T
	_ = t.Get()
	var g Getter = &t
	_ = g.Get()
	f := look
	_ = f(&x)
	v := vec.New[int]()
	v.Push(1)
	v.Drop()
}
`

// argFactsAt returns the facts recorded for the argument of the call in
// function main whose text, within the call, is arg, and the call's
// function as written.
func argFactsAt(t *testing.T, prog *load.Program, calls *escape.Calls, fun string, arg int) *escape.ArgFacts {
	t.Helper()
	for _, p := range prog.Packages {
		if p != prog.Main {
			continue
		}
		for _, f := range p.Files {
			var found *escape.ArgFacts
			done := false
			ast.Inspect(f, func(n ast.Node) bool {
				c, ok := n.(*ast.CallExpr)
				if !ok || done || exprString(c.Fun) != fun {
					return true
				}
				done = true
				if arg < 0 {
					found = calls.Arg(funSelector(c.Fun).X)
				} else {
					found = calls.Arg(c.Args[arg])
				}
				return false
			})
			if done {
				return found
			}
		}
	}
	t.Fatalf("no call of %s", fun)
	return nil
}

func exprString(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return exprString(e.X) + "." + e.Sel.Name
	}
	return ""
}

// TestCallFacts checks what this analysis says internal/moves of the
// arguments of calls: kept, returned, stored into another argument, or
// none of these; through a static call, a method, an interface, a func
// value, a variadic argument and the drop of an owner.
func TestCallFacts(t *testing.T) {
	prog := loadSrc(t, factsSrc)
	// keep and pack keep a view of the frame: errors, which the facts
	// are made regardless of.
	calls, _ := CheckCalls(prog)
	if calls == nil {
		t.Fatal("no facts")
	}
	for _, c := range []struct {
		fun                  string
		arg                  int // -1: the receiver
		self, returned, into bool
	}{
		{"keep", 0, true, false, false},
		{"ret", 0, true, true, false},
		{"into", 1, true, false, true},
		{"look", 0, false, false, false},
		{"pack", 0, true, false, false},
		{"pack", 1, true, false, false},
		{"t.Get", -1, false, false, false},
		{"g.Get", -1, false, false, false},
		{"f", 0, false, false, false},
		{"v.Push", -1, false, false, false},
		{"v.Drop", -1, false, false, false},
	} {
		af := argFactsAt(t, prog, calls, c.fun, c.arg)
		if af == nil {
			t.Errorf("%s argument %d: no facts", c.fun, c.arg)
			continue
		}
		// Stored into another argument: what a receiver stores back into
		// itself, moves ignores.
		other := false
		for _, in := range af.Into {
			other = other || c.arg >= 0 || in.Arg == nil || exprString(in.Arg) != strings.Split(c.fun, ".")[0]
		}
		if af.Self != c.self || af.Returned != c.returned || other != c.into {
			t.Errorf("%s argument %d: Self %v Returned %v Into %v; want %v %v %v", c.fun, c.arg, af.Self, af.Returned, af.Into, c.self, c.returned, c.into)
		}
	}
	if af := argFactsAt(t, prog, calls, "into", 1); af != nil && len(af.Into) == 1 && !af.Into[0].Direct {
		t.Errorf("into(&d, &x) stores &x into d itself: %+v", af.Into[0])
	}
}

// TestCallFactsMoves checks that the move checker's verdicts given this
// analysis's facts about calls are those it reaches given internal/escape's,
// on programs that pass views of local owners to calls (the corpus as a
// whole: go run ./tools/escape2diff -moves).
func TestCallFactsMoves(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{
		"testdata/plutonium/dropsites.p",
		"testdata/lib/chunked.p",
		"testdata/lib/vec.p",
		"testdata/lib/vecfixed.p",
		"cmd/pluto/testdata/diag/container_views/main.p",
		"cmd/pluto/testdata/diag/defer_view_moved/main.p",
		"cmd/pluto/testdata/diag/moves/main.p",
		"cmd/pluto/testdata/diag/moves_more/main.p",
	} {
		src, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			t.Fatal(err)
		}
		t.Run(f, func(t *testing.T) {
			prog := loadSrc(t, string(src))
			oldCalls, _ := escape.Analyze(prog)
			newCalls, _ := CheckCalls(prog)
			_, oldErr := moves.AnalyzeWith(prog, oldCalls)
			_, newErr := moves.AnalyzeWith(prog, newCalls)
			if got, want := firstLines(newErr), firstLines(oldErr); !slices.Equal(got, want) {
				t.Errorf("move errors given this analysis's facts:\n%s\ngiven internal/escape's:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
			}
		})
	}
}

// TestAnalyzeNew checks that the new mode gives this analysis's verdict and
// its facts about calls.
func TestAnalyzeNew(t *testing.T) {
	ok, bad := loadSrc(t, accepted), loadSrc(t, rejected)
	if calls, err := Analyze(ok, ModeNew); err != nil || calls == nil {
		t.Errorf("accepted program: %v, facts %v", err, calls != nil)
	}
	if _, err := Analyze(bad, ModeNew); err == nil {
		t.Errorf("rejected program accepted")
	}
}

// firstLines returns the first lines of err's errors, sorted.
func firstLines(err error) []string {
	if err == nil {
		return nil
	}
	var out []string
	for _, l := range strings.Split(err.Error(), "\n") {
		if l != "" && !strings.HasPrefix(l, " ") && !strings.HasPrefix(l, "\t") {
			out = append(out, l)
		}
	}
	slices.Sort(out)
	return out
}
