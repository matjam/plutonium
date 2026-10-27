// Ported from internal/opt/sroa_test.go at the go-cutover tag.

package opt

import (
	"pluto/testing"

	"compiler/compat"
	"compiler/internal/ssa"
)

// copyThrough builds func(dst, src *[16]byte) { var t [16]byte; t = *src;
// *dst = t } with t's leaves and pads as given, runs SROA, and reports
// whether t was split.
func copyThrough(t *testing.T, leaves []ssa.Leaf, pads []ssa.Pad) bool {
	t.Helper()
	f := ssa.NewFunc("copy")
	f.Params = compat.Append[*ssa.Type](nil, ssa.TPtr, ssa.TPtr)
	b := f.Entry
	b.Kind = ssa.BlockRet
	mem := b.NewValue(ssa.OpInitMem, ssa.TMem)
	dst := b.NewValueI(ssa.OpArg, ssa.TPtr, 0)
	src := b.NewValueI(ssa.OpArg, ssa.TPtr, 1)
	slot := f.NewSlot("t", 16, 8)
	slot.Leaves = compat.Clone(leaves)
	slot.Pads = compat.Clone(pads)
	addr := b.NewValueA(ssa.OpSlotAddr, ssa.TPtr, slot)
	in := b.NewValueI(ssa.OpMove, ssa.TMem, 16, addr, src, mem)
	out := b.NewValueI(ssa.OpMove, ssa.TMem, 16, dst, addr, in)
	b.SetControl(out)
	if err := f.Verify(); err != nil {
		t.Fatal(err)
	}
	SROA(f)
	if err := f.Verify(); err != nil {
		t.Fatal(err)
	}
	for _, s := range f.Slots {
		if s == slot {
			return false
		}
	}
	return true
}

type sroaCase struct {
	name   string
	leaves []ssa.Leaf
	pads   []ssa.Pad
	split  bool
}

func TestSROACoverage(t *testing.T) {
	tests := []sroaCase{
		{"exact", []ssa.Leaf{{Off: 0, Type: ssa.TI64}, {Off: 8, Type: ssa.TF64}}, nil, true},
		{"padded", []ssa.Leaf{{Off: 0, Type: ssa.TI32}, {Off: 8, Type: ssa.TI64}}, []ssa.Pad{{Off: 4, Size: 4}}, true},
		{"tailPad", []ssa.Leaf{{Off: 0, Type: ssa.TI64}, {Off: 8, Type: ssa.TBool}}, []ssa.Pad{{Off: 9, Size: 7}}, true},
		// The bytes.IndexAny bug: an sret buffer listed the leaves of one
		// result and none for another held in memory, and splitting the
		// buffer dropped the second result.
		{"missingLeaf", []ssa.Leaf{{Off: 0, Type: ssa.TI64}, {Off: 8, Type: ssa.TI32}}, nil, false},
		{"gapNotPadding", []ssa.Leaf{{Off: 0, Type: ssa.TI32}, {Off: 8, Type: ssa.TI64}}, nil, false},
		{"overlap", []ssa.Leaf{{Off: 0, Type: ssa.TI64}, {Off: 4, Type: ssa.TI64}, {Off: 12, Type: ssa.TI32}}, nil, false},
		{"tooBig", []ssa.Leaf{{Off: 0, Type: ssa.TI64}, {Off: 8, Type: ssa.TI64}}, []ssa.Pad{{Off: 16, Size: 8}}, false},
		{"emptyPad", []ssa.Leaf{{Off: 0, Type: ssa.TI64}, {Off: 8, Type: ssa.TI64}}, []ssa.Pad{{Off: 16, Size: 0}}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := copyThrough(t, tc.leaves, tc.pads); got != tc.split {
				t.Errorf("split = %v, want %v", got, tc.split)
			}
		})
	}
}

// TestSROAMoveWithinSlot checks that a copy from one half of a slot to the
// other keeps the slot whole: split leaf by leaf, the copy would read
// leaves it had already overwritten.
func TestSROAMoveWithinSlot(t *testing.T) {
	f := ssa.NewFunc("shift")
	f.Results = compat.Append[*ssa.Type](nil, ssa.TI64)
	b := f.Entry
	b.Kind = ssa.BlockRet
	mem := b.NewValue(ssa.OpInitMem, ssa.TMem)
	slot := f.NewSlot("t", 16, 8)
	slot.Leaves = compat.Append[ssa.Leaf](nil, ssa.Leaf{Off: 0, Type: ssa.TI64}, ssa.Leaf{Off: 8, Type: ssa.TI64})
	addr := b.NewValueA(ssa.OpSlotAddr, ssa.TPtr, slot)
	hi := b.NewValueI(ssa.OpOffPtr, ssa.TPtr, 8, addr)
	st := b.NewValueA(ssa.OpStore, ssa.TMem, ssa.TI64, addr, f.ConstInt(ssa.TI64, 7), mem)
	mv := b.NewValueI(ssa.OpMove, ssa.TMem, 8, hi, addr, st)
	ld := b.NewValue(ssa.OpLoad, ssa.TI64, hi, mv)
	b.SetControl(mv, ld)
	SROA(f)
	if err := f.Verify(); err != nil {
		t.Fatal(err)
	}
	if len(f.Slots) != 1 || f.Slots[0] != slot {
		t.Errorf("slot split; slots now %v", f.Slots)
	}
}

func TestLeavesCover(t *testing.T) {
	s := &ssa.Slot{Size: 0}
	if !s.LeavesCover() {
		t.Error("empty slot should be covered")
	}
	s = &ssa.Slot{Size: 8, Pads: []ssa.Pad{{Off: 0, Size: 8}}}
	if !s.LeavesCover() {
		t.Error("all-padding slot should be covered")
	}
}
