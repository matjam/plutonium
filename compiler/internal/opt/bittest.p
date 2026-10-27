// Translated by go2pu from internal/opt/bittest.go at the go-cutover tag; edited by hand since.

package opt

import (
	"compiler/compat"
	"compiler/internal/ssa"
)

// BitTest turns a chain of comparisons of one value with constants close
// together,
//
//	c == ' ' || c == '\n' || c == '\t' || c == '\r'
//
// into a test of bit c-lo of a constant mask, (mask >> (c-lo)) & 1 != 0,
// which the backend selects as bt; lo is the smallest constant. The chain
// must be an Or of Eq (or an And of Ne, giving the negation) whose inner
// Ors are used only by the chain, with at least three constants, all less
// than 64 above lo. A logical shift by 64 or more gives 0, so values
// outside the range test false without a check.
//
// Telling the root of a chain from its inner nodes needs the users of each
// value, which the pass collects first.
func BitTest(f *ssa.Func) {
	users := map[*ssa.Value][]*ssa.Value{}
	for _, b := range f.Blocks {
		for _, v := range b.Values {
			for _, a := range v.Args {
				// Only chain nodes' users are asked for.
				if (a.Op == ssa.OpOr || a.Op == ssa.OpAnd) && a.Type.IsBool() {
					users[a] = compat.Append(users[a], v)
				}
			}
		}
	}
	changed := false
	for _, b := range f.Blocks {
		for i := 0; i < len(b.Values); i++ {
			v := b.Values[i]
			if v.Op != ssa.OpOr && v.Op != ssa.OpAnd || !v.Type.IsBool() {
				continue
			}
			// Only the root of a chain: no user continues it.
			root := true
			for _, u := range users[v] {
				if u.Op == v.Op && v.Uses == 1 {
					root = false
				}
			}
			if root && bitTestChain(f, v) {
				changed = true
			}
		}
	}
	if changed {
		ssa.CopyElim(f)
		ssa.DeadCode(f)
	}
}

// minBitTest is the fewest comparisons worth a bit test.
const minBitTest = 3

// bitTestChain rewrites the chain rooted at v, if it is one.
func bitTestChain(f *ssa.Func, v *ssa.Value) bool {
	leafOp := ssa.OpEq
	if v.Op == ssa.OpAnd {
		leafOp = ssa.OpNe
	}
	var x *ssa.Value
	var cs []uint64
	var walk func(w *ssa.Value, top bool) bool
	walk = func(w *ssa.Value, top bool) bool {
		if w.Op == v.Op && (top || w.Uses == 1) {
			return walk(w.Args[0], false) && walk(w.Args[1], false)
		}
		if w.Op != leafOp || !isConst(w.Args[1]) {
			return false
		}
		y := w.Args[0]
		if !y.Type.IsInt() || x != nil && y != x {
			return false
		}
		x = y
		cs = compat.AppendPlain(cs, ssa.ZeroExt(y.Type, w.Args[1].AuxInt))
		return true
	}
	if !walk(v, true) {
		return false
	}
	// The constants must lie within 64 of the smallest, lo; the bit
	// tested is x - lo. Comparing zero-extended values throughout, an x
	// below lo wraps to a count far above 63, which tests false.
	lo, hi := cs[0], cs[0]
	for _, c := range cs {
		lo, hi = min(lo, c), max(hi, c)
	}
	if hi < 64 {
		lo = 0 // no subtraction needed
	}
	var mask uint64
	n := 0
	for _, c := range cs {
		if c-lo >= 64 {
			return false
		}
		if mask&(1<<(c-lo)) == 0 {
			mask |= 1 << (c - lo)
			n++
		}
	}
	if n < minBitTest {
		return false
	}
	cnt := x
	if x.Type.Size < 8 {
		cnt = newBefore(v, ssa.OpZeroExt, ssa.TI64, x)
	}
	if lo != 0 {
		cnt = newBefore(v, ssa.OpSub, ssa.TI64, cnt, f.ConstInt(ssa.TI64, int64(lo)))
	}
	sh := newBefore(v, ssa.OpShrL, ssa.TI64, f.ConstInt(ssa.TI64, int64(mask)), cnt)
	bit := newBefore(v, ssa.OpAnd, ssa.TI64, sh, f.ConstInt(ssa.TI64, 1))
	op := ssa.OpNe
	if v.Op == ssa.OpAnd {
		op = ssa.OpEq
	}
	v.Reset(op)
	v.AddArg(bit)
	v.AddArg(f.ConstInt(ssa.TI64, 0))
	return true
}
