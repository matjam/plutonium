package opt

import (
	"os"
	"sort"

	"github.com/matjam/plutonium/internal/ssa"
)

// GenCheck inserts the generation checks of generational references
// (lower/gencheck.go, lib/pluto/alloc/gen.p, abi.md section 1.3) in
// functions marked with ssa.PragmaGenCheck. It runs after inlining and
// scalar replacement, so that the checks neither make functions look too
// big to inline nor keep frame slots from being split, and it skips
// accesses whose address is derived from a frame slot or a symbol.
//
// Each memory access through any other pointer p uses p's address without
// its tag, computed once where p is defined, and is preceded by a check of
// p somewhere before it:
//
//	t := p >> 48
//	if t != 0 {
//		if !rt.GenRegisteredLive(p) { panicStale(p) }
//	}
//	access p &^ tag
//
// A check of p stays valid until something that may free (GenFrees):
// a call to a function that may free, an indirect call, an intrinsic, a
// volatile access. Where checks go:
//
//   - Accesses through the same base pointer in one block share one check
//     (genPlacer.runs): the base is the address with its constant offsets
//     taken off, and the check, placed before the first of them, covers
//     the union of their byte extents. Only accesses with no call and no
//     barrier between them share a check, and only extents that together
//     form one interval, so the shared check reads exactly the bytes the
//     separate checks would have read, a little earlier: the range check
//     is defined byte by byte (abi.md section 1.3), so it fails exactly
//     when one of the separate checks would have, and nothing between the
//     first access and the last can free, end the task, or let it see
//     another task's free.
//   - An access needs no check of its own if every byte of its extent was
//     checked, through the same base, on every path to it since the last
//     such barrier (a forward dataflow over the whole function): a check
//     covered by earlier checks, of the same extent or of larger ones, is
//     removed.
//   - In a loop without barriers, the accesses through a p defined outside
//     the loop are checked once, before the loop, if the loop surely
//     reaches one of them on its first iteration once its header lets it
//     in (genPlacer.hoist): the check is guarded by the header's test as
//     the first iteration evaluates it, so a loop that runs no iteration
//     checks nothing, and a program never panics over a pointer it would
//     not have used. A loop leaving by a runtime panic before it reaches
//     the access may report the stale pointer instead; a loop that never
//     reaches it nor leaves (with no calls in it, so that it runs forever
//     doing nothing another task can see) panics where it would have hung.
//   - Memory the function holds (genPlacer.holds: a new allocation, or an
//     owned[T] parameter, not yet passed to a call or stored) can be freed
//     only by an arena reset or through synchronization with another task,
//     so its check survives barriers that may free other memory but reset
//     nothing (isReset), in loops as well.
//
// Multi-byte accesses check every covered granule and both extent bounds.
// Their checks use the actual derived address, including interior offsets.
// Functions marked ssa.PragmaGenMaskOnly (allocator internals and the named
// runtime metadata/bootstrap hooks) only remove tags.
func GenCheck(f *ssa.Func) {
	if f.Pragmas&(ssa.PragmaGenCheck|ssa.PragmaGenMaskOnly) == 0 {
		return
	}
	if f.Pragmas&ssa.PragmaGenMaskOnly != 0 || os.Getenv("PLUTO_GENCHECK_MASKONLY") != "" {
		genMaskOnly(f)
		return
	}
	g := &genPlacer{
		f:     f,
		exts:  map[genExtent]int{},
		parts: map[*ssa.Value]*genParts{},
		at:    map[*ssa.Value][]*genSite{},
		kind:  map[*ssa.Value]uint8{},
	}
	g.run()
	f.InvalidateCFG()
}

const genTagShift = 48

// genParts are a pointer's bits as an integer and its address without its
// tag, computed right after the pointer is defined.
type genParts struct {
	bits  *ssa.Value // Bitcast <i64> p
	plain *ssa.Value // the address without its tag, as an i64
	addr  *ssa.Value // the same, as a pointer
}

// A genSite is a place where a pointer may need its check: before the
// first of the accesses that share it, or at the end of a loop's preheader
// a check was hoisted to.
type genSite struct {
	v     *ssa.Value // the first access (for a hoisted check, the one it was hoisted from)
	arg   int        // 0 for a check before v; -1 for a hoisted check
	ext   int        // the extent checked: an index of genPlacer.extOf
	block *ssa.Block // v's block, or the preheader
	// guard, for a hoisted check, is the condition under which the check
	// runs (nil: always); mem is the memory state at the end of block.
	guard, mem *ssa.Value
	covered    bool // a check before the loop takes its place
	need       bool // a check is emitted here
}

// A genExtent is the bytes [root+off, root+off+size): root is a base
// pointer, an address with its constant offsets taken off (genOffsetRoot).
type genExtent struct {
	root      *ssa.Value
	off, size int64
}

// A genAccess is an address an access goes through, which emit replaces
// by the same address without its tag: the offsets and indexes steps
// apply to base (addrRoot), applied again to base without its tag, which
// is computed once where base is defined. Offsets keep a pointer's tag and
// never reach its bits (addresses use 47 bits), so the result is the same
// as the address's own without its tag, as genMaskOnly does too.
type genAccess struct {
	v     *ssa.Value
	arg   int
	base  *ssa.Value
	steps []*ssa.Value
}

