package amd64

import (
	"container/heap"
	"fmt"
	"math"
	"sort"

	"github.com/matjam/plutonium/internal/ssa"
)

// expandPCopies turns each parallel copy into a sequence of copies that has
// the same effect, using a temporary to break cycles.
func expandPCopies(f *MFunc) {
	for _, b := range f.Blocks {
		var out []*MInst
		for _, in := range b.Insts {
			if in.Op != MPCOPY {
				out = append(out, in)
				continue
			}
			type mv struct{ dst, src Reg }
			var pending []mv
			for i := range in.Dsts {
				if in.Dsts[i] != in.Srcs[i] {
					pending = append(pending, mv{in.Dsts[i], in.Srcs[i]})
				}
			}
			for len(pending) > 0 {
				progress := false
				for i := 0; i < len(pending); i++ {
					m := pending[i]
					blocked := false
					for j, o := range pending {
						if j != i && o.src == m.dst {
							blocked = true
							break
						}
					}
					if blocked {
						continue
					}
					out = append(out, &MInst{Op: MCOPY, Size: 8, Dst: RegOp(m.dst), Src: RegOp(m.src)})
					pending = append(pending[:i], pending[i+1:]...)
					progress = true
					i--
				}
				if !progress {
					// Every destination is still needed as a source: a cycle.
					// Save one destination's value and redirect its readers.
					m := pending[0]
					t := f.newVReg(f.classOf(m.dst))
					out = append(out, &MInst{Op: MCOPY, Size: 8, Dst: RegOp(t), Src: RegOp(m.dst)})
					for j := range pending {
						if pending[j].src == m.dst {
							pending[j].src = t
						}
					}
				}
			}
		}
		b.Insts = out
	}
}

// ----------------------------------------------------------------------------
// Liveness

// regSet is a set of registers, as a bit set over dense indexes.
type regSet []uint64

func (f *MFunc) regIndex(r Reg) int {
	if r.IsPhys() {
		return int(r)
	}
	return NumPhys + int(r-FirstVirtual)
}

func (f *MFunc) indexReg(i int) Reg {
	if i < NumPhys {
		return Reg(i)
	}
	return Reg(i-NumPhys) + FirstVirtual
}

func (f *MFunc) numRegIndexes() int { return NumPhys + int(f.nextVReg-FirstVirtual) }

func newRegSet(n int) regSet { return make(regSet, (n+63)/64) }

func (s regSet) has(i int) bool { return s[i/64]&(1<<(i%64)) != 0 }
func (s regSet) add(i int)      { s[i/64] |= 1 << (i % 64) }
func (s regSet) remove(i int)   { s[i/64] &^= 1 << (i % 64) }

// unionChanged sets s |= t and reports whether s changed.
func (s regSet) unionChanged(t regSet) bool {
	changed := false
	for i := range s {
		n := s[i] | t[i]
		if n != s[i] {
			s[i] = n
			changed = true
		}
	}
	return changed
}

func (s regSet) copyFrom(t regSet) { copy(s, t) }

func (s regSet) forEach(fn func(i int)) {
	for w, bits := range s {
		for bits != 0 {
			b := bits & -bits
			i := w*64 + bitIndex(b)
			fn(i)
			bits &^= b
		}
	}
}

func bitIndex(b uint64) int {
	n := 0
	for b > 1 {
		b >>= 1
		n++
	}
	return n
}

// liveness computes the registers live out of each block.
func liveness(f *MFunc) []regSet {
	n := f.numRegIndexes()
	use := make([]regSet, len(f.Blocks))
	def := make([]regSet, len(f.Blocks))
	liveOut := make([]regSet, len(f.Blocks))
	liveIn := make([]regSet, len(f.Blocks))
	var buf []Reg
	for i, b := range f.Blocks {
		use[i], def[i] = newRegSet(n), newRegSet(n)
		liveOut[i], liveIn[i] = newRegSet(n), newRegSet(n)
		for _, in := range b.Insts {
			buf = in.Uses(buf[:0])
			for _, r := range buf {
				if x := f.regIndex(r); !def[i].has(x) {
					use[i].add(x)
				}
			}
			buf = in.Defs(buf[:0])
			for _, r := range buf {
				def[i].add(f.regIndex(r))
			}
		}
	}
	index := map[*MBlock]int{}
	for i, b := range f.Blocks {
		index[b] = i
	}
	tmp := newRegSet(n)
	for changed := true; changed; {
		changed = false
		for i := len(f.Blocks) - 1; i >= 0; i-- {
			b := f.Blocks[i]
			for _, s := range b.Succs {
				if liveOut[i].unionChanged(liveIn[index[s]]) {
					changed = true
				}
			}
			// in = use | (out &^ def)
			for w := range tmp {
				tmp[w] = use[i][w] | liveOut[i][w]&^def[i][w]
			}
			if liveIn[i].unionChanged(tmp) {
				changed = true
			}
		}
	}
	return liveOut
}

