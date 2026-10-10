package ssa

import "testing"

// TestScheduleLoadPastDisjointStore checks that a load reading the memory
// from before a store it cannot alias keeps its place after that store,
// and that a load it may alias still goes first.
func TestScheduleLoadPastDisjointStore(t *testing.T) {
	f := NewFunc("f")
	f.Params = []*Type{TPtr}
	f.Results = []*Type{TI64}
	b := f.Entry
	b.Kind = BlockRet
	mem := b.NewValue(OpInitMem, TMem)
	p := b.NewValueI(OpArg, TPtr, 0)
	p8 := b.NewValueI(OpOffPtr, TPtr, 8, p)
	one := f.ConstInt(TI64, 1)
	st0 := b.NewValueA(OpStore, TMem, TI64, p, one, mem)  // writes p[0]
	ld8 := b.NewValue(OpLoad, TI64, p8, mem)              // p[8], from before st0
	ld0 := b.NewValue(OpLoad, TI64, p, mem)               // p[0], from before st0
	st8 := b.NewValueA(OpStore, TMem, TI64, p8, one, st0) // writes p[8]
	sum := b.NewValue(OpAdd, TI64, ld8, ld0)
	b.SetControl(st8, sum)
	if err := f.Verify(); err != nil {
		t.Fatal(err)
	}
	Schedule(f)
	if err := f.Verify(); err != nil {
		t.Fatal(err)
	}
	pos := map[*Value]int{}
	for i, v := range b.Values {
		pos[v] = i
	}
	if pos[ld0] > pos[st0] {
		t.Errorf("load of p[0] moved below the store to p[0]:\n%s", f)
	}
	if pos[ld8] < pos[st0] || pos[ld8] > pos[st8] {
		t.Errorf("load of p[8] should stay between the stores:\n%s", f)
	}
}

func TestDisjoint(t *testing.T) {
	f := NewFunc("f")
	b := f.Entry
	p := b.NewValueI(OpArg, TPtr, 0)
	q := b.NewValueI(OpArg, TPtr, 1)
	s1 := b.NewValueA(OpSymAddr, TPtr, &Sym{Name: "a"})
	s2 := b.NewValueA(OpSymAddr, TPtr, &Sym{Name: "b"})
	off := func(x *Value, c int64) *Value { return b.NewValueI(OpOffPtr, TPtr, c, x) }
	tests := []struct {
		p    *Value
		n    int64
		q    *Value
		m    int64
		want bool
	}{
		{p, 8, off(p, 8), 8, true},
		{p, 8, off(p, 4), 8, false},
		{p, 8, q, 8, false},             // unknown pointers
		{s1, 8, s2, 8, true},            // different globals
		{off(s1, 16), 8, s2, -1, true},  // unknown size, different globals
		{s1, -1, off(s1, 64), 8, false}, // unknown size, same global
		{off(s1, 8), 8, off(s1, 16), 8, true},
	}
	for i, tc := range tests {
		if got := Disjoint(tc.p, tc.n, tc.q, tc.m); got != tc.want {
			t.Errorf("%d: Disjoint = %v, want %v", i, got, tc.want)
		}
	}
	if !SameBytes(off(off(p, 8), 8), off(p, 16)) {
		t.Error("p+8+8 and p+16 should be the same bytes")
	}
}
