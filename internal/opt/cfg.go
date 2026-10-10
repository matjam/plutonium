package opt

import "github.com/matjam/plutonium/internal/ssa"

// SimplifyCFG folds branches on constants, merges a block into its only
// predecessor when that predecessor has no other successor, and skips
// empty forwarding blocks. It runs to a fixed point.
func SimplifyCFG(f *ssa.Func) {
	for changed := true; changed; {
		changed = false
		for _, b := range f.Blocks {
			if foldConstBranch(b) {
				changed = true
			}
		}
		if changed {
			f.RemoveDeadBlocks()
		}
		verifyStep(f, "foldConstBranch")
		for _, b := range f.Blocks {
			if mergeIntoPred(f, b) {
				changed = true
				break // f.Blocks changed
			}
		}
		verifyStep(f, "mergeIntoPred")
		for _, b := range f.Blocks {
			if skipEmpty(f, b) {
				changed = true
				break
			}
		}
		verifyStep(f, "skipEmpty")
		f.RemoveDeadBlocks()
	}
	ssa.CopyElim(f)
}

// verifyStep checks the function after one step of a pass when
// PLUTO_SSACHECK is set.
func verifyStep(f *ssa.Func, step string) {
	if !checkSSA {
		return
	}
	if err := f.Verify(); err != nil {
		panic("after " + step + ": " + err.Error())
	}
}

// foldConstBranch turns "if const" into a jump.
func foldConstBranch(b *ssa.Block) bool {
	if b.Kind != ssa.BlockIf {
		return false
	}
	c := b.Controls[0]
	if c.Op != ssa.OpConst {
		// if (x) then T else T: either way.
		if b.Succs[0].B == b.Succs[1].B && !hasPhis(b.Succs[0].B) {
			b.RemoveSucc(1)
			b.Kind = ssa.BlockPlain
			b.SetControl()
			return true
		}
		return false
	}
	drop := 1
	if c.AuxInt == 0 {
		drop = 0
	}
	b.RemoveSucc(drop)
	b.Kind = ssa.BlockPlain
	b.SetControl()
	b.Likely = ssa.Unknown
	return true
}

func hasPhis(b *ssa.Block) bool {
	return len(b.Values) > 0 && b.Values[0].Op == ssa.OpPhi
}

// mergeIntoPred appends b to its only predecessor p if p jumps only to b.
func mergeIntoPred(f *ssa.Func, b *ssa.Block) bool {
	if len(b.Preds) != 1 || b == f.Entry {
		return false
	}
	p := b.Preds[0].B
	if p == b || p.Kind != ssa.BlockPlain || len(p.Succs) != 1 {
		return false
	}
	// Phis with a single predecessor are copies of their argument.
	for _, v := range b.Values {
		if v.Op == ssa.OpPhi {
			v.CopyOf(v.Args[0])
		}
	}
	for _, v := range b.Values {
		v.Block = p
	}
	p.Values = append(p.Values, b.Values...)
	b.Values = nil
	// p takes over b's ending.
	p.RemoveSucc(0)
	p.Kind = b.Kind
	p.SetControl(b.Controls...)
	b.SetControl()
	p.Likely = b.Likely
	for len(b.Succs) > 0 {
		e := b.Succs[0]
		// Rewire b -> s into p -> s keeping s's predecessor slot.
		s := e.B
		p.Succs = append(p.Succs, ssa.Edge{B: s, I: e.I})
		s.Preds[e.I] = ssa.Edge{B: p, I: len(p.Succs) - 1}
		b.Succs = b.Succs[1:]
	}
	// b is now empty and unreachable: drop it.
	for i, x := range f.Blocks {
		if x == b {
			f.Blocks = append(f.Blocks[:i], f.Blocks[i+1:]...)
			break
		}
	}
	f.InvalidateCFG()
	return true
}

// skipEmpty redirects predecessors of an empty block that only jumps on,
// straight to its target, when the target has no phis.
func skipEmpty(f *ssa.Func, b *ssa.Block) bool {
	if b == f.Entry || b.Kind != ssa.BlockPlain || len(b.Values) != 0 || len(b.Preds) == 0 {
		return false
	}
	target := b.Succs[0].B
	if target == b || hasPhis(target) {
		return false
	}
	for _, e := range append([]ssa.Edge(nil), b.Preds...) {
		p := e.B
		// Point p's successor edge at target, adding a predecessor there.
		p.Succs[e.I] = ssa.Edge{B: target, I: len(target.Preds)}
		target.Preds = append(target.Preds, ssa.Edge{B: p, I: e.I})
	}
	b.Preds = nil
	f.InvalidateCFG()
	return true
}
