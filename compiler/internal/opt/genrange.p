// Translated by go2pu from internal/opt/genrange.go at the go-cutover tag; edited by hand since.

package opt

import (
	"compiler/compat"
	"compiler/internal/ssa"
	"compiler/pu"
)

// Range checks before counted loops (abi.md section 1.3, Loops).
//
// An access through base + i*scale in a loop over i checks a different
// address in each iteration, so neither sharing nor hoisting a check of
// one extent applies. When the loop surely runs to the end of its count
// once it starts, and touches the same extent of each element in every
// iteration, one range check before the loop covers every byte it will
// touch: hoistRange puts there the check of
//
//	[base + init*scale + off, base + (n-1)*scale + off + size)
//
// guarded by the loop's entry test, so that a loop that runs no iteration
// checks nothing. It does this only when:
//
//   - the loop holds no call of any kind and no barrier: nothing in it can
//     free, reset, drop, yield, end the task or the program, or let it see
//     another task's free, and nothing ends it early;
//   - the loop leaves only by its header's test, i < n (signed or
//     unsigned), entered when the test holds: no break, and no runtime
//     panic path, such as an index check prove could not remove;
//   - i is the header's phi, starting at init and stepping by 1 on every
//     way round, and init, n and base are fixed before the loop;
//   - the access is not in the header and every iteration reaches it, so
//     it touches the extent of every i from init to n-1;
//   - the extent of one element is at least scale bytes long, so the
//     extents of consecutive iterations leave no gap between them.
//
// The loop then touches exactly the bytes the check covers, in order, with
// nothing that can free between the check and any of the accesses: the
// range check is defined byte by byte, so it fails exactly when one of the
// accesses' own checks would have, only earlier. The only way the loop can
// stop before its count is a hardware trap (a division by zero, say), a
// runtime panic, which may then report the stale pointer instead, as for
// the checks hoisted by genPlacer.hoist.

// A genRange is a check hoistRange put at the end of a loop's preheader:
// of the n bytes at p, a pointer derived from base, when guard holds.
type genRange struct {
	pre                    *ssa.Block
	v                      *ssa.Value // the access it was hoisted from, for positions
	base, p, n, guard, mem *ssa.Value
}

// hoistRange puts before loop l one range check for each access whose
// address advances with the loop's count, under the conditions above.
func (g *genPlacer) hoistRange(l *ssa.Loop) {
	for _, b := range l.Blocks {
		if g.barrier[b.ID] || g.calls[b.ID] {
			return
		}
	}
	h := l.Header
	pre, pi := g.preheader(l)
	if pre == nil || h.Kind != ssa.BlockIf {
		return
	}
	for _, b := range l.Blocks {
		for _, e := range b.Succs {
			if b != h && !l.Contains(g.ln, e.B) {
				return // a break, or a panic path
			}
		}
	}
	if !l.Contains(g.ln, h.Succs[0].B) || l.Contains(g.ln, h.Succs[1].B) {
		return
	}
	c := h.Controls[0]
	if c.Op != ssa.OpLess && c.Op != ssa.OpLessu {
		return
	}
	i, n := c.Args[0], c.Args[1]
	if i.Op != ssa.OpPhi || i.Block != h || i.Type != ssa.TI64 || pi >= len(i.Args) {
		return
	}
	init := i.Args[pi]
	if !g.fixedBefore(l, pre, n) || !g.fixedBefore(l, pre, init) {
		return
	}
	for k, a := range i.Args {
		if k != pi && !genIncrement(a, i) {
			return
		}
	}
	var entry *genCond
	for _, s := range g.sites {
		if s.covered || s.arg < 0 || s.block == h || !l.Contains(g.ln, s.block) {
			continue
		}
		x := g.extOf[s.ext]
		r := x.root
		if r.Op != ssa.OpAddPtr || !g.fixedBefore(l, pre, r.Args[0]) {
			continue
		}
		scale := genScale(r.Args[1], i)
		if scale <= 0 || x.size < scale || !g.reaches(l, s.block) {
			continue
		}
		if entry == nil {
			entry = g.entryCond(l, pre, pi)
		}
		if !entry.ok || entry.v == nil {
			return
		}
		i64 := func(v int64) *ssa.Value { return g.f.ConstInt(ssa.TI64, v) }
		first := g.newValue(pre, s.v, ssa.OpMul, ssa.TI64, init, i64(scale))
		p := g.newValue(pre, s.v, ssa.OpAddPtr, ssa.TPtr, r.Args[0], first)
		if x.off != 0 {
			p = g.newValue(pre, s.v, ssa.OpOffPtr, ssa.TPtr, p)
			p.AuxInt = x.off
		}
		count := g.newValue(pre, s.v, ssa.OpSub, ssa.TI64, n, init)
		bytes := g.newValue(pre, s.v, ssa.OpMul, ssa.TI64, count, i64(scale))
		bytes = g.newValue(pre, s.v, ssa.OpAdd, ssa.TI64, bytes, i64(x.size-scale))
		s.covered = true
		g.ranges = compat.Append(g.ranges, pu.New(genRange{pre: pre, v: s.v, base: r.Args[0], p: p, n: bytes, guard: entry.v, mem: g.memOut(pre)}))
	}
}

// fixedBefore reports whether v is defined before loop l is entered from
// pre.
func (g *genPlacer) fixedBefore(l *ssa.Loop, pre *ssa.Block, v *ssa.Value) bool {
	return !l.Contains(g.ln, v.Block) && g.dom.Dominates(v.Block, pre)
}

// genIncrement reports whether a is i + 1.
func genIncrement(a, i *ssa.Value) bool {
	return a.Op == ssa.OpAdd && (a.Args[0] == i && constEq(a.Args[1], 1) || a.Args[1] == i && constEq(a.Args[0], 1))
}

// genScale returns s if idx is i*s for a constant s > 0, and 0 otherwise.
func genScale(idx, i *ssa.Value) int64 {
	switch {
	case idx == i:
		return 1
	case idx.Op == ssa.OpShl && idx.Args[0] == i && idx.Args[1].Op == ssa.OpConst &&
		idx.Args[1].AuxInt >= 0 && idx.Args[1].AuxInt < 32:
		return 1 << idx.Args[1].AuxInt
	case idx.Op == ssa.OpMul && idx.Args[0] == i && idx.Args[1].Op == ssa.OpConst && idx.Args[1].AuxInt > 0:
		return idx.Args[1].AuxInt
	case idx.Op == ssa.OpMul && idx.Args[1] == i && idx.Args[0].Op == ssa.OpConst && idx.Args[0].AuxInt > 0:
		return idx.Args[0].AuxInt
	}
	return 0
}

// checkRange ends r's preheader with its check.
func (g *genPlacer) checkRange(r *genRange) {
	cont := g.splitAt(r.pre, nil)
	g.lookup(r.pre, cont, r.v, r.mem, r.guard, g.partsOf(r.base).bits, r.p, r.n)
}