type genPlacer struct {
	f      *ssa.Func
	dom    *ssa.DomTree
	ln     *ssa.LoopNest
	sites  []*genSite
	access []genAccess
	at     map[*ssa.Value][]*genSite // checks by the access they go before
	hoists map[*ssa.Block][]*genSite // hoisted sites by preheader
	exts   map[genExtent]int         // the extents checked, numbered
	extOf  []genExtent
	// over is, by extent, the extents through the same root that overlap
	// it, by offset: those whose checks may cover it (genPlacer.covered).
	over  [][]int
	parts map[*ssa.Value]*genParts
	// barrier and reset are, by block ID, whether the block holds a
	// barrier, and one that may reset (isReset); calls, whether it holds a
	// call of any kind.
	barrier, reset []bool
	calls          []bool
	// ranges are the checks hoistRange put before loops.
	ranges            []*genRange
	inlFree, inlReset []bool
	split             []*ssa.Block // preheaders made by splitting an edge
	cond              map[*ssa.Loop]*genCond

	// Roots the function holds (genPlacer.holds): a new allocation, from
	// where it is made (freshAt), or an owner parameter, from the entry
	// (owned); and where each stops being held (escapeAt).
	freshAt  map[*ssa.Value][]int
	owned    []int
	escapeAt map[*ssa.Value][]int
	// heldOut is, by block ID, the roots held at the end of the block.
	heldOut   [][]uint64
	escBlocks map[int][]*ssa.Block
	words     int
	// kind is, for each barrier, genFree or genReset.
	kind map[*ssa.Value]uint8
}

const (
	genFree  = 1 // a barrier: may free
	genReset = 2 // may reset, too: isReset
)

// genCond is a loop's entry condition, once computed.
type genCond struct {
	ok bool
	v  *ssa.Value // nil: the loop always runs its first iteration's body
}

func (g *genPlacer) run() {
	g.inlFree = genInlinedFrom(g.f, genFreeFuncs)
	g.inlReset = genInlinedFrom(g.f, genResetFuncs)
	g.splitPreheaders()
	g.dom = g.f.Dom()
	g.ln = g.f.Loops()
	g.collect()
	if len(g.sites) == 0 {
		g.unsplit()
		return
	}
	g.words = (len(g.extOf) + 63) / 64
	g.holds()
	g.hoists = map[*ssa.Block][]*genSite{}
	g.cond = map[*ssa.Loop]*genCond{}
	// Which roots are held at the end of each block does not depend on
	// the checks: hoisting asks it of preheaders.
	g.heldOut = g.dataflow(false)
	for _, l := range g.ln.Loops { // inner loops first
		g.hoist(l)
		g.hoistRange(l)
	}
	g.dataflow(true)
	g.emit()
	g.unsplit()
	// The tagged addresses the accesses used, and the entry conditions of
	// hoisting that went further out, are left without uses.
	ssa.DeadCode(g.f)
}

// isBarrier reports whether v may make a checked pointer stale.
func (g *genPlacer) isBarrier(v *ssa.Value) bool {
	if v.Op == ssa.OpCall {
		if v.Call != nil && v.Call.NoReturn {
			return false
		}
		return !g.f.GenNoFree[v.Aux.(*ssa.Sym)]
	}
	return genBarrierOp(v, g.inlFree, false)
}

// isReset reports whether v may make stale memory it is not given a
// pointer to: the barriers that end the checks of held roots too.
func (g *genPlacer) isReset(v *ssa.Value) bool {
	if v.Op == ssa.OpCall {
		if v.Call != nil && v.Call.NoReturn {
			return false
		}
		return !g.f.GenNoReset[v.Aux.(*ssa.Sym)]
	}
	return genBarrierOp(v, g.inlReset, true)
}

// splitPreheaders gives each loop entered by one edge, from a block with
// other successors, a block of its own on that edge, where checks hoisted
// out of the loop can go.
func (g *genPlacer) splitPreheaders() {
	ln := g.f.Loops()
	var edges [][2]*ssa.Block
	for _, l := range ln.Loops {
		h := l.Header
		var from *ssa.Block
		n := 0
		for _, e := range h.Preds {
			if !l.Contains(ln, e.B) {
				from = e.B
				n++
			}
		}
		if n == 1 && len(from.Succs) > 1 {
			edges = append(edges, [2]*ssa.Block{from, h})
		}
	}
	for _, e := range edges {
		from, h := e[0], e[1]
		for i, s := range from.Succs {
			if s.B == h {
				g.split = append(g.split, splitEdge(g.f, from, i))
				break
			}
		}
	}
}

// splitEdge puts a new empty block on edge i of b.
func splitEdge(f *ssa.Func, b *ssa.Block, i int) *ssa.Block {
	e := b.Succs[i]
	n := f.NewBlock(ssa.BlockPlain)
	n.CopyPos(e.B)
	b.Succs[i] = ssa.Edge{B: n, I: 0}
	n.Preds = []ssa.Edge{{B: b, I: i}}
	n.Succs = []ssa.Edge{{B: e.B, I: e.I}}
	e.B.Preds[e.I] = ssa.Edge{B: n, I: 0}
	return n
}

// unsplit removes the blocks splitPreheaders made that nothing went into.
func (g *genPlacer) unsplit() {
	if len(g.split) == 0 {
		return
	}
	gone := map[*ssa.Block]bool{}
	for _, n := range g.split {
		if len(n.Values) != 0 || n.Kind != ssa.BlockPlain || len(n.Preds) != 1 || len(n.Succs) != 1 {
			continue
		}
		p, s := n.Preds[0], n.Succs[0]
		p.B.Succs[p.I] = ssa.Edge{B: s.B, I: s.I}
		s.B.Preds[s.I] = ssa.Edge{B: p.B, I: p.I}
		gone[n] = true
	}
	if len(gone) == 0 {
		return
	}
	keep := g.f.Blocks[:0]
	for _, b := range g.f.Blocks {
		if !gone[b] {
			keep = append(keep, b)
		}
	}
	clear(g.f.Blocks[len(keep):])
	g.f.Blocks = keep
	g.f.InvalidateCFG()
}

