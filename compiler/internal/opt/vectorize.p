// Translated by go2pu from internal/opt/vectorize.go at the go-cutover tag; edited by hand since.

package opt

import (
	"compiler/compat"
	"compiler/internal/ssa"
	"compiler/pu"
	"pluto/math"
)

// Vectorize runs counted loops over float64 data two iterations at a time
// in SSE2 packed doubles (ssa.TF64x2). A loop qualifies when
//
//   - it is a header testing i < n, with i stepping by one from a phi and n
//     loop-invariant, and a single body block;
//   - its only phis are i and the memory state (no reductions or other
//     values carried between iterations);
//   - its body holds only float64 loads and stores at base + i*stride +
//     offset, float64 arithmetic, and the integer arithmetic that computes
//     those addresses; and
//   - no access of one iteration can overlap a store of another, which
//     holds when every access through a base lies within one stride of the
//     others and different bases are different stack slots or globals.
//
// The vector loop runs first while two iterations remain; the original
// loop then runs what is left. Each lane performs exactly the scalar
// operations of its iteration, in the same order, so results are
// bit-identical: nothing is reassociated, and there are no reductions.
//
// Loads of one field from consecutive elements are packed with a load
// and a load-high (movsd, movhpd); contiguous float64 arrays use one
// 16-byte load. Stores split the lanes the same way. Loop-invariant
// operands are broadcast before the loop.
//
// Functions that may not use vector instructions (ssa.Func.MayVectorize)
// are left alone.
func Vectorize(f *ssa.Func) {
	if !f.MayVectorize() {
		return
	}
	var headers []*ssa.Block
	for _, l := range f.Loops().Loops {
		if len(l.Blocks) == 2 {
			headers = compat.Append(headers, l.Header)
		}
	}
	changed := false
	for _, h := range headers {
		if vl := matchVecLoop(f, h); vl != nil {
			vl.rewrite(f)
			f.InvalidateCFG()
			changed = true
		}
	}
	if changed {
		ssa.DeadCode(f)
	}
}

// minVecFloatOps is the fewest float operations worth vectorising a loop
// for.
const minVecFloatOps = 2

// A vecAccess is a float64 load or store at base + i*stride + off.
type vecAccess struct {
	v      *ssa.Value
	base   *ssa.Value // loop-invariant pointer
	stride int64
	off    int64
}

// A vecLoop is a loop Vectorize can rewrite.
type vecLoop struct {
	h, body       *ssa.Block
	preIdx, latch int // indexes into h.Preds
	i, mem, n     *ssa.Value
	accesses      map[*ssa.Value]vecAccess
}

