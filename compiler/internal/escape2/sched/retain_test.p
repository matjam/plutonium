// Ported from internal/escape2/sched/retain_test.go at the go-cutover tag.

package sched

import (
	"pluto/testing"

	"compiler/compat"
	"compiler/internal/escape2/eir"
	"compiler/pu"
)

func retainPut(r *retained, id eir.FuncID, solves, bytes int) {
	r.solves[id] = solves
	k := pu.New(keptSolve{ws: r.workspace(), st: pu.New(fakeKept{bytes: bytes})})
	r.kept[id] = k
	r.account(id, k)
}

// TestRetainEvictsLeastSolved checks which kept states go past the budget:
// those of the functions solved the fewest times, the largest first among
// equals, the one just solved included.
func TestRetainEvictsLeastSolved(t *testing.T) {
	saved := retainBudget
	defer func() { retainBudget = saved }()
	retainBudget = 30
	r := newRetained()
	put := func(id eir.FuncID, solves, bytes int) { retainPut(r, id, solves, bytes) }
	put(1, 5, 10)
	put(2, 1, 10)
	put(3, 3, 10)
	put(4, 3, 10) // 40 bytes: 2 (solved once) goes
	if _, ok := r.kept[2]; ok || len(r.kept) != 3 {
		t.Errorf("kept %v, want 2 dropped (fewest solves)", keysOf(r.kept))
	}
	put(5, 1, 15) // the one just solved, solved least: it goes
	if _, ok := r.kept[5]; ok || len(r.kept) != 3 {
		t.Errorf("kept %v, want 5 dropped (just solved, fewest solves)", keysOf(r.kept))
	}
	put(6, 9, 15) // 3 and 4 tie on solves: both go to fit 6
	if _, ok := r.kept[6]; !ok || len(r.kept) != 2 || r.kept[1] == nil {
		t.Errorf("kept %v, want 1 and 6", keysOf(r.kept))
	}
	if r.bytes > retainBudget {
		t.Errorf("bytes %d past the budget %d", r.bytes, retainBudget)
	}
}

func keysOf(m map[eir.FuncID]*keptSolve) []eir.FuncID {
	var out []eir.FuncID
	for id := range m {
		out = compat.Append(out, id)
	}
	sortIDs(out)
	return out
}