// A genMember is an access that may share a check with others through the
// same root in its block (genPlacer.runs).
type genMember struct {
	v         *ssa.Value
	idx       int // v's place in its block
	off, size int64
}

// genGroup names the accesses through one root in one stretch of a block
// with no call or barrier.
type genGroup struct {
	root *ssa.Value
	seg  int
}

// genMergeLimit bounds the extents through one root that coverage looks
// through; past it, only an equal extent covers another.
const genMergeLimit = 64

// collect finds the accesses that need a check, the checks they share, and
// the blocks that hold barriers.
func (g *genPlacer) collect() {
	g.barrier = make([]bool, g.f.NumBlocks())
	g.reset = make([]bool, g.f.NumBlocks())
	g.calls = make([]bool, g.f.NumBlocks())
	for _, b := range g.f.Blocks {
		var order []genGroup
		groups := map[genGroup][]genMember{}
		seg := 0
		for idx, v := range b.Values {
			// A check may cover the accesses after it only up to the next
			// call or barrier: nothing that can free memory, end the task
			// or the program, or synchronize with another task comes between
			// a shared check and the accesses it covers.
			bound := v.Op == ssa.OpCall || v.Op == ssa.OpCallInd
			if bound {
				g.calls[b.ID] = true
			}
			if g.isReset(v) {
				g.reset[b.ID] = true
				g.barrier[b.ID] = true
				g.kind[v] = genReset
				bound = true
			} else if g.isBarrier(v) {
				g.barrier[b.ID] = true
				g.kind[v] = genFree
				bound = true
			}
			if bound {
				seg++
			}
			for _, ai := range genAddrArgs(v) {
				base, steps := addrRoot(v.Args[ai])
				if isStaticAddr(base) || isUntagPattern(base) {
					continue
				}
				size := genAccessSize(v)
				if size == 0 {
					continue
				}
				root, off := genOffsetRoot(v.Args[ai])
				g.access = append(g.access, genAccess{v: v, arg: ai, base: base, steps: steps})
				k := genGroup{root, seg}
				if groups[k] == nil {
					order = append(order, k)
				}
				groups[k] = append(groups[k], genMember{v: v, idx: idx, off: off, size: size})
			}
			if bound {
				seg++
			}
		}
		for _, k := range order {
			g.runs(b, k.root, groups[k])
		}
	}
	g.overlaps()
}

// genOffsetRoot returns the pointer p is p plus constant offsets of, and
// the sum of the offsets.
func genOffsetRoot(p *ssa.Value) (*ssa.Value, int64) {
	off := int64(0)
	for p.Op == ssa.OpOffPtr {
		off += p.AuxInt
		p = p.Args[0]
	}
	return p, off
}

// runs gives the accesses ms, through root in one stretch of block b with
// no call or barrier, their checks: one for each run of extents that
// together form one interval, before the first access of the run. Extents
// with a gap between them are checked separately, since a check of the gap
// could fail where the accesses would not.
func (g *genPlacer) runs(b *ssa.Block, root *ssa.Value, ms []genMember) {
	sorted := append([]genMember(nil), ms...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].off < sorted[j].off })
	for i := 0; i < len(sorted); {
		first := sorted[i]
		lo, hi := first.off, first.off+first.size
		j := i + 1
		for ; j < len(sorted) && sorted[j].off <= hi; j++ {
			if sorted[j].idx < first.idx {
				first = sorted[j]
			}
			hi = max(hi, sorted[j].off+sorted[j].size)
		}
		s := &genSite{v: first.v, ext: g.extent(genExtent{root, lo, hi - lo}), block: b}
		g.sites = append(g.sites, s)
		g.at[first.v] = append(g.at[first.v], s)
		i = j
	}
}

// extent returns x's number, giving it one the first time.
func (g *genPlacer) extent(x genExtent) int {
	if i, ok := g.exts[x]; ok {
		return i
	}
	g.exts[x] = len(g.extOf)
	g.extOf = append(g.extOf, x)
	return len(g.extOf) - 1
}

// overlaps finds, for each extent, the extents through the same root that
// overlap it.
func (g *genPlacer) overlaps() {
	g.over = make([][]int, len(g.extOf))
	byRoot := map[*ssa.Value][]int{}
	for i, x := range g.extOf {
		byRoot[x.root] = append(byRoot[x.root], i)
	}
	for _, ids := range byRoot {
		if len(ids) > genMergeLimit {
			for _, i := range ids {
				g.over[i] = []int{i}
			}
			continue
		}
		sort.SliceStable(ids, func(i, j int) bool { return g.extOf[ids[i]].off < g.extOf[ids[j]].off })
		for _, i := range ids {
			x := g.extOf[i]
			for _, j := range ids {
				if y := g.extOf[j]; y.off < x.off+x.size && x.off < y.off+y.size {
					g.over[i] = append(g.over[i], j)
				}
			}
		}
	}
}

// covered reports whether every byte of extent e is in an extent avail
// says has been checked: the separate checks then say what a check of e
// would (a range check is defined byte by byte, abi.md section 1.3).
func (g *genPlacer) covered(e int, avail []uint64) bool {
	x := g.extOf[e]
	pos, end := x.off, x.off+x.size
	for _, j := range g.over[e] {
		if avail[j/64]&(1<<(j%64)) == 0 {
			continue
		}
		y := g.extOf[j]
		if y.off > pos {
			return false // over is by offset: none later starts at pos
		}
		pos = max(pos, y.off+y.size)
		if pos >= end {
			return true
		}
	}
	return false
}

