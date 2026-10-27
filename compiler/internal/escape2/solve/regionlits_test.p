// Ported from internal/escape2/solve/regionlits_test.go at the go-cutover tag.

package solve

import (
	"pluto/testing"

	"compiler/internal/escape2/domain"
	"compiler/internal/escape2/eir"
)

// regionLitEnv makes deferEnv's iter(p, yield), which defers yield(p) with
// body (which keeps its argument) the call's only candidate, and
// mk(q), which returns a closure of lit capturing q, built in the region its
// caller provides: lit keeps what it captured, not its argument.
func regionLitEnv(t *testing.T) (*testEnv, *fb, *fb, *fb) {
	e, _, iter := deferEnv(t)
	e.solve(t, iter.f)
	lit := e.fn("lit", 2, 0) // (x, record): keeps the record's first capture
	lit.sink(eir.SinkGlobal, lit.load(lit.field(lit.input(1), 0)))
	e.solve(t, lit.f)
	mk := e.fn("mk", 1, 1)
	rec := mk.obj("rec", eir.LifeCaller, -1)
	mk.f.Objects[rec].Kind = eir.ObjClosure
	clo := mk.reg()
	mk.emit(eir.Instr{Op: eir.OpClosure, Dst: clo, Obj: rec, Callee: lit.f.ID, Args: regs(mk.input(0))})
	mk.ret(0, clo)
	r := e.solve(t, mk.f)
	if got := r.Summary.Region.Lits; len(got) != 1 || got[0] != lit.f.ID {
		t.Fatalf("mk's region literals are %v, want [%d]", got, lit.f.ID)
	}
	return e, iter, lit, mk
}

// TestRegionLiteralResolved: user(q, r) passes mk(q)'s closure to iter
// with r: the deferred call applies lit, which keeps q, not body, which
// would keep r.
func TestRegionLiteralResolved(t *testing.T) {
	e, iter, _, mk := regionLitEnv(t)
	b := e.fn("user", 2, 0)
	f := b.static(mk.f, 1, b.input(0))
	b.static(iter.f, 0, b.input(1), f[0])
	r := e.solve(t, b.f)
	q := domain.MakeKey(0, domain.Root, domain.KeyFlags{})
	keepsQ, keepsR := false, false
	for _, k := range r.Summary.Esc {
		keepsQ = keepsQ || k.Input() == 0 && (k == q || k.GE() || k.Deep())
		keepsR = keepsR || k.Input() == 1
	}
	if !keepsQ || keepsR {
		t.Errorf("user keeps %s; want q (lit keeps its capture) and not r (body is not called)", e.describe(r.Summary))
	}
}

// TestRegionLiteralUnknown: a region whose literals the summary does not
// all name (eir.NoFunc) falls back to the call's candidates.
func TestRegionLiteralUnknown(t *testing.T) {
	e, iter, _, mk := regionLitEnv(t)
	e.sums[mk.f.ID].Region.Lits = funcIDs(eir.NoFunc)
	b := e.fn("user", 2, 0)
	f := b.static(mk.f, 1, b.input(0))
	b.static(iter.f, 0, b.input(1), f[0])
	r := e.solve(t, b.f)
	keepsR := false
	for _, k := range r.Summary.Esc {
		keepsR = keepsR || k.Input() == 1
	}
	if !keepsR {
		t.Errorf("user keeps %s; the fallback's body keeps r", e.describe(r.Summary))
	}
}

// TestRegionLiteralThroughCall: a function that returns mk's closure
// names mk's literal in its own summary, and its callers resolve the call
// the same way.
func TestRegionLiteralThroughCall(t *testing.T) {
	e, iter, lit, mk := regionLitEnv(t)
	w := e.fn("wrap", 1, 1)
	res := w.static(mk.f, 1, w.input(0))
	w.ret(0, res[0])
	rw := e.solve(t, w.f)
	if got := rw.Summary.Region.Lits; len(got) != 1 || got[0] != lit.f.ID {
		t.Fatalf("wrap's region literals are %v, want [%d]", got, lit.f.ID)
	}
	b := e.fn("user", 2, 0)
	f := b.static(w.f, 1, b.input(0))
	b.static(iter.f, 0, b.input(1), f[0])
	r := e.solve(t, b.f)
	for _, k := range r.Summary.Esc {
		if k.Input() == 1 {
			t.Errorf("user keeps r: %s", e.describe(r.Summary))
		}
	}
	_ = mk
}
