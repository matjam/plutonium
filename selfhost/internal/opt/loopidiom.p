// Translated by go2pu from internal/opt/loopidiom.go at the go-cutover tag; edited by hand since.

package opt

import (
	"selfhost/internal/ssa"
)

// LoopIdiom replaces a loop that stores one constant byte to each
// consecutive address, a long constant distance, with a single Fill:
//
//	for i := 0; i < 1000; i++ { p[i] = c }
//
// The body block then fills p[i:n] and sets i to n, so the loop runs its
// body once and exits with the same values as before.
func LoopIdiom(f *ssa.Func) {
	changed := false
	for _, h := range f.Blocks {
		if fillLoop(f, h) {
			changed = true
		}
	}
	if changed {
		ssa.DeadCode(f)
	}
}

// minFill is the shortest fill worth a Fill.
const minFill = 256

// fillLoop rewrites the loop headed by h if it is a byte-fill loop.
func fillLoop(f *ssa.Func, h *ssa.Block) bool {
	if h.Kind != ssa.BlockIf || len(h.Preds) != 2 || len(h.Succs) != 2 {
		return false
	}
	b := h.Succs[0].B
	if b == h || b.Kind != ssa.BlockPlain || len(b.Preds) != 1 || len(b.Succs) != 1 || b.Succs[0].B != h {
		return false
	}
	k := b.Succs[0].I // b's index among h's predecessors
	cond := h.Controls[0]
	if cond.Op != ssa.OpLess || cond.Block != h {
		return false
	}
	i, n := cond.Args[0], cond.Args[1]
	outside := func(v *ssa.Value) bool { return v.Block != h && v.Block != b }
	if i.Op != ssa.OpPhi || i.Block != h || !i.Type.Same(ssa.TI64) || !outside(n) {
		return false
	}
	// rep stosb takes some time to start, so only long fills pay. The
	// bounds must be constants to know that.
	if init := i.Args[1-k]; !isConst(init) || !isConst(n) || n.AuxInt-init.AuxInt < minFill {
		return false
	}

	// The body: the store, the address, the increment and constants.
	var st, inc *ssa.Value
	for _, v := range b.Values {
		switch v.Op {
		case ssa.OpStore:
			if st != nil {
				return false
			}
			st = v
		case ssa.OpAdd:
			if inc != nil {
				return false
			}
			inc = v
		case ssa.OpAddPtr, ssa.OpConst:
		default:
			return false
		}
	}
	if st == nil || inc == nil || st.IsVolatile() || st.StoreType().Size != 1 {
		return false
	}
	if i.Args[k] != inc || inc.Args[0] != i || !constEq(inc.Args[1], 1) {
		return false
	}
	addr, val := st.Args[0], st.Args[1]
	if addr.Op != ssa.OpAddPtr || addr.Args[1] != i || !outside(addr.Args[0]) || val.Op != ssa.OpConst {
		return false
	}
	// Only the memory and i are carried around the loop, and the body's
	// values are used only inside it.
	m := st.Args[2]
	if m.Op != ssa.OpPhi || m.Block != h || m.Args[k] != st {
		return false
	}
	for _, v := range h.Values {
		switch {
		case v == i || v == m:
		case v.Op == ssa.OpPhi, v.Op.HasSideEffect(), v.Op.HasMemResult():
			return false
		}
	}
	if inc.Uses != 1 || st.Uses != 1 || addr.Uses != 1 {
		return false
	}

	length := b.NewValueBefore(st, ssa.OpSub, ssa.TI64, n, i)
	fill := b.NewValueBefore(st, ssa.OpFill, ssa.TMem, addr, val, length, m)
	m.SetArg(k, fill)
	i.SetArg(k, n)
	f.InvalidateCFG()
	return true
}