// hoist moves the checks of pointers defined outside loop l to l's
// preheader: of every such pointer if l has no barriers, and of those the
// function holds throughout l if none of its barriers may reset.
func (g *genPlacer) hoist(l *ssa.Loop) {
	barriers := false
	for _, b := range l.Blocks {
		if g.reset[b.ID] {
			return
		}
		barriers = barriers || g.barrier[b.ID]
	}
	h := l.Header
	pre, pi := g.preheader(l)
	if pre == nil {
		return
	}
	// The sites in l, by extent of an invariant root, in the order first
	// seen.
	var order []int
	byRoot := map[int][]*genSite{}
	for _, s := range g.sites {
		root := g.extOf[s.ext].root
		if s.covered || !l.Contains(g.ln, s.block) || l.Contains(g.ln, root.Block) ||
			!g.dom.Dominates(root.Block, pre) || barriers && !g.heldIn(l, pre, s) {
			continue
		}
		if byRoot[s.ext] == nil {
			order = append(order, s.ext)
		}
		byRoot[s.ext] = append(byRoot[s.ext], s)
	}
	for _, r := range order {
		sites := byRoot[r]
		// An unguarded site in the header, before its test, runs whenever
		// the loop is entered: one check before the loop covers all.
		var from *genSite
		for _, s := range sites {
			if s.block == h && s.guard == nil {
				from = s
				break
			}
		}
		if from != nil {
			for _, s := range sites {
				s.covered = true
			}
			g.addHoist(pre, from, nil)
			continue
		}
		// Otherwise an unguarded site the first iteration surely reaches
		// lets a check run before the loop when the header lets that
		// iteration in, which covers every site past the header.
		for _, s := range sites {
			if s.guard == nil && g.reaches(l, s.block) {
				from = s
				break
			}
		}
		if from != nil {
			if c := g.entryCond(l, pre, pi); c.ok {
				for _, s := range sites {
					if s.block != h {
						s.covered = true
					}
				}
				g.addHoist(pre, from, c.v)
				continue
			}
		}
		// A check hoisted out of an inner loop, guarded by a condition
		// that does not change in l, moves out of l on its own.
		for _, s := range sites {
			if s.covered || s.guard == nil || !g.reaches(l, s.block) || !g.invariant(l, s.guard) {
				continue
			}
			var entry *ssa.Value
			if s.block != h {
				c := g.entryCond(l, pre, pi)
				if !c.ok {
					continue
				}
				entry = c.v
			}
			guard := g.cloneInvariant(l, pre, s.guard)
			if entry != nil {
				guard = g.newValue(pre, s.v, ssa.OpAnd, ssa.TBool, guard, entry)
			}
			s.covered = true
			g.addHoist(pre, s, guard)
		}
	}
}

// preheader returns the block loop l is entered from, the only one, which
// leads only to l, and the index of its edge among the header's
// predecessors; nil if there is none.
func (g *genPlacer) preheader(l *ssa.Loop) (*ssa.Block, int) {
	var pre *ssa.Block
	pi := -1
	for i, e := range l.Header.Preds {
		if l.Contains(g.ln, e.B) {
			continue
		}
		if pre != nil {
			return nil, -1 // entered from more than one place
		}
		pre, pi = e.B, i
	}
	if pre == nil || len(pre.Succs) != 1 || !g.dom.Reachable(pre) {
		return nil, -1
	}
	return pre, pi
}

// addHoist adds a check of from's root at the end of pre.
func (g *genPlacer) addHoist(pre *ssa.Block, from *genSite, guard *ssa.Value) {
	s := &genSite{v: from.v, arg: -1, ext: from.ext, block: pre, guard: guard, mem: g.memOut(pre)}
	g.sites = append(g.sites, s)
	g.hoists[pre] = append(g.hoists[pre], s)
}

// reaches reports whether, once loop l has let its first iteration in
// past its header's test, it surely reaches block a before it leaves,
// other than by a runtime panic.
func (g *genPlacer) reaches(l *ssa.Loop, a *ssa.Block) bool {
	h := l.Header
	for _, e := range h.Preds {
		if l.Contains(g.ln, e.B) && !g.dom.Dominates(a, e.B) {
			return false // an iteration may go round without it
		}
	}
	for _, b := range l.Blocks {
		for _, e := range b.Succs {
			if l.Contains(g.ln, e.B) || b == h || g.dom.Dominates(a, b) || genIsPanicBlock(e.B) {
				continue
			}
			return false
		}
	}
	return true
}

// entryCond returns the condition, evaluated at the end of pre, under
// which loop l's header lets its first iteration into the loop: its test
// with each of its phis replaced by the value it has on entry.
func (g *genPlacer) entryCond(l *ssa.Loop, pre *ssa.Block, pi int) *genCond {
	if c := g.cond[l]; c != nil {
		return c
	}
	c := &genCond{}
	g.cond[l] = c
	h := l.Header
	if h.Kind != ssa.BlockIf {
		c.ok = h.Kind == ssa.BlockPlain
		return c
	}
	in0 := l.Contains(g.ln, h.Succs[0].B)
	in1 := l.Contains(g.ln, h.Succs[1].B)
	if in0 && in1 {
		c.ok = true
		return c
	}
	budget := 32
	if !g.cloneable(l, h.Controls[0], true, &budget) {
		return c
	}
	memo := map[*ssa.Value]*ssa.Value{}
	v := g.cloneEntry(l, pre, pi, h.Controls[0], memo)
	if !in0 {
		v = g.newValue(pre, h.Controls[0], ssa.OpNot, ssa.TBool, v)
	}
	c.ok, c.v = true, v
	return c
}

