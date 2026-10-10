package opt

import "github.com/matjam/plutonium/internal/ssa"

// BranchElim replaces small if/else diamonds and if-then triangles that
// only compute integer values with straight-line code: both sides are
// computed and the merge's phis become Selects (cmov). This removes
// branches the hardware cannot predict, such as a test of a number's
// parity, at the cost of always doing the work of both sides.
func BranchElim(f *ssa.Func) {
	users := map[*ssa.Value][]*ssa.Value{}
	for _, b := range f.Blocks {
		for _, v := range b.Values {
			for _, a := range v.Args {
				users[a] = append(users[a], v)
			}
		}
	}
	changed := false
	for _, b := range f.Blocks {
		if elimBranch(b, users) {
			changed = true
		}
	}
	if changed {
		f.RemoveDeadBlocks()
		ssa.CopyElim(f)
	}
}

// maxHoisted is the most values BranchElim moves out of the arms of a
// branch; beyond it, computing both arms costs more than a mispredict.
const maxHoisted = 4

// maxSelects is the most phis BranchElim turns into Selects at one merge.
const maxSelects = 2

// feedsAddress reports whether v reaches the address of a memory access
// through integer arithmetic and phis. A load whose address waits on a
// cmov cannot start early, whereas a predicted branch lets the processor
// run ahead, so such selects are better left as branches (as in a binary
// heap's choice of which child to visit next).
func feedsAddress(v *ssa.Value, users map[*ssa.Value][]*ssa.Value) bool {
	seen := map[*ssa.Value]bool{}
	var walk func(v *ssa.Value, depth int) bool
	walk = func(v *ssa.Value, depth int) bool {
		if seen[v] || depth > 6 {
			return false
		}
		seen[v] = true
		for _, u := range users[v] {
			switch u.Op {
			case ssa.OpAddPtr, ssa.OpOffPtr:
				return true
			case ssa.OpLoad, ssa.OpStore, ssa.OpZero, ssa.OpMove:
				if u.Args[0] == v {
					return true
				}
			case ssa.OpPhi, ssa.OpCopy, ssa.OpAdd, ssa.OpSub, ssa.OpMul, ssa.OpShl,
				ssa.OpZeroExt, ssa.OpSignExt, ssa.OpTrunc, ssa.OpSelect, ssa.OpBitcast:
				if walk(u, depth+1) {
					return true
				}
			}
		}
		return false
	}
	return walk(v, 0)
}

func elimBranch(b *ssa.Block, users map[*ssa.Value][]*ssa.Value) bool {
	if b.Kind != ssa.BlockIf || b.Likely != ssa.Unknown {
		return false
	}
	c := b.Controls[0]
	t, e := b.Succs[0].B, b.Succs[1].B
	arm := func(x *ssa.Block) bool {
		return x != b && x.Kind == ssa.BlockPlain && len(x.Preds) == 1
	}
	var m *ssa.Block
	var arms []*ssa.Block
	var tPred, ePred *ssa.Block // the predecessors of m on the true and false paths
	switch {
	case arm(t) && arm(e) && t.Succs[0].B == e.Succs[0].B:
		m, arms, tPred, ePred = t.Succs[0].B, []*ssa.Block{t, e}, t, e
	case arm(t) && t.Succs[0].B == e:
		m, arms, tPred, ePred = e, []*ssa.Block{t}, t, b
	case arm(e) && e.Succs[0].B == t:
		m, arms, tPred, ePred = t, []*ssa.Block{e}, b, e
	default:
		return false
	}
	if m == b || len(m.Preds) != 2 || m.Preds[0].B == m.Preds[1].B {
		return false
	}
	n := 0
	for _, a := range arms {
		for _, v := range a.Values {
			if !hoistable(v) {
				return false
			}
			if v.Op != ssa.OpConst {
				n++
			}
		}
	}
	var phis []*ssa.Value
	for _, v := range m.Values {
		if v.Op != ssa.OpPhi {
			break
		}
		if !v.Type.IsInteger() || v.Type.Size > 8 || feedsAddress(v, users) {
			return false
		}
		phis = append(phis, v)
	}
	if n > maxHoisted || len(phis) == 0 || len(phis) > maxSelects {
		return false
	}

	// Hoist the arms into b and select at the end of b.
	for _, a := range arms {
		for _, v := range a.Values {
			v.Block = b
		}
		b.Values = append(b.Values, a.Values...)
		a.Values = nil
	}
	ti, ei := 0, 1
	if m.Preds[0].B != tPred {
		ti, ei = 1, 0
	}
	if m.Preds[ti].B != tPred || m.Preds[ei].B != ePred {
		panic("branchelim: merge predecessors do not match the arms")
	}
	for _, phi := range phis {
		x, y := phi.Args[ti], phi.Args[ei]
		if x == y {
			phi.CopyOf(x)
			continue
		}
		phi.CopyOf(b.NewValue(ssa.OpSelect, phi.Type, c, x, y))
	}

	// b now falls straight through the true arm (or to m).
	b.RemoveSucc(1)
	b.Kind = ssa.BlockPlain
	b.SetControl()
	b.Likely = ssa.Unknown
	return true
}

// hoistable reports whether v can run unconditionally: it has no side
// effects, cannot fault, and is cheap.
func hoistable(v *ssa.Value) bool {
	switch v.Op {
	case ssa.OpConst, ssa.OpCopy, ssa.OpSlotAddr, ssa.OpSymAddr, ssa.OpOffPtr, ssa.OpAddPtr,
		ssa.OpAdd, ssa.OpSub, ssa.OpMul, ssa.OpAnd, ssa.OpOr, ssa.OpXor, ssa.OpAndNot,
		ssa.OpNeg, ssa.OpCom, ssa.OpNot, ssa.OpZeroExt, ssa.OpSignExt, ssa.OpTrunc, ssa.OpSelect,
		ssa.OpEq, ssa.OpNe, ssa.OpLess, ssa.OpLeq, ssa.OpLessu, ssa.OpLequ:
		return !v.Type.IsFloat() && !v.Type.IsMem()
	case ssa.OpShl, ssa.OpShrL, ssa.OpShrA, ssa.OpRotl:
		// Variable counts need range fix-ups; constant ones are one instruction.
		return v.Args[1].Op == ssa.OpConst
	}
	return false
}
