package lower

import (
	"testing"

	"github.com/matjam/plutonium/internal/ssa"
)

// A release can also collect a task-exit flag. Eliding the held reference
// must preserve that result and its effects rather than replacing it by memory.
func TestHoldReleasePreservesStatusCall(t *testing.T) {
	tmp := &ownedTemp{}
	retain := &ssa.Value{Op: ssa.OpCall, Type: ssa.TupleOf(ssa.TMem), Call: &ssa.CallInfo{}}
	release := &ssa.Value{Op: ssa.OpCall, Type: ssa.TupleOf(ssa.TMem), Call: &ssa.CallInfo{}}
	status := &ssa.Value{Op: ssa.OpCall, Type: ssa.TupleOf(ssa.TMem, ssa.TBool), Call: &ssa.CallInfo{Results: []*ssa.Type{ssa.TBool}}}
	effect := &ssa.Value{Op: ssa.OpCall, Type: ssa.TupleOf(ssa.TMem), Call: &ssa.CallInfo{}}
	rec := &holdRec{tmp: tmp, retains: []*ssa.Value{retain}, start: 1, end: 4, closed: true}
	s := &fnState{
		f:           &ssa.Func{},
		callLog:     []*ssa.Value{retain, release, status, effect},
		holdOf:      map[*ownedTemp]*holdRec{tmp: rec},
		strHolds:    []*holdRec{rec},
		tempRelease: map[*ssa.Value]bool{},
	}
	s.markReferenceRelease(release)
	s.noteRelease(tmp, 1)
	s.recordHolds()
	if len(s.f.StrHolds) != 1 {
		t.Fatalf("got %d recorded holds, want one", len(s.f.StrHolds))
	}
	hold := s.f.StrHolds[0]
	if len(hold.Releases) != 1 || hold.Releases[0] != release {
		t.Fatal("task-exit status was included among elidable reference releases")
	}
	if len(hold.Covered) != 2 || hold.Covered[0] != status || hold.Covered[1] != effect {
		t.Fatal("task-exit status effects were omitted from the protected call interval")
	}
}
