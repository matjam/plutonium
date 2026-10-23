package solve

import (
	"testing"

	"github.com/matjam/plutonium/internal/escape2/eir"
)

// A recursive walker shaped like ast.inspect: walk(t, f) calls f, whose
// value is heap memory on the recursive call, so that call falls back to
// its candidates, each of which calls its argument (a function value it is
// given) as a deferred call of its own; and walk calls t.2 as a deferred
// call of its own code. The candidates' deferred calls come from more
// origins than MaxDeferredOrigins, of the same input as walk's own call,
// and from functions made before walk (ranked first). Made inside the
// fallback, they resolve there and are not counted (dcall.fb): walk's own
// call stays deferred, and does not fall back in turn.
func TestDeferredFallbackDoesNotFeedCap(t *testing.T) {
	e := newEnv()
	var cands []eir.FuncID
	for range MaxDeferredOrigins + 1 {
		c := e.fn("cand", 2, 0) // (g, record): calls g
		call := &eir.Call{Kind: eir.CallFunc, Target: c.input(0)}
		c.call(call)
		e.deferred[call] = true
		e.solve(t, c.f)
		cands = append(cands, c.f.ID)
	}
	walk := e.fn("walk", 2, 0) // (t, f)
	rec := &eir.Call{Kind: eir.CallStatic, Callee: walk.f.ID, Args: []eir.Reg{walk.load(walk.field(walk.input(0), 0)), walk.heap()}}
	walk.call(rec)
	c1 := &eir.Call{Kind: eir.CallFunc, Target: walk.input(1), Args: []eir.Reg{walk.load(walk.field(walk.input(0), 1))}}
	walk.call(c1)
	e.deferred[c1] = true
	e.cands[c1] = cands
	c2 := &eir.Call{Kind: eir.CallFunc, Target: walk.load(walk.field(walk.input(0), 2))}
	walk.call(c2)
	e.deferred[c2] = true
	var r Result
	for range 6 {
		r = e.solve(t, walk.f)
	}
	own, fromCands := false, 0
	for _, d := range r.Summary.Deferred {
		switch {
		case d.Origin == walk.f.ID && d.Site == c2.Site:
			own = true
		case d.Origin != walk.f.ID:
			fromCands++
		}
	}
	for _, id := range cands {
		if len(e.sums[id].Deferred) == 0 {
			t.Fatal("a candidate defers nothing: the test tests nothing")
		}
	}
	if fromCands != 0 {
		t.Errorf("walk defers %d calls of the fallback's candidates: %s", fromCands, e.describe(r.Summary))
	}
	if !own {
		t.Errorf("walk's own call fell back: %s", e.describe(r.Summary))
	}
}