func matchVecLoop(f *ssa.Func, h *ssa.Block) *vecLoop {
	if h.Kind != ssa.BlockIf || len(h.Preds) != 2 || len(h.Succs) != 2 {
		return nil
	}
	body := h.Succs[0].B
	if body == h || body.Kind != ssa.BlockPlain || len(body.Preds) != 1 || body.Succs[0].B != h {
		return nil
	}
	vl := pu.New(vecLoop{h: h, body: body, latch: body.Succs[0].I})
	vl.preIdx = 1 - vl.latch
	inLoop := func(v *ssa.Value) bool { return v.Block == h || v.Block == body }

	cond := h.Controls[0]
	if cond.Op != ssa.OpLess || cond.Block != h || cond.Uses != 1 {
		return nil
	}
	vl.i, vl.n = cond.Args[0], cond.Args[1]
	if vl.i.Op != ssa.OpPhi || vl.i.Block != h || !vl.i.Type.Same(ssa.TI64) || inLoop(vl.n) {
		return nil
	}
	inc := vl.i.Args[vl.latch]
	if inc.Op != ssa.OpAdd || inc.Block != body || inc.Args[0] != vl.i || !constEq(inc.Args[1], 1) {
		return nil
	}
	for _, v := range h.Values {
		switch {
		case v == vl.i || v == cond:
		case v.Op == ssa.OpPhi && v.Type.IsMem() && vl.mem == nil:
			vl.mem = v
		default:
			return nil
		}
	}
	if vl.mem == nil || vl.mem.Args[vl.latch].Block != body {
		return nil // nothing stored: nothing to gain
	}

	// Classify the body.
	vl.accesses = map[*ssa.Value]vecAccess{}
	floatOps := 0
	for _, v := range body.Values {
		switch v.Op {
		case ssa.OpLoad, ssa.OpStore:
			t := v.Type
			if v.Op == ssa.OpStore {
				t = v.StoreType()
			}
			if v.IsVolatile() || !t.Same(ssa.TF64) {
				return nil
			}
			base, stride, off, ok := vl.affinePtr(v.Args[0])
			if !ok || v.Op == ssa.OpStore && stride == 0 {
				return nil
			}
			vl.accesses[v] = vecAccess{v: v, base: base, stride: stride, off: off}
		case ssa.OpFAdd, ssa.OpFSub, ssa.OpFMul, ssa.OpFDiv, ssa.OpFNeg, ssa.OpFSqrt, ssa.OpFAbs:
			if !v.Type.Same(ssa.TF64) {
				return nil
			}
			floatOps++
		case ssa.OpAdd, ssa.OpSub, ssa.OpMul, ssa.OpShl, ssa.OpAddPtr, ssa.OpOffPtr:
			// Address arithmetic, rebuilt from the accesses' strides and
			// offsets; anything else using it is rejected below.
		default:
			return nil
		}
	}
	if floatOps < minVecFloatOps {
		return nil
	}
	// Memory flows from the phi through the stores, each consuming the
	// previous one; loads read the phi or a store of the body.
	m := vl.mem
	for _, v := range body.Values {
		switch v.Op {
		case ssa.OpLoad:
			if a := v.Args[1]; a != vl.mem && (a.Block != body || a.Op != ssa.OpStore) {
				return nil
			}
		case ssa.OpStore:
			if v.Args[2] != m {
				return nil
			}
			m = v
		}
	}
	if vl.mem.Args[vl.latch] != m {
		return nil
	}
	// Integer values may feed only addresses and the increment: float ops
	// take floats, and stores and loads only float64s, so the one way out
	// is a store of an address-typed value, which the type check above
	// already rejects. Float values come from the body or from outside the
	// loop; phis other than i and mem were rejected.
	for _, v := range body.Values {
		for _, a := range v.Args {
			if a.Type.IsFloat() && a.Block == h {
				return nil
			}
		}
	}
	if !vl.independent() {
		return nil
	}
	return vl
}

// affinePtr writes p as base + i*stride + off with base loop-invariant.
func (vl *vecLoop) affinePtr(p *ssa.Value) (base *ssa.Value, stride, off int64, ok bool) {
	if p.Block != vl.h && p.Block != vl.body {
		return p, 0, 0, true
	}
	switch p.Op {
	case ssa.OpOffPtr:
		base, stride, off, ok = vl.affinePtr(p.Args[0])
		return base, stride, off + p.AuxInt, ok && fitsOffset(off+p.AuxInt)
	case ssa.OpAddPtr:
		base, stride, off, ok = vl.affinePtr(p.Args[0])
		s, o, ok2 := vl.affineInt(p.Args[1])
		if !ok || !ok2 || stride != 0 {
			return nil, 0, 0, false
		}
		return base, s, off + o, fitsOffset(s) && fitsOffset(off+o)
	}
	return nil, 0, 0, false
}

// affineInt writes integer x as i*scale + off.
func (vl *vecLoop) affineInt(x *ssa.Value) (scale, off int64, ok bool) {
	if x == vl.i {
		return 1, 0, true
	}
	if !x.Type.Same(ssa.TI64) {
		return 0, 0, false
	}
	if x.Op == ssa.OpConst {
		return 0, x.AuxInt, true
	}
	if x.Block != vl.body {
		return 0, 0, false
	}
	switch x.Op {
	case ssa.OpMul, ssa.OpShl:
		c := x.Args[1]
		if !isConst(c) {
			return 0, 0, false
		}
		k := c.AuxInt
		if x.Op == ssa.OpShl {
			if k < 0 || k > 20 {
				return 0, 0, false
			}
			k = 1 << k
		}
		if !fitsOffset(k) {
			return 0, 0, false
		}
		s, o, ok := vl.affineInt(x.Args[0])
		return s * k, o * k, ok && fitsOffset(s*k) && fitsOffset(o*k)
	case ssa.OpAdd, ssa.OpSub:
		s1, o1, ok1 := vl.affineInt(x.Args[0])
		s2, o2, ok2 := vl.affineInt(x.Args[1])
		if x.Op == ssa.OpSub {
			s2, o2 = -s2, -o2
		}
		return s1 + s2, o1 + o2, ok1 && ok2 && fitsOffset(s1+s2) && fitsOffset(o1+o2)
	}
	return 0, 0, false
}

