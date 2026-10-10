package opt

import "github.com/matjam/plutonium/internal/ssa"

// MemOpt removes redundant memory operations by following the memory
// chain:
//
//   - a load of what a store just wrote becomes the stored value;
//   - a load becomes an earlier load of the same bytes when only writes
//     that cannot touch those bytes lie between them;
//   - a store of the value just loaded from the same address, with nothing
//     written there in between, is dropped;
//   - a store that a later store to the same bytes overwrites, with nothing
//     reading memory in between, is dropped.
//
// Two accesses are known apart when they are at non-overlapping constant
// offsets from the same pointer, or in different stack slots or globals.
// Volatile accesses, calls, phis and other memory operations end a walk.
func MemOpt(f *ssa.Func) {
	// A rewrite leaves copies in the memory chain, which end the walks of
	// later values; the next round sees past them.
	for round := 0; round < 8; round++ {
		changed := false
		lf := newLoadFinder(f)
		for _, b := range f.Blocks {
			for _, v := range b.Values {
				switch v.Op {
				case ssa.OpLoad:
					if forwardLoad(v, lf) {
						changed = true
					}
				case ssa.OpStore:
					if redundantStore(v) || deadStoreBelow(v) {
						changed = true
					}
				}
			}
		}
		if !changed {
			break
		}
		ssa.CopyElim(f)
		ssa.DeadCode(f)
	}
}

// maxMemWalk bounds how far a walk follows the memory chain.
const maxMemWalk = 64

// A memLoc is a pointer split into a base and a constant offset.
type memLoc struct {
	base any // *ssa.Slot, *ssa.Sym or the base *ssa.Value
	off  int64
}

// locOf splits p into a base and constant offset.
func locOf(p *ssa.Value) memLoc {
	var off int64
	for {
		switch {
		case p.Op == ssa.OpOffPtr:
			off += p.AuxInt
			p = p.Args[0]
			continue
		case p.Op == ssa.OpAddPtr && p.Args[1].Op == ssa.OpConst:
			off += p.Args[1].AuxInt
			p = p.Args[0]
			continue
		case p.Op == ssa.OpSlotAddr || p.Op == ssa.OpSymAddr:
			return memLoc{base: p.Aux, off: off}
		}
		return memLoc{base: p, off: off}
	}
}

// objectOf returns the stack slot or global p points into, or nil.
func objectOf(p *ssa.Value) any {
	for p.Op == ssa.OpOffPtr || p.Op == ssa.OpAddPtr {
		p = p.Args[0]
	}
	if p.Op == ssa.OpSlotAddr || p.Op == ssa.OpSymAddr {
		return p.Aux
	}
	return nil
}

// sameBytes reports whether p and q address the same location.
func sameBytes(p, q *ssa.Value) bool {
	return p == q || locOf(p) == locOf(q)
}

// disjoint reports whether n bytes at p and m bytes at q cannot overlap.
// A negative size means unknown.
func disjoint(p *ssa.Value, n int64, q *ssa.Value, m int64) bool {
	lp, lq := locOf(p), locOf(q)
	if lp.base == lq.base && n >= 0 && m >= 0 {
		return lp.off+n <= lq.off || lq.off+m <= lp.off
	}
	op, oq := objectOf(p), objectOf(q)
	return op != nil && oq != nil && op != oq
}

// written returns the address and size a plain memory write changes, and
// false for anything else (calls, phis, volatile stores and so on).
func written(m *ssa.Value) (p *ssa.Value, size int64, ok bool) {
	switch m.Op {
	case ssa.OpStore:
		if m.IsVolatile() {
			return nil, 0, false
		}
		return m.Args[0], m.StoreType().Size, true
	case ssa.OpZero, ssa.OpMove:
		return m.Args[0], m.AuxInt, true
	case ssa.OpFill:
		return m.Args[0], -1, true
	}
	return nil, 0, false
}