// ----------------------------------------------------------------------------
// Iterated register coalescing

type edge struct{ a, b Reg }

type allocator struct {
	f      *MFunc
	class  RegClass
	k      int
	colors []Reg // allocatable registers of the class

	precolored map[Reg]bool
	initial    []Reg

	simplifyWL, freezeWL, spillWL map[Reg]bool
	spilled                       []Reg
	coalesced                     map[Reg]bool
	colored                       map[Reg]bool
	selectStack                   []Reg
	onStack                       map[Reg]bool

	moves                                                                     []*MInst
	coalescedMoves, constrainedMoves, frozenMoves, worklistMoves, activeMoves map[*MInst]bool

	// moveOrder is each move's index in moves, and pending a min-heap of
	// the indices of moves added to worklistMoves, so that coalesce finds
	// the first worklist move in program order without scanning moves.
	// Entries for moves that have left the worklist are skipped lazily.
	moveOrder map[*MInst]int
	pending   intHeap

	adjSet    map[edge]bool
	adjList   map[Reg][]Reg
	degree    map[Reg]int
	moveList  map[Reg][]*MInst
	alias     map[Reg]Reg
	color     map[Reg]Reg
	cost      map[Reg]float64
	spillTemp map[Reg]bool // registers created by spilling: never spill again
}

// allocate assigns physical registers to every virtual register in f,
// spilling to stack slots where needed.
func allocate(f *MFunc) error {
	spillTemps := map[Reg]bool{}
	for round := 0; ; round++ {
		if round > 50 {
			return fmt.Errorf("amd64: register allocation of %s did not converge", f.Name)
		}
		liveOut := liveness(f)
		done := true
		var colorOf = map[Reg]Reg{}
		for _, class := range []RegClass{ClassGP, ClassXMM} {
			a := newAllocator(f, class, spillTemps)
			a.build(liveOut)
			a.run()
			if len(a.spilled) > 0 {
				a.rewrite(spillTemps)
				done = false
				continue
			}
			for r, c := range a.color {
				colorOf[r] = c
			}
		}
		if !done {
			continue
		}
		applyColors(f, colorOf)
		return nil
	}
}

func newAllocator(f *MFunc, class RegClass, spillTemps map[Reg]bool) *allocator {
	colors := allocatable[class]
	if class == ClassGP && f.frameFP {
		colors = colors[:len(colors)-1] // RBP is the frame pointer
	}
	a := &allocator{
		f: f, class: class, k: len(colors), colors: colors,
		precolored: map[Reg]bool{},
		simplifyWL: map[Reg]bool{}, freezeWL: map[Reg]bool{}, spillWL: map[Reg]bool{},
		coalesced: map[Reg]bool{}, colored: map[Reg]bool{}, onStack: map[Reg]bool{},
		coalescedMoves: map[*MInst]bool{}, constrainedMoves: map[*MInst]bool{}, frozenMoves: map[*MInst]bool{},
		worklistMoves: map[*MInst]bool{}, activeMoves: map[*MInst]bool{}, moveOrder: map[*MInst]int{},
		adjSet: map[edge]bool{}, adjList: map[Reg][]Reg{}, degree: map[Reg]int{},
		moveList: map[Reg][]*MInst{}, alias: map[Reg]Reg{}, color: map[Reg]Reg{}, cost: map[Reg]float64{},
		spillTemp: spillTemps,
	}
	for r := Reg(0); r < NumPhys; r++ {
		if r.PhysClass() == class {
			a.precolored[r] = true
			a.color[r] = r
			a.degree[r] = math.MaxInt32 / 2
		}
	}
	for r := Reg(FirstVirtual); r < f.nextVReg; r++ {
		if f.regClass[r] == class {
			a.initial = append(a.initial, r)
		}
	}
	return a
}

func (a *allocator) inClass(r Reg) bool { return a.f.classOf(r) == a.class && r != RSP }