// genPure reports whether op computes its result from its arguments only,
// and cannot trap.
func genPure(op ssa.Op) bool {
	switch op {
	case ssa.OpConst, ssa.OpConstFloat, ssa.OpAdd, ssa.OpSub, ssa.OpMul,
		ssa.OpAnd, ssa.OpOr, ssa.OpXor, ssa.OpAndNot, ssa.OpShl, ssa.OpShrL,
		ssa.OpShrA, ssa.OpNeg, ssa.OpCom, ssa.OpEq, ssa.OpNe, ssa.OpLess,
		ssa.OpLeq, ssa.OpLessu, ssa.OpLequ, ssa.OpFEq, ssa.OpFNe, ssa.OpFLess,
		ssa.OpFLeq, ssa.OpNot, ssa.OpSelect, ssa.OpSignExt, ssa.OpZeroExt,
		ssa.OpTrunc, ssa.OpBitcast, ssa.OpOffPtr, ssa.OpAddPtr, ssa.OpCopy:
		return true
	}
	return false
}

// cloneable reports whether v can be computed before loop l: it is
// defined outside l, or is a pure function of such values and, with
// entry set, of the phis of l's header.
func (g *genPlacer) cloneable(l *ssa.Loop, v *ssa.Value, entry bool, budget *int) bool {
	if !l.Contains(g.ln, v.Block) {
		return true
	}
	*budget--
	if *budget < 0 {
		return false
	}
	if v.Op == ssa.OpPhi {
		return entry && v.Block == l.Header
	}
	if !genPure(v.Op) {
		return false
	}
	for _, a := range v.Args {
		if !g.cloneable(l, a, entry, budget) {
			return false
		}
	}
	return true
}

// invariant reports whether v can be computed before loop l without the
// header's phis.
func (g *genPlacer) invariant(l *ssa.Loop, v *ssa.Value) bool {
	budget := 32
	return g.cloneable(l, v, false, &budget)
}

// cloneEntry computes v at the end of pre for the first iteration of l.
func (g *genPlacer) cloneEntry(l *ssa.Loop, pre *ssa.Block, pi int, v *ssa.Value, memo map[*ssa.Value]*ssa.Value) *ssa.Value {
	if !l.Contains(g.ln, v.Block) {
		return v
	}
	if w := memo[v]; w != nil {
		return w
	}
	var w *ssa.Value
	if v.Op == ssa.OpPhi {
		w = v.Args[pi]
	} else {
		args := make([]*ssa.Value, len(v.Args))
		for i, a := range v.Args {
			args[i] = g.cloneEntry(l, pre, pi, a, memo)
		}
		w = g.newValue(pre, v, v.Op, v.Type, args...)
		w.AuxInt, w.Aux = v.AuxInt, v.Aux
	}
	memo[v] = w
	return w
}

// cloneInvariant computes v, invariant in l, at the end of pre.
func (g *genPlacer) cloneInvariant(l *ssa.Loop, pre *ssa.Block, v *ssa.Value) *ssa.Value {
	return g.cloneEntry(l, pre, -1, v, map[*ssa.Value]*ssa.Value{})
}

// newValue appends a value to b, at pos's position.
func (g *genPlacer) newValue(b *ssa.Block, pos *ssa.Value, op ssa.Op, t *ssa.Type, args ...*ssa.Value) *ssa.Value {
	v := b.NewValue(op, t, args...)
	v.CopyPos(pos)
	return v
}

// memOut returns the memory state at the end of b.
func (g *genPlacer) memOut(b *ssa.Block) *ssa.Value {
	for ; b != nil; b = g.dom.Idom(b) {
		for i := len(b.Values) - 1; i >= 0; i-- {
			if v := b.Values[i]; v.Type.IsMem() {
				return v
			}
		}
	}
	panic("opt: no memory state in the entry block")
}

// heldIn reports whether the function holds root r from the end of pre
// throughout loop l.
func (g *genPlacer) heldIn(l *ssa.Loop, pre *ssa.Block, s *genSite) bool {
	i := s.ext
	out := g.heldOut[pre.ID]
	if out == nil || out[g.words+i/64]&(1<<(i%64)) == 0 {
		return false
	}
	for _, b := range g.escBlocks[i] {
		if l.Contains(g.ln, b) {
			return false
		}
	}
	return true
}