// fitsOffset keeps strides and offsets small enough that sums of a few of
// them cannot overflow and lane addresses fit a displacement.
func fitsOffset(c int64) bool { return c > -1<<24 && c < 1<<24 }

// independent reports whether no access of one iteration can touch the
// bytes a store of another iteration writes.
func (vl *vecLoop) independent() bool {
	var all, stores []vecAccess
	for _, a := range vl.accesses {
		all = compat.Append(all, a)
		if a.v.Op == ssa.OpStore {
			stores = compat.Append(stores, a)
		}
	}
	for _, s := range stores {
		for _, a := range all {
			if a.v == s.v {
				continue
			}
			lb, ls := locOf(a.base), locOf(s.base)
			if lb.Base != ls.Base {
				oa, os := objectOf(a.base), objectOf(s.base)
				if oa == nil || os == nil || oa == os {
					return false
				}
				continue
			}
			// The same base: iteration k touches [k*stride + off, +8).
			// Two accesses of different iterations are apart when their
			// offsets differ by at most |stride| - 8.
			if a.stride != s.stride {
				return false
			}
			d := (lb.Off + a.off) - (ls.Off + s.off)
			st := s.stride
			if d < 0 {
				d = -d
			}
			if st < 0 {
				st = -st
			}
			if d > st-8 {
				return false
			}
		}
	}
	return true
}

