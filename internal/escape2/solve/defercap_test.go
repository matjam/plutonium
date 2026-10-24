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

// capCand is a candidate of a function value that leaks its record: what
// a fallback of a call of the value applies.
func capCand(t *testing.T, e *testEnv) eir.FuncID {
	c := e.fn("leak", 1, 0) // (record)
	c.sink(eir.SinkGlobal, c.input(0))
	e.solve(t, c.f)
	return c.f.ID
}

// A closure that calls more closures of its record than
// MaxDeferredOrigins (a type checker's walker of nested closures): its
// own calls are not counted, so each stays deferred and none falls back.
// A fallback there would bind the record to the rest of the value, whose
// paths through the record the candidate's entries cut to GE.
func TestDeferredOwnCallsNotCapped(t *testing.T) {
	e := newEnv()
	leak := capCand(t, e)
	f := e.fn("walker", 1, 0) // (record)
	n := MaxDeferredOrigins + 4
	for j := range n {
		c := &eir.Call{Kind: eir.CallFunc, Target: f.load(f.field(f.input(0), j))}
		f.call(c)
		e.deferred[c] = true
		e.cands[c] = []eir.FuncID{leak}
	}
	r := e.solve(t, f.f)
	if len(r.Summary.Deferred) != n {
		t.Errorf("%d own calls, %d Deferred entries: %s", n, len(r.Summary.Deferred), e.describe(r.Summary))
	}
	if len(r.Summary.Esc) != 0 {
		t.Errorf("an own call fell back: %s", e.describe(r.Summary))
	}
}

// The calls a function forwards from its callees are still capped: past
// MaxDeferredOrigins origin calls of one input, the rest fall back.
func TestDeferredForwardedCallsCapped(t *testing.T) {
	e := newEnv()
	leak := capCand(t, e)
	g := e.fn("forwarder", 1, 0)
	n := MaxDeferredOrigins + 4
	for j := range n {
		c := e.fn("callee", 1, 0)
		call := &eir.Call{Kind: eir.CallFunc, Target: c.load(c.field(c.input(0), j))}
		c.call(call)
		e.deferred[call] = true
		e.cands[call] = []eir.FuncID{leak}
		e.solve(t, c.f)
		g.static(c.f, 0, g.input(0))
	}
	r := e.solve(t, g.f)
	if len(r.Summary.Deferred) != MaxDeferredOrigins {
		t.Errorf("%d forwarded origin calls, %d Deferred entries, want %d: %s", n, len(r.Summary.Deferred), MaxDeferredOrigins, e.describe(r.Summary))
	}
	if len(r.Summary.Esc) == 0 {
		t.Errorf("no forwarded call past the bound fell back: %s", e.describe(r.Summary))
	}
}
