package amd64

import (
	"fmt"
	"slices"
	"testing"
)

// TestRewriteSpillOrder checks that an instruction defining several spilled
// registers gets its spill stores in definition order, so that the output
// does not depend on map iteration order. Each round builds a fresh function
// so that a map-ordered rewrite would almost surely differ in some round.
func TestRewriteSpillOrder(t *testing.T) {
	const n = 8
	for round := range 20 {
		f := newMFunc("t")
		var defs []Reg
		for range n {
			defs = append(defs, f.newVReg(ClassGP))
		}
		def := &MInst{Op: MDEF, ImplicitDefs: append([]Reg(nil), defs...)}
		f.Blocks = []*MBlock{{Insts: []*MInst{def}}}
		a := newAllocator(f, ClassGP, map[Reg]bool{})
		a.spilled = defs
		a.rewrite(map[Reg]bool{})

		insts := f.Blocks[0].Insts
		if len(insts) != 1+n || insts[0] != def {
			t.Fatalf("round %d: got %d instructions, want the def and %d spills", round, len(insts), n)
		}
		for i, in := range insts[1:] {
			want := fmt.Sprintf("spill%d", defs[i]-FirstVirtual)
			if in.Op != MSPILL || in.Slot.Name != want || in.Src.Reg != def.ImplicitDefs[i] {
				t.Fatalf("round %d: spill %d is %s to %s from %s, want %s from %s",
					round, i, in.Op, in.Slot.Name, in.Src.Reg, want, def.ImplicitDefs[i])
			}
		}
	}
}

func TestIntHeap(t *testing.T) {
	var h intHeap
	in := []int{5, 3, 9, 1, 1, 7, 0, 12, 4, 8, 2}
	for _, x := range in {
		h.push(x)
	}
	var got []int
	for len(h) > 0 {
		got = append(got, h[0])
		h.pop()
	}
	want := slices.Clone(in)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("heap order %v, want %v", got, want)
	}
}

// chain builds a function whose blocks run b0 -> b1 -> ... -> b(n-1) but
// are laid out in reverse after the entry, defines a register in b0 and
// uses it in the last block.
func chain(n int) (*MFunc, Reg) {
	f := newMFunc("chain")
	r := f.newVReg(ClassGP)
	blocks := make([]*MBlock, n)
	for i := range blocks {
		blocks[i] = &MBlock{ID: i}
	}
	for i := 0; i+1 < n; i++ {
		blocks[i].Succs = []*MBlock{blocks[i+1]}
		blocks[i+1].Preds = []*MBlock{blocks[i]}
	}
	blocks[0].Insts = []*MInst{{Op: MMOV, Size: 8, Dst: RegOp(r), Src: ImmOp(1)}}
	blocks[n-1].Insts = []*MInst{{Op: MMOV, Size: 8, Dst: RegOp(RAX), Src: RegOp(r)}, {Op: MRET}}
	f.Blocks = append([]*MBlock{blocks[0]}, blocks[1:]...)
	slices.Reverse(f.Blocks[1:])
	return f, r
}

func TestPostorder(t *testing.T) {
	f, _ := chain(4) // layout b0 b3 b2 b1
	index := map[*MBlock]int{}
	for i, b := range f.Blocks {
		index[b] = i
	}
	got := postorder(f, index)
	want := []int{1, 2, 3, 0} // b3 b2 b1 b0
	if !slices.Equal(got, want) {
		t.Fatalf("postorder = %v, want %v", got, want)
	}
}

// TestLivenessLayout checks that a register defined at the top of a long
// chain is live out of every block before its use, whatever the layout.
func TestLivenessLayout(t *testing.T) {
	f, r := chain(200)
	live := liveness(f)
	x := f.regIndex(r)
	for i, b := range f.Blocks {
		want := b.ID != 199 // live out of all but the block that uses it
		if live[i].has(x) != want {
			t.Fatalf("block %d: live out %v, want %v", b.ID, live[i].has(x), want)
		}
	}
}
