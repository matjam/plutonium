package amd64

import (
	"fmt"
	"math"
	"reflect"
	"slices"
	"testing"
)

// Compare the filtered graph with the preceding unfiltered algorithm, including
// insertion order and spill costs, across mixed classes, moves and call clobbers.
func TestInterferenceClassFiltering(t *testing.T) {
	f := newMFunc("mixed")
	g0, g1 := f.newVReg(ClassGP), f.newVReg(ClassGP)
	x0, x1 := f.newVReg(ClassXMM), f.newVReg(ClassXMM)
	for range 130 {
		f.newVReg(ClassGP)
	}
	f.Blocks = []*MBlock{
		{Depth: 0, Insts: []*MInst{
			{Op: MDEF, ImplicitDefs: []Reg{g0, x0, g1, x1}},
			{Op: MCOPY, Size: 8, Dst: RegOp(g1), Src: RegOp(g0)},
			{Op: MCOPY, Size: 16, Dst: RegOp(x1), Src: RegOp(x0)},
		}},
		{Depth: 3, Insts: []*MInst{
			{Op: MCALL, ImplicitDefs: append([]Reg(nil), callerSaved...), ImplicitUses: []Reg{g0, x0}},
			{Op: MUSE, ImplicitUses: []Reg{g0, g1, x0, x1, RSP}},
		}},
	}
	liveOut := []regSet{newRegSet(f.numRegIndexes()), newRegSet(f.numRegIndexes())}
	for _, s := range liveOut {
		for i := range f.numRegIndexes() {
			s.add(i)
		}
	}
	for _, grown := range []bool{false, true} {
		if grown {
			// The GP spill rewrite can grow the register set before the XMM
			// allocator uses the previously computed, shorter liveOut sets.
			var last Reg
			for range 64 {
				last = f.newVReg(ClassGP)
			}
			f.Blocks[1].Insts[1].ImplicitUses = append(f.Blocks[1].Insts[1].ImplicitUses, last)
		}
		for _, class := range []RegClass{ClassGP, ClassXMM} {
			got, want := newAllocator(f, class, nil), newAllocator(f, class, nil)
			got.build(liveOut)
			buildUnfilteredInterference(want, liveOut)
			for name, pair := range map[string][2]any{
				"edges": {got.adjSet, want.adjSet}, "adjacency order": {got.adjList, want.adjList},
				"degrees": {got.degree, want.degree}, "costs": {got.cost, want.cost},
				"moves": {got.moves, want.moves}, "move lists": {got.moveList, want.moveList},
				"move order": {got.moveOrder, want.moveOrder}, "worklist moves": {got.worklistMoves, want.worklistMoves},
			} {
				if !reflect.DeepEqual(pair[0], pair[1]) {
					t.Fatalf("class %d, grown %v changed %s", class, grown, name)
				}
			}
		}
	}
}

// Historical graph construction is an independent parity oracle for the class
// intersection optimization. It deliberately retains cross-class live entries.
func buildUnfilteredInterference(a *allocator, liveOut []regSet) {
	f := a.f
	live := newRegSet(f.numRegIndexes())
	var defs, uses []Reg
	for bi := len(f.Blocks) - 1; bi >= 0; bi-- {
		b := f.Blocks[bi]
		live.copyFrom(liveOut[bi])
		weight := math.Pow(10, float64(min(b.Depth, 6)))
		for ii := len(b.Insts) - 1; ii >= 0; ii-- {
			in := b.Insts[ii]
			defs, uses = in.Defs(defs[:0]), in.Uses(uses[:0])
			for _, rs := range [][]Reg{defs, uses} {
				for _, r := range rs {
					if a.inClass(r) && !r.IsPhys() {
						a.cost[r] += weight
					}
				}
			}
			if in.isMove() && a.inClass(in.Dst.Reg) && a.inClass(in.Src.Reg) {
				live.remove(f.regIndex(in.Src.Reg))
				a.moveList[in.Dst.Reg] = append(a.moveList[in.Dst.Reg], in)
				a.moveList[in.Src.Reg] = append(a.moveList[in.Src.Reg], in)
				a.moveOrder[in] = len(a.moves)
				a.moves = append(a.moves, in)
				a.addWorklistMove(in)
			}
			for _, d := range defs {
				live.add(f.regIndex(d))
			}
			for _, d := range defs {
				if a.inClass(d) {
					live.forEach(func(i int) {
						if r := f.indexReg(i); a.inClass(r) {
							a.addEdge(r, d)
						}
					})
				}
			}
			for _, d := range defs {
				live.remove(f.regIndex(d))
			}
			for _, u := range uses {
				live.add(f.regIndex(u))
			}
		}
	}
}

func TestRegSetIteration(t *testing.T) {
	s := newRegSet(192)
	var got []int
	collect := func(i int) { got = append(got, i) }
	s.forEach(collect)
	if len(got) != 0 {
		t.Fatalf("empty set visits %v", got)
	}
	for i := range 192 {
		s.add(i)
		got = nil
		s.forEach(collect)
		if !slices.Equal(got, []int{i}) {
			t.Fatalf("single bit %d visits %v", i, got)
		}
		s.remove(i)
	}
	want := []int{0, 1, 31, 32, 62, 63, 64, 65, 127, 128, 191}
	for _, i := range want {
		s.add(i)
	}
	got = nil
	s.forEach(collect)
	if !slices.Equal(got, want) {
		t.Fatalf("want ascending visits %v, got %v", want, got)
	}
	for _, i := range want {
		if !s.has(i) {
			t.Fatalf("iteration changed bit %d", i)
		}
	}
}

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
