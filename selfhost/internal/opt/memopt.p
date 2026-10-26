// Translated by go2pu from internal/opt/memopt.go at the go-cutover tag; edited by hand since.

package opt

import (
	"selfhost/compat"
	"selfhost/internal/ssa"
	"selfhost/pu"
)

// MemOpt removes redundant memory operations by following the memory
// chain:
//
//   - a load of what a store just wrote becomes the stored value;
//   - a load of bytes a Zero just cleared becomes the constant 0;
//   - a load becomes an earlier load of the same bytes when only writes
//     that cannot touch those bytes lie between them;
//   - a store of the value just loaded from the same address, with nothing
//     written there in between, is dropped;
//   - a store that a later store to the same bytes overwrites, with nothing
//     reading memory in between, is dropped.
//
// Two accesses are known apart when they are at non-overlapping constant
// offsets from the same pointer, or in different stack slots or globals.
// Volatile accesses, calls, phis and other memory operations end a walk,
// except that a load from a stack slot whose address the function keeps to
// itself sees past calls, which cannot reach the slot.
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
					if redundantStore(v) || deadStoreBelow(v, lf) {
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

// The alias analysis lives in package ssa, which the scheduler shares.
var (
	locOf     = ssa.LocOf
	objectOf  = ssa.ObjectOf
	sameBytes = ssa.SameBytes
	disjoint  = ssa.Disjoint
	written   = ssa.Written
)

// A loadFinder finds the loads that read a memory state.
type loadFinder struct {
	dom     *ssa.DomTree
	readers map[*ssa.Value][]*ssa.Value // memory state -> loads of it
	pos     []int                       // index of each value in its block
	private map[*ssa.Slot]bool          // slots no call can reach (privateSlots)
}

func newLoadFinder(f *ssa.Func) *loadFinder {
	lf := pu.New(loadFinder{dom: f.Dom(), readers: map[*ssa.Value][]*ssa.Value{}, pos: compat.Make[int](f.NumValues()), private: privateSlots(f)})
	for _, b := range f.Blocks {
		for i, v := range b.Values {
			lf.pos[v.ID] = i
			if v.Op == ssa.OpLoad && !v.IsVolatile() {
				lf.readers[v.Args[1]] = compat.Append(lf.readers[v.Args[1]], v)
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
		if w.Block == v.Block && lf.pos[w.ID] < lf.pos[v.ID] && lf.near(w, v) || w.Block != v.Block && lf.dom.Dominates(w.Block, v.Block) {
			return w
		}
	}
	return nil
}

// otherReaders returns the loads reading memory state m, provided none of
// them reads any of the n bytes at p.
func (lf *loadFinder) otherReaders(m, p *ssa.Value, n int64) ([]*ssa.Value, bool) {
	var out []*ssa.Value
	for _, w := range lf.readers[m] {
		if w.Op != ssa.OpLoad || w.Args[1] != m {
			continue // merged away this round
		}
		if !disjoint(p, n, w.Args[0], w.Type.Size) {
			return nil, false
		}
		out = compat.Append(out, w)
	}
	return out, true
}

// addrReadsMemory reports whether p is computed, within block b, from a
// value that reads or writes memory.
func addrReadsMemory(p *ssa.Value, b *ssa.Block) bool {
	seen := map[*ssa.Value]bool{}
	var walk func(x *ssa.Value) bool
	walk = func(x *ssa.Value) bool {
		if x.Block != b || x.Op == ssa.OpPhi || seen[x] {
			return false
		}
		seen[x] = true
		if x.Op.HasMemArg() || x.Op == ssa.OpSelectN {
			return true
		}
		for _, a := range x.Args {
			if walk(a) {
				return true
			}
		}
		return false
	}
	return walk(p)
}

// memOptWindow is how far apart, in values of one block, a load may be
// from the earlier load or store whose value it reuses. Reusing a value
// keeps it in a register until then; across a long stretch of code (a
// fully unrolled loop) that costs spills, which are dearer than loading
// again.
const memOptWindow = 64

// near reports whether w, earlier in v's block, is close enough to v for
// v to reuse its value.
func (lf *loadFinder) near(w, v *ssa.Value) bool {
	return w.Block != v.Block || lf.pos[v.ID]-lf.pos[w.ID] <= memOptWindow
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
			if c := lf.callPast(m, p); c != nil {
				m = c
				continue
			}
			if c := lf.phiPast(m, p, n); c != nil {
				m = c
				continue
			}
			break
		}
		if m.Op == ssa.OpZero && v.Type.IsInteger() && within(p, n, q, size) {
			// Bytes a Zero cleared read as zero.
			v.Reset(ssa.OpConst)
			v.AuxInt = 0
			return true
		}
		if m.Op == ssa.OpStore && sameBytes(p, q) {
			if !m.Args[1].Type.Same(v.Type) || !lf.near(m, v) {
				break
			}
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

// within reports whether the n bytes at p lie inside the m bytes at q.
func within(p *ssa.Value, n int64, q *ssa.Value, m int64) bool {
	lp, lq := locOf(p), locOf(q)
	return lp.Base == lq.Base && n >= 0 && m >= 0 && lq.Off <= lp.Off && lp.Off+n <= lq.Off+m
}

// privateSlots returns the stack slots whose address is used only to load
// from and store to them, or passed only to calls that never return: no
// call that returns can read or change them. The defer record is left
// out, since the runtime finds it without being given its address.
func privateSlots(f *ssa.Func) map[*ssa.Slot]bool {
	private := map[*ssa.Slot]bool{}
	leaked := map[*ssa.Slot]bool{}
	if f.DeferRecord != nil {
		leaked[f.DeferRecord] = true
	}
	// derived maps an address to the slot it points into.
	derived := map[*ssa.Value]*ssa.Slot{}
	use := func(s *ssa.Slot, u, w *ssa.Value) {
		switch u.Op {
		case ssa.OpLoad, ssa.OpZero:
			if u.Args[0] != w {
				leaked[s] = true
			}
		case ssa.OpStore:
			if u.Args[1] == w {
				leaked[s] = true
			}
		case ssa.OpMove:
		case ssa.OpOffPtr, ssa.OpAddPtr:
			if u.Args[0] != w {
				leaked[s] = true
			}
		case ssa.OpCall, ssa.OpCallInd:
			if u.Call == nil || !u.Call.NoReturn {
				leaked[s] = true
			}
		default:
			leaked[s] = true
		}
	}
	for _, b := range f.Blocks {
		for _, v := range b.Values {
			if v.Op == ssa.OpSlotAddr {
				s := v.Aux.(*ssa.Slot)
				derived[v] = s
				private[s] = true
			}
		}
	}
	// Offsets from an address may come before it in f.Blocks.
	for changed := true; changed; {
		changed = false
		for _, b := range f.Blocks {
			for _, v := range b.Values {
				if v.Op != ssa.OpOffPtr && v.Op != ssa.OpAddPtr || derived[v] != nil {
					continue
				}
				if s := derived[v.Args[0]]; s != nil {
					derived[v] = s
					changed = true
				}
			}
		}
	}
	for _, b := range f.Blocks {
		for _, v := range b.Values {
			for _, a := range v.Args {
				if s := derived[a]; s != nil {
					use(s, v, a)
				}
			}
		}
		for _, c := range b.Controls {
			if s := derived[c]; s != nil {
				leaked[s] = true
			}
		}
	}
	for s := range leaked {
		delete(private, s)
	}
	return private
}

// callPast returns the memory state before m, if m is the memory a call
// returns and p points into a slot no call can reach; or nil.
func (lf *loadFinder) callPast(m, p *ssa.Value) *ssa.Value {
	if m.Op != ssa.OpSelectN || m.AuxInt != 0 || !m.Args[0].Op.IsCall() {
		return nil
	}
	s, ok := objectOf(p).(*ssa.Slot)
	if !ok || !lf.private[s] {
		return nil
	}
	return m.Args[0].MemArg()
}

// phiPast returns the memory state that every argument of the memory phi
// m comes from without a write to the n bytes at p, or nil.
func (lf *loadFinder) phiPast(m, p *ssa.Value, n int64) *ssa.Value {
	if m.Op != ssa.OpPhi || !m.Type.IsMem() {
		return nil
	}
	var from *ssa.Value
	for _, a := range m.Args {
		for i := 0; i < maxMemWalk && a != from; i++ {
			if q, size, ok := written(a); ok && disjoint(p, n, q, size) {
				a = a.MemArg()
			} else if c := lf.callPast(a, p); c != nil {
				a = c
			} else {
				break
			}
		}
		if a == m {
			return nil
		}
		if from == nil {
			from = a
		} else if a != from {
			return nil
		}
	}
	return from
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
func deadStoreBelow(v *ssa.Value, lf *loadFinder) bool {
	if v.IsVolatile() {
		return false
	}
	p, n := v.Args[0], v.StoreType().Size
	user := v // the value whose memory argument is m
	m := v.Args[2]
	for i := 0; i < maxMemWalk; i++ {
		// Besides user, only loads of other bytes may read m.
		readers, ok := lf.otherReaders(m, p, n)
		if !ok || m.Uses != int32(1+len(readers)) {
			return false
		}
		q, size, ok := written(m)
		if !ok {
			return false
		}
		if m.Op == ssa.OpStore && sameBytes(p, q) && size == n {
			// The loads that read the dead store's memory read the
			// memory before it instead, which holds the same bytes for
			// them. They then precede the store in the schedule, which
			// their addresses must allow.
			if m.Uses > 1 {
				for _, r := range readers {
					if addrReadsMemory(r.Args[0], r.Block) {
						return false
					}
				}
			}
			user.SetArg(len(user.Args)-1, m.Args[2])
			for _, r := range readers {
				r.SetArg(1, m.Args[2])
			}
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