// rewrite builds the vector loop in front of the original one.
func (vl *vecLoop) rewrite(f *ssa.Func) {
	h, body := vl.h, vl.body
	preEdge := h.Preds[vl.preIdx]
	pre := preEdge.B
	init, minit := vl.i.Args[vl.preIdx], vl.mem.Args[vl.preIdx]

	vh := f.NewBlock(ssa.BlockIf)
	vb := f.NewBlock(ssa.BlockPlain)
	vexit := f.NewBlock(ssa.BlockPlain)
	vh.CopyPos(h)
	vb.CopyPos(body)
	vexit.CopyPos(h)

	// pre -> vh, replacing pre -> h.
	pre.Succs[preEdge.I] = ssa.Edge{B: vh, I: 0}
	vh.Preds = compat.Append(vh.Preds, ssa.Edge{B: pre, I: preEdge.I})
	// vexit -> h, in pre's place among h's predecessors.
	vexit.Succs = compat.Append(vexit.Succs, ssa.Edge{B: h, I: vl.preIdx})
	h.Preds[vl.preIdx] = ssa.Edge{B: vexit, I: 0}

	vi := vh.NewValue(ssa.OpPhi, ssa.TI64, init)
	vm := vh.NewValue(ssa.OpPhi, ssa.TMem, minit)
	vl.i.SetArg(vl.preIdx, vi)
	vl.mem.SetArg(vl.preIdx, vm)

	// Two iterations remain while i+1 < n. With a constant n that is
	// i < n-1; otherwise test i < n first so that i+1 cannot overflow.
	n := vl.n
	if isConst(n) && n.AuxInt != math.MinInt64 {
		vh.SetControl(vh.NewValue(ssa.OpLess, ssa.TBool, vi, f.ConstInt(ssa.TI64, n.AuxInt-1)))
		vh.AddEdgeTo(vb)
		vh.AddEdgeTo(vexit)
	} else {
		vh2 := f.NewBlock(ssa.BlockIf)
		vh2.CopyPos(h)
		vh.SetControl(vh.NewValue(ssa.OpLess, ssa.TBool, vi, n))
		vh.AddEdgeTo(vh2)
		vh.AddEdgeTo(vexit)
		next := vh2.NewValue(ssa.OpAdd, ssa.TI64, vi, f.ConstInt(ssa.TI64, 1))
		vh2.SetControl(vh2.NewValue(ssa.OpLess, ssa.TBool, next, n))
		vh2.AddEdgeTo(vb)
		vh2.AddEdgeTo(vexit)
		vh2.Likely = ssa.Likely
	}
	vh.Likely = h.Likely
	vb.AddEdgeTo(vh)

	// Broadcasts of loop-invariant floats go before the loop.
	splats := map[*ssa.Value]*ssa.Value{}
	vmap := map[*ssa.Value]*ssa.Value{}
	vec := func(x *ssa.Value) *ssa.Value {
		if v := vmap[x]; v != nil {
			return v
		}
		if s := splats[x]; s != nil {
			return s
		}
		s := pre.NewValue(ssa.OpVPack, ssa.TF64x2, x, x)
		splats[x] = s
		return s
	}
	// Each base's address for lane 0, i*stride from the base.
	type baseKey struct {
		base   *ssa.Value
		stride int64
	}
	bases := map[baseKey]*ssa.Value{}
	laneAddr := func(a vecAccess, lane int64) *ssa.Value {
		p := a.base
		if a.stride != 0 {
			k := baseKey{a.base, a.stride}
			if bases[k] == nil {
				idx := vb.NewValue(ssa.OpMul, ssa.TI64, vi, f.ConstInt(ssa.TI64, a.stride))
				bases[k] = vb.NewValue(ssa.OpAddPtr, ssa.TPtr, a.base, idx)
			}
			p = bases[k]
		}
		if off := a.off + lane*a.stride; off != 0 {
			p = vb.NewValueI(ssa.OpOffPtr, ssa.TPtr, off, p)
		}
		return p
	}

	memMap := map[*ssa.Value]*ssa.Value{vl.mem: vm}
	for _, v := range body.Values {
		first := len(vb.Values)
		switch v.Op {
		case ssa.OpLoad:
			a := vl.accesses[v]
			m := memMap[v.Args[1]]
			switch a.stride {
			case 8:
				vmap[v] = vb.NewValue(ssa.OpLoad, ssa.TF64x2, laneAddr(a, 0), m)
			case 0:
				x := vb.NewValue(ssa.OpLoad, ssa.TF64, laneAddr(a, 0), m)
				vmap[v] = vb.NewValue(ssa.OpVPack, ssa.TF64x2, x, x)
			default:
				x0 := vb.NewValue(ssa.OpLoad, ssa.TF64, laneAddr(a, 0), m)
				x1 := vb.NewValue(ssa.OpLoad, ssa.TF64, laneAddr(a, 1), m)
				vmap[v] = vb.NewValue(ssa.OpVPack, ssa.TF64x2, x0, x1)
			}
		case ssa.OpStore:
			a := vl.accesses[v]
			m := memMap[v.Args[2]]
			x := vec(v.Args[1])
			if a.stride == 8 {
				memMap[v] = vb.NewValueA(ssa.OpStore, ssa.TMem, ssa.TF64x2, laneAddr(a, 0), x, m)
				break
			}
			for lane := int64(0); lane < 2; lane++ {
				x := vb.NewValueI(ssa.OpVLane, ssa.TF64, lane, x)
				m = vb.NewValueA(ssa.OpStore, ssa.TMem, ssa.TF64, laneAddr(a, lane), x, m)
			}
			memMap[v] = m
		case ssa.OpFAdd, ssa.OpFSub, ssa.OpFMul, ssa.OpFDiv, ssa.OpFNeg, ssa.OpFSqrt, ssa.OpFAbs:
			w := vb.NewValue(v.Op, ssa.TF64x2)
			for _, a := range v.Args {
				w.AddArg(vec(a))
			}
			vmap[v] = w
		}
		// The vector code for v takes v's position, so that a fault in a
		// vector load or store names the line (and inlined call) of the
		// scalar access it replaces.
		for _, w := range vb.Values[first:] {
			w.CopyPos(v)
		}
	}
	vi.AddArg(vb.NewValue(ssa.OpAdd, ssa.TI64, vi, f.ConstInt(ssa.TI64, 2)))
	vm.AddArg(memMap[vl.mem.Args[vl.latch]])
}
