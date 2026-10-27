// Ported from internal/arch/amd64/regalloc_test.go at the go-cutover tag.

package amd64

import (
	"pluto/math"
	"pluto/slices"
	"pluto/testing"

	"compiler/compat"
	"compiler/pu"
)

// regs returns a heap copy of its arguments, for the implicit register lists
// of an instruction.
func regs(rs ...Reg) []Reg { return compat.Clone(rs) }

// insts returns a heap slice of its arguments.
func insts(is ...*MInst) []*MInst {
	var out []*MInst
	for _, in := range is {
		out = compat.Append(out, in)
	}
	return out
}

func newInst(m MInst) *MInst { return pu.New(m) }

func TestMoveStateTransitions(t *testing.T) {
	f := newMFunc("moves")
	u, v := f.newVReg(ClassGP), f.newVReg(ClassGP)
	a := newAllocator(f, ClassGP, nil)
	active, worklist, both, dead := newInst(MInst{}), newInst(MInst{}), newInst(MInst{}), newInst(MInst{})
	a.moves = insts(active, worklist, both, dead)
	for i, m := range a.moves {
		a.moveOrder[m] = i
	}
	a.moveList[u] = insts(dead, both, worklist, active, active)
	a.moveList[v] = insts(active, both)
	a.activeMoves[active], a.activeMoves[both] = true, true
	a.addWorklistMove(worklist)
	a.addWorklistMove(both)
	if !a.moveRelated(u) || !a.moveRelated(v) || a.moveRelated(RSP) {
		t.Fatal("active and worklist moves must participate, absent moves must not")
	}
	// The Go test also checked with testing.AllocsPerRun that the predicate
	// does not allocate; pluto/testing has no such counter, and a map read
	// cannot allocate in Plutonium.
	a.enableMoves(regs(u, v, u))
	if len(a.activeMoves) != 0 || len(a.worklistMoves) != 3 || a.worklistMoves[dead] {
		t.Fatalf("wrong enabled move states: active=%d, worklist=%d", len(a.activeMoves), len(a.worklistMoves))
	}
	// Shared nodes and repeated entries must not enqueue a move a second time.
	if len(a.pending) != 4 {
		t.Fatalf("enabled moves inserted %d heap entries, want 4", len(a.pending))
	}
	for i, want := range insts(active, worklist, both) {
		if got := a.firstMove(); got != want {
			t.Fatalf("move order: move %d came out wrong", i)
		}
		delete(a.worklistMoves, want)
	}
	if a.firstMove() != nil || a.moveRelated(u) {
		t.Fatal("completed moves must leave the worklist and predicate")
	}
}

func TestFreezeMoveStates(t *testing.T) {
	f := newMFunc("freeze")
	u, v, w := f.newVReg(ClassGP), f.newVReg(ClassGP), f.newVReg(ClassGP)
	a := newAllocator(f, ClassGP, nil)
	active := newInst(MInst{Dst: RegOp(u), Src: RegOp(v)})
	worklist := newInst(MInst{Dst: RegOp(u), Src: RegOp(w)})
	a.moveList[u], a.moveList[v], a.moveList[w] = insts(active, worklist), insts(active), insts(worklist)
	a.activeMoves[active], a.worklistMoves[worklist] = true, true
	a.freezeWL[v], a.freezeWL[w] = true, true
	a.freezeMoves(u)
	if a.activeMoves[active] || !a.frozenMoves[active] || !a.frozenMoves[worklist] {
		t.Fatal("freeze must retire active moves and mark both participating states")
	}
	if !a.simplifyWL[v] || a.freezeWL[v] || a.simplifyWL[w] || !a.freezeWL[w] || !a.worklistMoves[worklist] {
		t.Fatal("only the neighbor with no remaining active or worklist move can simplify")
	}
}

// sameMap reports whether two maps hold the same entries.
func sameMap[K comparable, V comparable](a, b map[K]V) bool {
	if len(a) != len(b) {
		return false
	}
	for k, x := range a {
		y, ok := b[k]
		if !ok || x != y {
			return false
		}
	}
	return true
}

// sameSliceMap reports whether two maps of slices hold the same entries, in
// the same order.
func sameSliceMap[K comparable, V comparable](a, b map[K][]V) bool {
	if len(a) != len(b) {
		return false
	}
	for k, x := range a {
		y, ok := b[k]
		if !ok || !slices.Equal(x, y) {
			return false
		}
	}
	return true
}

func sameInterference(a, b interference) bool {
	return slices.Equal(a.bits, b.bits) && (a.big == nil) == (b.big == nil) && sameMap(a.big, b.big)
}

