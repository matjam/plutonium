// Ported from internal/escape2/solve/regionself_test.go at the go-cutover tag.

package solve

import (
	"pluto/testing"

	"compiler/internal/escape2/eir"
)

// TestRegionSelf: a function whose returned closure captures only an
// input has no Region.Self; one whose returned closure captures another
// closure it builds in its caller's memory has Self, and a caller's region
// for it then holds the region itself.
func TestRegionSelf(t *testing.T) {
	e, _, lit, mk := regionLitEnv(t)
	if e.sums[mk.f.ID].Region.Self {
		t.Errorf("mk's region holds a pointer into itself: %+v", e.sums[mk.f.ID].Region)
	}
	two := e.fn("two", 1, 1)
	inner := two.obj("inner", eir.LifeCaller, -1)
	two.f.Objects[inner].Kind = eir.ObjClosure
	c1 := two.reg()
	two.emit(eir.Instr{Op: eir.OpClosure, Dst: c1, Obj: inner, Callee: lit.f.ID, Args: regs(two.input(0))})
	outer := two.obj("outer", eir.LifeCaller, -1)
	two.f.Objects[outer].Kind = eir.ObjClosure
	c2 := two.reg()
	two.emit(eir.Instr{Op: eir.OpClosure, Dst: c2, Obj: outer, Callee: lit.f.ID, Args: regs(c1)})
	two.ret(0, c2)
	r := e.solve(t, two.f)
	if !r.Summary.Region.Self {
		t.Errorf("two's outer closure captures its inner one, yet no Self: %+v", r.Summary.Region)
	}
	// A caller that keeps what the returned closure's record leads to
	// keeps the region (the inner closure is in it).
	b := e.fn("user", 1, 0)
	f := b.static(two.f, 1, b.input(0))
	b.at(7).sink(eir.SinkGlobal, b.load(b.field(f[0], 0)))
	ru := e.solve(t, b.f)
	wantFindings(t, ru, "E3002@7")
}