func (a *allocator) addEdge(u, v Reg) {
	if u == v || a.adjSet[edge{u, v}] {
		return
	}
	a.adjSet[edge{u, v}] = true
	a.adjSet[edge{v, u}] = true
	if !a.precolored[u] {
		a.adjList[u] = append(a.adjList[u], v)
		a.degree[u]++
	}
	if !a.precolored[v] {
		a.adjList[v] = append(a.adjList[v], u)
		a.degree[v]++
	}
}

func (a *allocator) build(liveOut []regSet) {
	f := a.f
	n := f.numRegIndexes()
	live := newRegSet(n)
	var defs, uses []Reg
	for bi := len(f.Blocks) - 1; bi >= 0; bi-- {
		b := f.Blocks[bi]
		live.copyFrom(liveOut[bi])
		weight := math.Pow(10, float64(min(b.Depth, 6)))
		for ii := len(b.Insts) - 1; ii >= 0; ii-- {
			in := b.Insts[ii]
			defs = in.Defs(defs[:0])
			uses = in.Uses(uses[:0])
			for _, r := range defs {
				if a.inClass(r) && !r.IsPhys() {
					a.cost[r] += weight
				}
			}
			for _, r := range uses {
				if a.inClass(r) && !r.IsPhys() {
					a.cost[r] += weight
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
				if !a.inClass(d) {
					continue
				}
				live.forEach(func(i int) {
					l := f.indexReg(i)
					if a.inClass(l) {
						a.addEdge(l, d)
					}
				})
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

func (a *allocator) run() {
	a.makeWorklist()
	for len(a.simplifyWL) > 0 || len(a.worklistMoves) > 0 || len(a.freezeWL) > 0 || len(a.spillWL) > 0 {
		switch {
		case len(a.simplifyWL) > 0:
			a.simplify()
		case len(a.worklistMoves) > 0:
			a.coalesce()
		case len(a.freezeWL) > 0:
			a.freeze()
		default:
			a.selectSpill()
		}
	}
	a.assignColors()
}

func (a *allocator) makeWorklist() {
	for _, n := range a.initial {
		switch {
		case a.degree[n] >= a.k:
			a.spillWL[n] = true
		case a.moveRelated(n):
			a.freezeWL[n] = true
		default:
			a.simplifyWL[n] = true
		}
	}
}

func (a *allocator) adjacent(n Reg) []Reg {
	var out []Reg
	for _, m := range a.adjList[n] {
		if !a.onStack[m] && !a.coalesced[m] {
			out = append(out, m)
		}
	}
	return out
}

func (a *allocator) nodeMoves(n Reg) []*MInst {
	var out []*MInst
	for _, m := range a.moveList[n] {
		if a.activeMoves[m] || a.worklistMoves[m] {
			out = append(out, m)
		}
	}
	return out
}

func (a *allocator) moveRelated(n Reg) bool { return len(a.nodeMoves(n)) > 0 }

// pick returns some element of a set, choosing the smallest register for
// determinism.
func pick(set map[Reg]bool) Reg {
	best := NoReg
	for r := range set {
		if best == NoReg || r < best {
			best = r
		}
	}
	return best
}

func (a *allocator) simplify() {
	n := pick(a.simplifyWL)
	delete(a.simplifyWL, n)
	a.selectStack = append(a.selectStack, n)
	a.onStack[n] = true
	for _, m := range a.adjacent(n) {
		a.decrementDegree(m)
	}
}

func (a *allocator) decrementDegree(m Reg) {
	if a.precolored[m] {
		return
	}
	d := a.degree[m]
	a.degree[m] = d - 1
	if d == a.k {
		a.enableMoves(append(a.adjacent(m), m))
		delete(a.spillWL, m)
		if a.moveRelated(m) {
			a.freezeWL[m] = true
		} else {
			a.simplifyWL[m] = true
		}
	}
}

func (a *allocator) enableMoves(nodes []Reg) {
	for _, n := range nodes {
		for _, m := range a.nodeMoves(n) {
			if a.activeMoves[m] {
				delete(a.activeMoves, m)
				a.addWorklistMove(m)
			}
		}
	}
}

func (a *allocator) getAlias(n Reg) Reg {
	for a.coalesced[n] {
		n = a.alias[n]
	}
	return n
}

func (a *allocator) addWorkList(u Reg) {
	if !a.precolored[u] && !a.moveRelated(u) && a.degree[u] < a.k {
		delete(a.freezeWL, u)
		a.simplifyWL[u] = true
	}
}

// ok is George's test: t can merge into precolored r.
func (a *allocator) ok(t, r Reg) bool {
	return a.degree[t] < a.k || a.precolored[t] || a.adjSet[edge{t, r}]
}

// conservative is Briggs's test.
func (a *allocator) conservative(nodes []Reg) bool {
	k := 0
	seen := map[Reg]bool{}
	for _, n := range nodes {
		if seen[n] {
			continue
		}
		seen[n] = true
		if a.degree[n] >= a.k {
			k++
		}
	}
	return k < a.k
}

func (a *allocator) addWorklistMove(m *MInst) {
	a.worklistMoves[m] = true
	heap.Push(&a.pending, a.moveOrder[m])
}

// firstMove returns the worklist move that comes first in program order,
// which keeps allocation deterministic.
func (a *allocator) firstMove() *MInst {
	for a.pending.Len() > 0 {
		if m := a.moves[a.pending[0]]; a.worklistMoves[m] {
			return m
		}
		heap.Pop(&a.pending)
	}
	return nil
}

// intHeap is a min-heap of ints for container/heap.
type intHeap []int

func (h intHeap) Len() int           { return len(h) }
func (h intHeap) Less(i, j int) bool { return h[i] < h[j] }
func (h intHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *intHeap) Push(x any)        { *h = append(*h, x.(int)) }
func (h *intHeap) Pop() any {
	old := *h
	x := old[len(old)-1]
	*h = old[:len(old)-1]
	return x
}

func (a *allocator) coalesce() {
	m := a.firstMove()
	delete(a.worklistMoves, m)
	x := a.getAlias(m.Dst.Reg)
	y := a.getAlias(m.Src.Reg)
	u, v := x, y
	if a.precolored[y] {
		u, v = y, x
	}
	switch {
	case u == v:
		a.coalescedMoves[m] = true
		a.addWorkList(u)
	case a.precolored[v] || a.adjSet[edge{u, v}]:
		a.constrainedMoves[m] = true
		a.addWorkList(u)
		a.addWorkList(v)
	case a.precolored[u] && a.allOK(v, u), !a.precolored[u] && a.conservative(append(a.adjacent(u), a.adjacent(v)...)):
		a.coalescedMoves[m] = true
		a.combine(u, v)
		a.addWorkList(u)
	default:
		a.activeMoves[m] = true
	}
}

func (a *allocator) allOK(v, u Reg) bool {
	for _, t := range a.adjacent(v) {
		if !a.ok(t, u) {
			return false
		}
	}
	return true
}

func (a *allocator) combine(u, v Reg) {
	if a.freezeWL[v] {
		delete(a.freezeWL, v)
	} else {
		delete(a.spillWL, v)
	}
	a.coalesced[v] = true
	a.alias[v] = u
	a.moveList[u] = append(a.moveList[u], a.moveList[v]...)
	a.enableMoves([]Reg{v})
	for _, t := range a.adjacent(v) {
		a.addEdge(t, u)
		a.decrementDegree(t)
	}
	if a.degree[u] >= a.k && a.freezeWL[u] {
		delete(a.freezeWL, u)
		a.spillWL[u] = true
	}
	// A spill temporary merged with another node keeps its protection.
	if a.spillTemp[v] {
		a.spillTemp[u] = true
	}
}

func (a *allocator) freeze() {
	u := pick(a.freezeWL)
	delete(a.freezeWL, u)
	a.simplifyWL[u] = true
	a.freezeMoves(u)
}

func (a *allocator) freezeMoves(u Reg) {
	for _, m := range a.nodeMoves(u) {
		x, y := m.Dst.Reg, m.Src.Reg
		var v Reg
		if a.getAlias(y) == a.getAlias(u) {
			v = a.getAlias(x)
		} else {
			v = a.getAlias(y)
		}
		delete(a.activeMoves, m)
		a.frozenMoves[m] = true
		if !a.precolored[v] && len(a.nodeMoves(v)) == 0 && a.degree[v] < a.k {
			delete(a.freezeWL, v)
			a.simplifyWL[v] = true
		}
	}
}

func (a *allocator) selectSpill() {
	// Spill the node with the lowest cost per degree; never a temporary
	// created by an earlier spill (its live range cannot get shorter).
	best := NoReg
	bestScore := math.Inf(1)
	var cands []Reg
	for r := range a.spillWL {
		cands = append(cands, r)
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i] < cands[j] })
	for _, r := range cands {
		score := a.cost[r] / float64(max(a.degree[r], 1))
		if a.spillTemp[r] {
			score = math.Inf(1) / 2
		}
		if best == NoReg || score < bestScore {
			best, bestScore = r, score
		}
	}
	delete(a.spillWL, best)
	a.simplifyWL[best] = true
	a.freezeMoves(best)
}

func (a *allocator) assignColors() {
	for len(a.selectStack) > 0 {
		n := a.selectStack[len(a.selectStack)-1]
		a.selectStack = a.selectStack[:len(a.selectStack)-1]
		delete(a.onStack, n)
		used := map[Reg]bool{}
		for _, w := range a.adjList[n] {
			wa := a.getAlias(w)
			if a.colored[wa] || a.precolored[wa] {
				used[a.color[wa]] = true
			}
		}
		chosen := NoReg
		for _, c := range a.colors {
			if !used[c] {
				chosen = c
				break
			}
		}
		if chosen == NoReg {
			a.spilled = append(a.spilled, n)
			continue
		}
		a.colored[n] = true
		a.color[n] = chosen
	}
	for n := range a.coalesced {
		a.color[n] = a.color[a.getAlias(n)]
	}
}

// rewrite spills the spilled registers to stack slots, giving each use
// and definition a fresh short-lived register.
func (a *allocator) rewrite(spillTemps map[Reg]bool) {
	f := a.f
	slots := map[Reg]*ssa.Slot{}
	for _, r := range a.spilled {
		slots[r] = f.newSlot(fmt.Sprintf("spill%d", r-FirstVirtual), 8, 8)
	}
	var buf []Reg
	for _, b := range f.Blocks {
		var out []*MInst
		for _, in := range b.Insts {
			// Uses: reload before.
			buf = in.Uses(buf[:0])
			reload := map[Reg]Reg{}
			for _, r := range buf {
				slot, spilled := slots[r]
				if _, done := reload[r]; spilled && !done {
					t := f.newVReg(a.class)
					spillTemps[t] = true
					reload[r] = t
					out = append(out, &MInst{Op: MRELOAD, Size: 8, Dst: RegOp(t), Slot: slot})
				}
			}
			buf = in.Defs(buf[:0])
			store := map[Reg]Reg{}
			for _, r := range buf {
				if _, ok := slots[r]; ok {
					t, reused := reload[r]
					if !reused {
						t = f.newVReg(a.class)
						spillTemps[t] = true
					}
					store[r] = t
				}
			}
			in.replaceRegs(func(r Reg) Reg {
				if t, ok := store[r]; ok {
					return t
				}
				if t, ok := reload[r]; ok {
					return t
				}
				return r
			})
			out = append(out, in)
			for r, t := range store {
				out = append(out, &MInst{Op: MSPILL, Size: 8, Src: RegOp(t), Slot: slots[r]})
			}
		}
		b.Insts = out
	}
}

// applyColors replaces virtual registers by their colours and drops copies
// that became no-ops.
func applyColors(f *MFunc, color map[Reg]Reg) {
	used := map[Reg]bool{}
	for _, b := range f.Blocks {
		out := b.Insts[:0]
		for _, in := range b.Insts {
			in.replaceRegs(func(r Reg) Reg {
				if r.IsVirtual() {
					c, ok := color[r]
					if !ok {
						panic(fmt.Sprintf("amd64: %s: no colour for %s", f.Name, r))
					}
					r = c
				}
				return r
			})
			if in.isMove() && in.Dst.Reg == in.Src.Reg {
				continue
			}
			var defs []Reg
			for _, r := range in.Defs(defs) {
				// An interrupt handler preserves every register, including
				// those its callees may change.
				if f.interrupt || isCalleeSaved(r) && in.Op != MCALL {
					used[r] = true
				}
			}
			out = append(out, in)
		}
		b.Insts = out
	}
	f.usedSaved = nil
	f.savedXMM = nil
	if f.interrupt {
		for r := RAX; r <= X15; r++ {
			switch {
			case !used[r] || r == RSP:
			case r.PhysClass() == ClassXMM:
				f.savedXMM = append(f.savedXMM, r)
			default:
				f.usedSaved = append(f.usedSaved, r)
			}
		}
		return
	}
	for _, r := range calleeSaved {
		if used[r] {
			f.usedSaved = append(f.usedSaved, r)
		}
	}
}