// A loadFinder finds the loads that read a memory state.
type loadFinder struct {
	dom     *ssa.DomTree
	readers map[*ssa.Value][]*ssa.Value // memory state -> loads of it
	pos     []int                       // index of each value in its block
}

func newLoadFinder(f *ssa.Func) *loadFinder {
	lf := &loadFinder{dom: f.Dom(), readers: map[*ssa.Value][]*ssa.Value{}, pos: make([]int, f.NumValues())}
	for _, b := range f.Blocks {
		for i, v := range b.Values {
			lf.pos[v.ID] = i
			if v.Op == ssa.OpLoad && !v.IsVolatile() {
				lf.readers[v.Args[1]] = append(lf.readers[v.Args[1]], v)
			}
		}
	}
	return lf
}

// find returns a load other than v of the same bytes and type as v,
// reading memory state m, that dominates v; or nil.
func (lf *loadFinder) find(v, m *ssa.Value) *ssa.Value {
	for _, w := range lf.readers[m] {
		if w == v || w.Op != ssa.OpLoad || !w.Type.Same(v.Type) || !sameBytes(w.Args[0], v.Args[0]) {
			continue
		}
		if w.Block == v.Block && lf.pos[w.ID] < lf.pos[v.ID] || w.Block != v.Block && lf.dom.Dominates(w.Block, v.Block) {
			return w
		}
	}
	return nil
}

// forwardLoad applies the load rules to v.
func forwardLoad(v *ssa.Value, lf *loadFinder) bool {
	if v.IsVolatile() {
		return false
	}
	p, n := v.Args[0], v.Type.Size
	m := v.Args[1]
	for i := 0; i < maxMemWalk; i++ {
		if i > 0 {
			if w := lf.find(v, m); w != nil {
				v.CopyOf(w)
				return true
			}
		}
		q, size, ok := written(m)
		if !ok {
			break
		}
		if m.Op == ssa.OpStore && sameBytes(p, q) && m.Args[1].Type.Same(v.Type) {
			v.CopyOf(m.Args[1])
			return true
		}
		if !disjoint(p, n, q, size) {
			break
		}
		m = m.MemArg()
	}
	return false
}

// redundantStore drops a store of the value loaded from the same address
// when nothing in between can have changed it.
func redundantStore(v *ssa.Value) bool {
	x := v.Args[1]
	if v.IsVolatile() || x.Op != ssa.OpLoad || x.IsVolatile() || !sameBytes(v.Args[0], x.Args[0]) || !x.Type.Same(v.StoreType()) {
		return false
	}
	p, n := v.Args[0], v.StoreType().Size
	m := v.Args[2]
	for i := 0; i < maxMemWalk && m != x.Args[1]; i++ {
		q, size, ok := written(m)
		if !ok || !disjoint(p, n, q, size) {
			return false
		}
		m = m.MemArg()
	}
	if m != x.Args[1] {
		return false
	}
	v.CopyOf(v.Args[2])
	return true
}

// deadStoreBelow drops an earlier store that v overwrites. The memory
// states between them must have no other readers.
func deadStoreBelow(v *ssa.Value) bool {
	if v.IsVolatile() {
		return false
	}
	p, n := v.Args[0], v.StoreType().Size
	user := v // the value whose memory argument is m
	m := v.Args[2]
	for i := 0; i < maxMemWalk; i++ {
		if m.Uses != 1 {
			return false
		}
		q, size, ok := written(m)
		if !ok {
			return false
		}
		if m.Op == ssa.OpStore && sameBytes(p, q) && size == n {
			user.SetArg(len(user.Args)-1, m.Args[2])
			return true
		}
		if !disjoint(p, n, q, size) {
			return false
		}
		// A copy reads its source: the earlier store must not be in it.
		if m.Op == ssa.OpMove && !disjoint(p, n, m.Args[1], size) {
			return false
		}
		user, m = m, m.MemArg()
	}
	return false
}
