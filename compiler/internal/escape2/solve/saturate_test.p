// Ported from internal/escape2/solve/saturate_test.go at the go-cutover tag.

package solve

import (
	"pluto/testing"

	"compiler/compat"
	"compiler/internal/escape2/domain"
	"compiler/internal/escape2/eir"
	"compiler/internal/escape2/summary"
	"compiler/pu"
)

// prevEnv is a testEnv that gives previous iterates (PreviousEnv).
type prevEnv struct {
	*testEnv
	prev map[eir.FuncID]*summary.Summary
}

func (e prevEnv) Previous(id eir.FuncID) *summary.Summary { return e.prev[id] }

// saturatedSummary is a summary saturated for input in of a function of n
// inputs (saturate.go).
func saturatedSummary(n, in int) *summary.Summary {
	root := func(i int) domain.Key { return domain.MakeKey(i, domain.Root, domain.KeyFlags{GE: true}) }
	s := pu.New(summary.Summary{Inputs: n, Esc: keys(root(in)), HeapStore: keys(root(in))})
	for j := 0; j < n; j++ {
		s.Store = compat.Append(s.Store, summary.StorePair{Src: root(in), Dst: root(j)})
		if j != in {
			s.Store = compat.Append(s.Store, summary.StorePair{Src: root(j), Dst: root(in)})
		}
	}
	return s
}

// satFunc is f(p, q): p's pointers two loads deep escape and are stored
// into q, and a frame object is stored through p's pointer.
func satFunc(e *testEnv) *fb {
	b := e.fn("f", 2, 0)
	x := b.obj("x", eir.LifeFrame, -1)
	p, q := b.input(0), b.input(1)
	b.at(1).sink(eir.SinkGlobal, b.load(b.load(p)))
	b.at(2).store(q, b.load(b.field(p, 1)))
	b.at(3).store(b.load(p), b.addr(x))
	return b
}

func solveEnv(t *testing.T, env Env, f *eir.Func) Result {
	t.Helper()
	ws := newWorkspace()
	return Solve(f, env, &ws)
}

// TestSaturatedInput: with a previous iterate saturated for input 0, the
// solve starts it as In(0, Root, GE): its entries are the root with GE, which
// the previous iterate holds, input 1's stay exact, and the frame finding is
// the same as without.
func TestSaturatedInput(t *testing.T) {
	e := newEnv()
	b := satFunc(e)
	plain := solveEnv(t, e, b.f)
	env := pu.New(prevEnv{e, map[eir.FuncID]*summary.Summary{b.f.ID: saturatedSummary(2, 0)}})
	sat := solveEnv(t, env, b.f)
	wantFindings(t, plain, "E3002@3")
	wantFindings(t, sat, "E3002@3")
	root0 := domain.MakeKey(0, domain.Root, domain.KeyFlags{GE: true})
	exact := false
	for _, k := range plain.Summary.Esc {
		exact = exact || k.Input() == 0 && k != root0
	}
	if !exact {
		t.Fatal("the solve that follows every path made no key of input 0 at a path: the test tests nothing")
	}
	for _, k := range sat.Summary.Esc {
		if k.Input() == 0 && k != root0 && k != root0.WithStrict() {
			t.Errorf("saturated input 0 made the key %v", k)
		}
	}
	for _, p := range sat.Summary.Store {
		if p.Src.Input() == 0 && p.Src != root0 {
			t.Errorf("saturated input 0 made the store source %v", p.Src)
		}
		if p.Dst.Input() == 1 && p.Dst.GE() {
			t.Errorf("input 1, not saturated, made the GE destination %v", p.Dst)
		}
	}
	// What the scheduler keeps, the previous iterate joined with this one,
	// covers the solve from every path.
	joined := summary.Join(e.paths, env.prev[b.f.ID], sat.Summary)
	if !summary.Leq(e.paths, plain.Summary, joined) {
		t.Error("the joined summary does not cover the solve that follows every path")
	}
}

// TestSaturatedInputPartial: a previous iterate that lacks one entry of the
// saturation (here the store of input 1 into input 0) leaves the input
// exact.
func TestSaturatedInputPartial(t *testing.T) {
	e := newEnv()
	b := satFunc(e)
	plain := solveEnv(t, e, b.f)
	prev := saturatedSummary(2, 0)
	prev.Store = prev.Store[:len(prev.Store)-1]
	if got := saturatedInputs(b.f, prev); got != nil {
		t.Fatalf("saturatedInputs = %v, want none", got)
	}
	env := pu.New(prevEnv{e, map[eir.FuncID]*summary.Summary{b.f.ID: prev}})
	sat := solveEnv(t, env, b.f)
	if !summary.Leq(e.paths, sat.Summary, plain.Summary) || !summary.Leq(e.paths, plain.Summary, sat.Summary) {
		t.Error("a partial saturation changed the summary")
	}
}

// TestSaturatedInputResults: an input a result holds only at a path, not
// at its root with GE, is not saturated; one no result holds is.
func TestSaturatedInputResults(t *testing.T) {
	e := newEnv()
	b := e.fn("g", 1, 1)
	prev := saturatedSummary(1, 0)
	prev.Res = compat.Make[summary.Result](1)
	if got := saturatedInputs(b.f, prev); len(got) != 1 || !got[0] {
		t.Fatalf("no result holds input 0: saturatedInputs = %v", got)
	}
	prev.Res[0].From = compat.Append[summary.ResultKey](nil, summary.ResultKey{Key: domain.MakeKey(0, e.paths.Of(domain.StepLoad), domain.KeyFlags{})})
	if got := saturatedInputs(b.f, prev); got != nil {
		t.Fatalf("a result holds input 0 at a path: saturatedInputs = %v", got)
	}
	prev.Res[0].From = compat.Append[summary.ResultKey](nil, summary.ResultKey{Key: domain.MakeKey(0, domain.Root, domain.KeyFlags{GE: true})})
	if got := saturatedInputs(b.f, prev); len(got) != 1 || !got[0] {
		t.Fatalf("a result holds input 0 anywhere: saturatedInputs = %v", got)
	}
}