// dataflow computes, forward over the function, the roots checked since
// the last barrier that ended their check (avail) and the roots the
// function holds (held), and returns both as they are at the end of each
// block, by block ID: avail in the first g.words words, held after. With
// decide set, it marks the sites that need a check: those whose root is
// not available on every path to them.
func (g *genPlacer) dataflow(decide bool) [][]uint64 {
	f := g.f
	n := 2 * g.words
	order := f.ReversePostOrder()
	out := make([][]uint64, f.NumBlocks())
	cur := make([]uint64, n)
	// A function too big for the dataflow starts each block with nothing
	// checked or held.
	if len(order)*n > 1<<21 {
		for _, b := range order {
			clear(cur)
			if b == f.Entry {
				g.entryState(cur)
			}
			g.transfer(b, cur, decide)
			out[b.ID] = append([]uint64(nil), cur...)
		}
		return out
	}
	in := make([][]uint64, f.NumBlocks())
	for _, b := range order {
		in[b.ID] = make([]uint64, n)
		out[b.ID] = make([]uint64, n)
		if b != f.Entry {
			for i := range out[b.ID] {
				out[b.ID][i] = ^uint64(0)
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for _, b := range order {
			g.meet(b, in[b.ID], out)
			copy(cur, in[b.ID])
			g.transfer(b, cur, false)
			if !equalBits(cur, out[b.ID]) {
				copy(out[b.ID], cur)
				changed = true
			}
		}
	}
	if decide {
		for _, b := range order {
			copy(cur, in[b.ID])
			g.transfer(b, cur, true)
		}
	}
	return out
}

// entryState sets what is checked and held on entry: the owner
// parameters are held.
func (g *genPlacer) entryState(cur []uint64) {
	clear(cur)
	for _, i := range g.owned {
		cur[g.words+i/64] |= 1 << (i % 64)
	}
}

// meet sets in to the intersection of the states the predecessors of b
// end with.
func (g *genPlacer) meet(b *ssa.Block, in []uint64, out [][]uint64) {
	if b == g.f.Entry {
		g.entryState(in)
		return
	}
	first := true
	for _, e := range b.Preds {
		o := out[e.B.ID]
		if o == nil {
			continue // unreachable
		}
		if first {
			copy(in, o)
			first = false
			continue
		}
		for i := range in {
			in[i] &= o[i]
		}
	}
	if first {
		clear(in)
	}
}

// transfer applies b's checks, escapes, barriers and allocations to cur;
// with decide set, it marks the sites that need a check.
func (g *genPlacer) transfer(b *ssa.Block, cur []uint64, decide bool) {
	avail, held := cur[:g.words], cur[g.words:]
	site := func(s *genSite) {
		if s.covered {
			return
		}
		i := s.ext
		have := g.covered(i, avail)
		if decide {
			s.need = !have
		}
		// Checked here or covered before, every byte of the extent is
		// checked from here on.
		if s.guard == nil {
			avail[i/64] |= 1 << (i % 64)
		}
	}
	for _, v := range b.Values {
		for _, s := range g.at[v] {
			site(s)
		}
		for _, i := range g.escapeAt[v] {
			held[i/64] &^= 1 << (i % 64)
		}
		switch g.kind[v] {
		case genReset:
			clear(avail)
		case genFree:
			for k := range avail {
				avail[k] &= held[k]
			}
		}
		for _, i := range g.freshAt[v] {
			// Freshness proves held lifetime, not the bounds of an
			// arbitrary access. The first extent still needs its check.
			held[i/64] |= 1 << (i % 64)
		}
	}
	for _, s := range g.hoists[b] {
		site(s)
	}
}

// holds finds the roots the function holds: new allocations (the result
// of an Allocator's Alloc, or of genFreshFuncs) and owner parameters, up
// to where a pointer to them, or into them, escapes (genPlacer.escapes).
// Nothing can free memory the function holds without being given a
// pointer to it, except by resetting an arena, so a check of such a root
// stays valid across barriers that do not reset (isReset).
func (g *genPlacer) holds() {
	g.freshAt = map[*ssa.Value][]int{}
	g.escapeAt = map[*ssa.Value][]int{}
	g.escBlocks = map[int][]*ssa.Block{}
	owner := map[int64]bool{}
	for _, a := range g.f.GenOwnerArgs {
		owner[a] = true
	}
	var cands []int
	for i, x := range g.extOf {
		r := x.root
		if (r.Op == ssa.OpSelectN && r.AuxInt == 1 && genFreshCall(r.Args[0]) || r.Op == ssa.OpArg && owner[r.AuxInt]) &&
			genHeldExtent(x) {
			cands = append(cands, i)
		}
	}
	if len(cands) == 0 {
		return
	}
	users := map[*ssa.Value][]*ssa.Value{}
	ctl := map[*ssa.Value][]*ssa.Block{}
	for _, b := range g.f.Blocks {
		for _, v := range b.Values {
			for _, a := range v.Args {
				users[a] = append(users[a], v)
			}
		}
		for _, c := range b.Controls {
			ctl[c] = append(ctl[c], b)
		}
	}
	type escaped struct {
		esc []*ssa.Value
		ok  bool
	}
	memo := map[*ssa.Value]escaped{}
	for _, i := range cands {
		r := g.extOf[i].root
		e, seen := memo[r]
		if !seen {
			e.esc, e.ok = escapes(r, users, ctl)
			memo[r] = e
		}
		esc, ok := e.esc, e.ok
		if !ok {
			continue
		}
		if r.Op == ssa.OpArg {
			g.owned = append(g.owned, i)
		} else {
			g.freshAt[r] = append(g.freshAt[r], i)
		}
		for _, u := range esc {
			g.escapeAt[u] = append(g.escapeAt[u], i)
			g.escBlocks[i] = append(g.escBlocks[i], u.Block)
		}
	}
}

// genHeldExtent reports whether extent x, through a pointer to an
// allocation the function holds, is memory of that allocation, which only
// a reset can free while it is held: x starts at the pointer, or lies in
// its first granule, which belongs to that allocation alone (allocations
// start on a granule, abi.md section 1.3). Field accesses past the first
// granule of a new allocation are checked as accesses through any pointer.
func genHeldExtent(x genExtent) bool {
	return x.off == 0 || x.off > 0 && x.off+x.size <= 16
}

// escapes returns the values at which root r, or a pointer derived from
// it, leaves the function's hands: a call it is passed to, a store of it
// into memory. ok is false if r is used in a way not followed here (a
// phi, a conversion to an integer), and is then never held.
func escapes(r *ssa.Value, users map[*ssa.Value][]*ssa.Value, ctl map[*ssa.Value][]*ssa.Block) (esc []*ssa.Value, ok bool) {
	seen := map[*ssa.Value]bool{r: true}
	work := []*ssa.Value{r}
	for len(work) > 0 {
		w := work[len(work)-1]
		work = work[:len(work)-1]
		for _, b := range ctl[w] {
			if b.Kind != ssa.BlockRet {
				return nil, false
			}
		}
		for _, u := range users[w] {
			switch u.Op {
			case ssa.OpLoad, ssa.OpZero, ssa.OpFill:
				for _, a := range u.Args[1:] {
					if a == w {
						return nil, false
					}
				}
			case ssa.OpStore:
				if u.Args[1] == w {
					esc = append(esc, u)
				}
			case ssa.OpMove, ssa.OpEq, ssa.OpNe:
			case ssa.OpOffPtr, ssa.OpAddPtr:
				if u.Args[0] != w {
					return nil, false
				}
				if !seen[u] {
					seen[u] = true
					work = append(work, u)
				}
			case ssa.OpCall, ssa.OpCallInd:
				esc = append(esc, u)
			default:
				return nil, false
			}
		}
	}
	return esc, true
}

func equalBits(a, b []uint64) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// emit inserts the checks decided on and gives every access the address
// without its tag.
func (g *genPlacer) emit() {
	for _, x := range g.extOf {
		g.partsOf(x.root)
	}
	// Hoisted checks first: an access's check splits the block before
	// it, which leaves the end of the block where it was.
	for _, s := range g.sites {
		if s.arg < 0 && s.need {
			g.check(s.block, nil, s)
		}
	}
	for _, r := range g.ranges {
		g.checkRange(r)
	}
	for _, s := range g.sites {
		if s.arg >= 0 && s.need {
			g.check(s.v.Block, s.v, s)
		}
	}
	for _, a := range g.access {
		a.v.SetArg(a.arg, g.rebuild(a.v.Block, a.v, g.partsOf(a.base).addr, a.steps))
	}
}

// genAddrArgs returns the arguments of v that are addresses it accesses
// through, which may need a check: none for a load from static data.
func genAddrArgs(v *ssa.Value) []int {
	switch v.Op {
	case ssa.OpLoad:
		if v.AuxInt&ssa.MemStatic != 0 {
			return nil
		}
		return genArg0
	case ssa.OpStore, ssa.OpZero, ssa.OpFill:
		return genArg0
	case ssa.OpMove:
		return genArgs01
	}
	return nil
}

func genAccessSize(v *ssa.Value) int64 {
	switch v.Op {
	case ssa.OpLoad:
		return v.Type.Size
	case ssa.OpStore:
		return v.Args[1].Type.Size
	case ssa.OpMove, ssa.OpZero, ssa.OpFill:
		return v.AuxInt
	}
	return 1
}

var (
	genArg0   = []int{0}
	genArgs01 = []int{0, 1}
)

// addrRoot follows offsets and indexing back to the pointer an address is
// derived from.
func addrRoot(p *ssa.Value) (*ssa.Value, []*ssa.Value) {
	var steps []*ssa.Value
	for p.Op == ssa.OpOffPtr || p.Op == ssa.OpAddPtr {
		steps = append(steps, p)
		p = p.Args[0]
	}
	return p, steps
}

// isUntagPattern reports whether p is a pointer lowering already took the
// tag off: Bitcast(ShrL(Shl(x, 16), 16)).
func isUntagPattern(p *ssa.Value) bool {
	if p.Op != ssa.OpBitcast {
		return false
	}
	s := p.Args[0]
	return s.Op == ssa.OpShrL && constEq(s.Args[1], 64-genTagShift) &&
		s.Args[0].Op == ssa.OpShl && constEq(s.Args[0].Args[1], 64-genTagShift)
}

// partsOf returns root's genParts, emitting them right after root's
// definition the first time.
func (g *genPlacer) partsOf(root *ssa.Value) *genParts {
	if p := g.parts[root]; p != nil {
		return p
	}
	p := genPartsAfter(g.f, root)
	g.parts[root] = p
	return p
}

// genPartsAfter emits root's parts right after its definition.
func genPartsAfter(f *ssa.Func, root *ssa.Value) *genParts {
	b := root.Block
	at := valueAfter(root)
	emit := func(op ssa.Op, t *ssa.Type, args ...*ssa.Value) *ssa.Value {
		var v *ssa.Value
		if at == nil {
			v = b.NewValue(op, t, args...)
		} else {
			v = b.NewValueBefore(at, op, t, args...)
		}
		v.CopyPos(root)
		return v
	}
	shift := f.ConstInt(ssa.TI64, 64-genTagShift)
	p := &genParts{}
	p.bits = emit(ssa.OpBitcast, ssa.TI64, root)
	shl := emit(ssa.OpShl, ssa.TI64, p.bits, shift)
	p.plain = emit(ssa.OpShrL, ssa.TI64, shl, shift)
	p.addr = emit(ssa.OpBitcast, ssa.TPtr, p.plain)
	return p
}

// valueAfter returns the value new values go before so that they follow v
// in its block (past any phis), or nil to append them.
func valueAfter(v *ssa.Value) *ssa.Value {
	b := v.Block
	i := 0
	for b.Values[i] != v {
		i++
	}
	i++
	for i < len(b.Values) && b.Values[i].Op == ssa.OpPhi {
		i++
	}
	if i < len(b.Values) {
		return b.Values[i]
	}
	return nil
}

// rebuild reapplies the offsets of steps to m, before v.
func (g *genPlacer) rebuild(b *ssa.Block, v, m *ssa.Value, steps []*ssa.Value) *ssa.Value {
	for i := len(steps) - 1; i >= 0; i-- {
		st := steps[i]
		if st.Op == ssa.OpOffPtr {
			m = b.NewValueBefore(v, ssa.OpOffPtr, ssa.TPtr, m)
			m.AuxInt = st.AuxInt
		} else {
			m = b.NewValueBefore(v, ssa.OpAddPtr, ssa.TPtr, m, st.Args[1])
		}
		m.CopyPos(v)
	}
	return m
}

// check splits b before at (at its end if at is nil) to check s's extent.
func (g *genPlacer) check(b *ssa.Block, at *ssa.Value, s *genSite) {
	x := g.extOf[s.ext]
	mem := s.mem
	if at != nil {
		mem = at.MemArg()
	}
	cont := g.splitAt(b, at)
	// The tagged address of the extent's first byte: an offset keeps the
	// tag of the pointer it is applied to.
	p := x.root
	if x.off != 0 {
		p = g.newValue(b, s.v, ssa.OpOffPtr, ssa.TPtr, p)
		p.AuxInt = x.off
	}
	var n *ssa.Value
	if x.size > 1 {
		n = g.f.ConstInt(ssa.TI64, x.size)
	}
	g.lookup(b, cont, s.v, mem, s.guard, g.parts[x.root].bits, p, n)
}

// splitAt splits b before at (at its end if at is nil): the block it
// returns takes the rest of b and its ending, and b is left without an
// ending, for a check (genPlacer.lookup) to end.
func (g *genPlacer) splitAt(b *ssa.Block, at *ssa.Value) *ssa.Block {
	f := g.f

	// Split b before at: cont takes the rest of b and its ending.
	cont := f.NewBlock(b.Kind)
	idx := len(b.Values)
	if at != nil {
		idx = 0
		for b.Values[idx] != at {
			idx++
		}
	}
	rest := b.Values[idx:]
	b.Values = b.Values[:idx:idx]
	for _, w := range rest {
		w.Block = cont
	}
	cont.Values = append(cont.Values, rest...)
	cont.Likely = b.Likely
	cont.CopyPos(b)
	cont.SetControl(b.Controls...)
	b.SetControl()
	for len(b.Succs) > 0 {
		e := b.Succs[0]
		cont.Succs = append(cont.Succs, ssa.Edge{B: e.B, I: e.I})
		e.B.Preds[e.I] = ssa.Edge{B: cont, I: len(cont.Succs) - 1}
		b.Succs = b.Succs[1:]
	}
	return cont
}

// lookup ends b, which splitAt left without an ending, with the check of
// the n bytes at p (one byte if n is nil), whose pointer has bits as an
// integer, when guard holds (nil: always); the check continues to cont.
// pos gives the positions, and mem is the memory state the lookup reads.
func (g *genPlacer) lookup(b, cont *ssa.Block, pos, mem, guard, bits, p, n *ssa.Value) {
	f := g.f
	i64 := func(c int64) *ssa.Value { return f.ConstInt(ssa.TI64, c) }
	tag := g.newValue(b, pos, ssa.OpShrL, ssa.TI64, bits, i64(genTagShift))
	tagged := g.newValue(b, pos, ssa.OpNe, ssa.TBool, tag, i64(0))
	if guard != nil {
		tagged = g.newValue(b, pos, ssa.OpAnd, ssa.TBool, guard, tagged)
	}
	chk := f.NewBlock(ssa.BlockIf)
	fail := f.NewBlock(ssa.BlockExit)
	chk.Pos, chk.Inl = pos.Pos, pos.Inl
	fail.Pos, fail.Inl = pos.Pos, pos.Inl
	b.Kind = ssa.BlockIf
	b.Likely = ssa.Unknown
	b.SetControl(tagged)
	b.AddEdgeTo(chk)
	b.AddEdgeTo(cont)

	// All tagged addresses use the directory. A lookup only reads stable
	// metadata; the original memory state remains valid for the payload
	// access, whose block is reached only after the lookup succeeds.
	args, sym := []*ssa.Value{p}, f.GenLive
	if n != nil {
		args, sym = append(args, n), f.GenRange
	}
	args = append(args, mem)
	live := g.newValue(chk, pos, ssa.OpCall, ssa.TupleOf(ssa.TMem, ssa.TBool), args...)
	live.Aux = sym
	live.Call = &ssa.CallInfo{ABI: ssa.ABIInternal, Results: []*ssa.Type{ssa.TBool}, Implicit: true}
	ok := g.newValue(chk, pos, ssa.OpSelectN, ssa.TBool, live)
	ok.AuxInt = 1
	chk.SetControl(ok)
	chk.Likely = ssa.Likely
	chk.AddEdgeTo(cont)
	chk.AddEdgeTo(fail)

	panicArgs, panicSym := []*ssa.Value{p}, f.GenPanic
	if n != nil {
		panicArgs, panicSym = append(panicArgs, n), f.GenRangePanic
	}
	panicArgs = append(panicArgs, mem)
	call := g.newValue(fail, pos, ssa.OpCall, ssa.TupleOf(ssa.TMem), panicArgs...)
	call.Aux = panicSym
	call.Call = &ssa.CallInfo{ABI: ssa.ABIInternal, NoReturn: true}
	sel := g.newValue(fail, pos, ssa.OpSelectN, ssa.TMem, call)
	fail.SetControl(sel)
}

// genMaskOnly removes the tag from the address of every access through a
// pointer that may have one, without checking it (the runtime and the
// allocator, and PLUTO_GENCHECK_MASKONLY, which measures the masking's
// cost alone).
func genMaskOnly(f *ssa.Func) {
	parts := map[*ssa.Value]*genParts{}
	type use struct {
		v     *ssa.Value
		arg   int
		root  *ssa.Value
		steps []*ssa.Value
	}
	var uses []use
	for _, b := range f.Blocks {
		for _, v := range b.Values {
			addrArgs := genAddrArgs(v)
			for _, ai := range addrArgs {
				root, steps := addrRoot(v.Args[ai])
				if isStaticAddr(root) || isUntagPattern(root) {
					continue
				}
				uses = append(uses, use{v, ai, root, steps})
			}
		}
	}
	g := &genPlacer{f: f}
	for _, u := range uses {
		p := parts[u.root]
		if p == nil {
			p = genPartsAfter(f, u.root)
			parts[u.root] = p
		}
		u.v.SetArg(u.arg, g.rebuild(u.v.Block, u.v, p.addr, u.steps))
	}
}
