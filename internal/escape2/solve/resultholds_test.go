package solve

import (
	"testing"

	"github.com/matjam/plutonium/internal/escape2/eir"
)

// //pu:resultholds p (eir.Func.ResultHolds): the function's escapes of p
// are left out of its summary, and what its result holds still reaches
// the caller, which checks it as any result. A caller that keeps the
// result past the frame p points into is still rejected.
func TestResultHolds(t *testing.T) {
	e := newEnv()
	use := e.fn("use", 1, 1) // stores a in package memory, returns it
	use.f.ResultHolds = []int{0}
	use.sink(eir.SinkGlobal, use.input(0))
	use.ret(0, use.input(0))
	r := e.solve(t, use.f)
	if len(r.Summary.Esc) != 0 || len(r.Summary.HeapStore) != 0 {
		t.Errorf("use keeps its //pu:resultholds parameter: %s", e.describe(r.Summary))
	}
	if len(r.Summary.Res) != 1 || len(r.Summary.Res[0].From) == 0 {
		t.Errorf("use's result does not hold its parameter: %s", e.describe(r.Summary))
	}

	local := e.fn("local", 0, 0) // uses a frame value for the call only
	x := local.obj("x", eir.LifeFrame, -1)
	local.static(use.f, 1, local.addr(x))
	wantFindings(t, e.solve(t, local.f), "")

	leaks := e.fn("leaks", 0, 1) // returns what holds its frame value
	y := leaks.obj("y", eir.LifeFrame, -1)
	res := leaks.static(use.f, 1, leaks.addr(y))
	leaks.at(9).ret(0, res[0])
	wantFindings(t, e.solve(t, leaks.f), "E3001@9")
}