// Compare the filtered graph with the preceding unfiltered algorithm, including
// insertion order and spill costs, across mixed classes, moves and call clobbers.
func TestInterferenceClassFiltering(t *testing.T) {
	f := newMFunc("mixed")
	g0, g1 := f.newVReg(ClassGP), f.newVReg(ClassGP)
	x0, x1 := f.newVReg(ClassXMM), f.newVReg(ClassXMM)
	for range 130 {
		f.newVReg(ClassGP)
	}
	b0 := pu.New(MBlock{Depth: 0})
	b0.Insts = insts(
		newInst(MInst{Op: MDEF, ImplicitDefs: regs(g0, x0, g1, x1)}),
		newInst(MInst{Op: MCOPY, Size: 8, Dst: RegOp(g1), Src: RegOp(g0)}),
		newInst(MInst{Op: MCOPY, Size: 16, Dst: RegOp(x1), Src: RegOp(x0)}),
	)
	b1 := pu.New(MBlock{Depth: 3})
	use := newInst(MInst{Op: MUSE, ImplicitUses: regs(g0, g1, x0, x1, RSP)})
	b1.Insts = insts(
		newInst(MInst{Op: MCALL, ImplicitDefs: compat.Clone(callerSaved), ImplicitUses: regs(g0, x0)}),
		use,
	)
	f.Blocks = compat.Append(f.Blocks, b0, b1)
	liveOut := compat.Make[regSet](2)
	for i := range liveOut {
		liveOut[i] = newRegSet(f.numRegIndexes())
		for j := range f.numRegIndexes() {
			liveOut[i].add(j)
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
			use.ImplicitUses = compat.Append(use.ImplicitUses, last)
		}
		for _, class := range []RegClass{ClassGP, ClassXMM} {
			got, want := newAllocator(f, class, nil), newAllocator(f, class, nil)
			got.build(liveOut)
			buildUnfilteredInterference(want, liveOut)
			name := ""
			switch {
			case !sameInterference(got.adjSet, want.adjSet):
				name = "edges"
			case !sameSliceMap(got.adjList, want.adjList):
				name = "adjacency order"
			case !sameMap(got.degree, want.degree):
				name = "degrees"
			case !sameMap(got.cost, want.cost):
				name = "costs"
			case !slices.Equal(got.moves, want.moves):
				name = "moves"
			case !sameSliceMap(got.moveList, want.moveList):
				name = "move lists"
			case !sameMap(got.moveOrder, want.moveOrder):
				name = "move order"
			case !sameMap(got.worklistMoves, want.worklistMoves):
				name = "worklist moves"
			}
			if name != "" {
				t.Fatalf("class %d, grown %v changed %s", int(class), grown, name)
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
				a.moveList[in.Dst.Reg] = compat.Append(a.moveList[in.Dst.Reg], in)
				a.moveList[in.Src.Reg] = compat.Append(a.moveList[in.Src.Reg], in)
				a.moveOrder[in] = len(a.moves)
				a.moves = compat.Append(a.moves, in)
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
	collect := func(i int) { got = compat.AppendPlain(got, i) }
	s.forEach(collect)
	if len(got) != 0 {
		t.Fatalf("empty set visits %d entries", len(got))
	}
	for i := range 192 {
		s.add(i)
		got = nil
		s.forEach(collect)
		if len(got) != 1 || got[0] != i {
			t.Fatalf("single bit %d visits %d entries", i, len(got))
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
		t.Fatalf("want ascending visits of %d bits, got %d visits", len(want), len(got))
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
			defs = compat.AppendPlain(defs, f.newVReg(ClassGP))
		}
		def := newInst(MInst{Op: MDEF, ImplicitDefs: compat.Clone(defs)})
		b := pu.New(MBlock{})
		b.Insts = insts(def)
		f.Blocks = compat.Append(f.Blocks, b)
		a := newAllocator(f, ClassGP, map[Reg]bool{})
		a.spilled = defs
		a.rewrite(map[Reg]bool{})

		out := f.Blocks[0].Insts
		if len(out) != 1+n || out[0] != def {
			t.Fatalf("round %d: got %d instructions, want the def and %d spills", round, len(out), n)
		}
		for i, in := range out[1:] {
			want := compat.Sprintfb("spill%d", compat.ArgI(int64(defs[i]-FirstVirtual)))
			if in.Op != MSPILL || in.Slot.Name != want || in.Src.Reg != def.ImplicitDefs[i] {
				t.Fatalf("round %d: spill %d is %s to %s from %s, want %s from %s",
					round, i, in.Op.String(), in.Slot.Name, in.Src.Reg.String(), want, def.ImplicitDefs[i].String())
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
		got = compat.AppendPlain(got, h[0])
		h.pop()
	}
	want := compat.Clone(in)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("heap order is not sorted: got %d entries, want %d", len(got), len(want))
	}
}

// chain builds a function whose blocks run b0 -> b1 -> ... -> b(n-1) but
// are laid out in reverse after the entry, defines a register in b0 and
// uses it in the last block.
func chain(n int) (*MFunc, Reg) {
	f := newMFunc("chain")
	r := f.newVReg(ClassGP)
	blocks := compat.Make[*MBlock](n)
	for i := range blocks {
		blocks[i] = pu.New(MBlock{ID: i})
	}
	for i := 0; i+1 < n; i++ {
		blocks[i].Succs = compat.Append(blocks[i].Succs, blocks[i+1])
		blocks[i+1].Preds = compat.Append(blocks[i+1].Preds, blocks[i])
	}
	blocks[0].Insts = insts(newInst(MInst{Op: MMOV, Size: 8, Dst: RegOp(r), Src: ImmOp(1)}))
	blocks[n-1].Insts = insts(newInst(MInst{Op: MMOV, Size: 8, Dst: RegOp(RAX), Src: RegOp(r)}), newInst(MInst{Op: MRET}))
	f.Blocks = compat.Append(f.Blocks, blocks[0])
	f.Blocks = compat.Append(f.Blocks, blocks[1:]...)
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
		t.Fatalf("postorder has the wrong order: %d, %d, %d, %d", got[0], got[1], got[2], got[3])
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
