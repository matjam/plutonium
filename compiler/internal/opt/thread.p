// Translated by go2pu from internal/opt/thread.go at the go-cutover tag; edited by hand since.

package opt

import (
	"compiler/compat"
	"compiler/internal/ssa"
)

// Jump threading of branches on phis of constants.
//
// A block that holds nothing but phis and branches on one of them,
//
//	b: x = phi(1, 0, y); if x goto t else u
//
// decides the branch statically for each predecessor that passes a
// constant: that predecessor can jump straight to t or u. Inlined functions
// returning constant booleans leave such blocks, and so does a loop over a
// function: the loop body returns true to go on and false to stop, and the
// iterator branches on the result. Threading the branches turns the
// iterator's loop and the body back into one loop.
//
// When the predecessor moves from b to t, the values of b's phis along its
// edge must reach their uses another way. Uses in phis of t, on the edge
// from b, get an argument for the new edge. Uses dominated by t, when t's
// only predecessor was b, read a new phi in t. Any other use (a use past a
// join of t and u, say) stops the threading.

// threadPhiBranch threads one predecessor of b past it, if it can.
func threadPhiBranch(f *ssa.Func, b *ssa.Block, uses *map[*ssa.Value][]phiUse) bool {
	if b.Kind != ssa.BlockIf || b == f.Entry {
		return false
	}
	c := b.Controls[0]
	if c.Op != ssa.OpPhi || c.Block != b {
		return false
	}
	for _, v := range b.Values {
		if v.Op != ssa.OpPhi {
			return false
		}
	}
	i := -1
	for j, e := range b.Preds {
		if e.B == b {
			return false
		}
		if c.Args[j].Op == ssa.OpConst && i < 0 {
			i = j
		}
	}
	if i < 0 {
		return false
	}
	k := 0
	if c.Args[i].AuxInt == 0 {
		k = 1
	}
	t := b.Succs[k].B
	if t == b {
		return false
	}
	dom := f.Dom()
	sole := len(t.Preds) == 1
	if *uses == nil {
		*uses = phiUseInventory(f)
	}
	if !phiUsesThreadable(b, t, sole, dom, *uses) {
		return false
	}

	// The values b's phis have along the threaded edge.
	along := map[*ssa.Value]*ssa.Value{}
	for _, v := range b.Values {
		along[v] = v.Args[i]
	}
	valueAlong := func(a *ssa.Value) *ssa.Value {
		if w, ok := along[a]; ok {
			return w
		}
		return a
	}

	// Uses dominated by t read new phis in t, when t had b alone before.
	var fresh map[*ssa.Value]*ssa.Value
	if sole {
		fresh = map[*ssa.Value]*ssa.Value{}
		for _, v := range b.Values {
			if usedUnder(f, v, t, dom) {
				fresh[v] = nil
			}
		}
	}

	p, pe := b.Preds[i].B, b.Preds[i].I
	edgeFromB := b.Succs[k].I
	b.RemovePred(i)
	p.Succs[pe] = ssa.Edge{B: t, I: len(t.Preds)}
	t.Preds = compat.Append(t.Preds, ssa.Edge{B: p, I: pe})
	for _, v := range t.Values {
		if v.Op == ssa.OpPhi {
			v.AddArg(valueAlong(v.Args[edgeFromB]))
		}
	}
	for _, v := range b.Values {
		if _, ok := fresh[v]; !ok {
			continue
		}
		var phi *ssa.Value
		if len(t.Values) > 0 {
			phi = t.NewValueBefore(t.Values[0], ssa.OpPhi, v.Type)
		} else {
			phi = t.NewValue(ssa.OpPhi, v.Type)
		}
		phi.AddArg(v)
		phi.AddArg(along[v])
		fresh[v] = phi
	}
	if len(fresh) > 0 {
		replaceUsesUnder(f, fresh, t, dom)
	}
	f.InvalidateCFG()
	return true
}

type phiUse struct {
	at      *ssa.Block
	phiEdge bool
}

// phiUseInventory records every argument and control occurrence for the
// current threading scan. Its caller discards it after any CFG mutation.
func phiUseInventory(f *ssa.Func) map[*ssa.Value][]phiUse {
	uses := make(map[*ssa.Value][]phiUse)
	for _, blk := range f.Blocks {
		for _, v := range blk.Values {
			for j, a := range v.Args {
				if a.Op != ssa.OpPhi || a.Block.Kind != ssa.BlockIf {
					continue
				}
				at := blk
				if v.Op == ssa.OpPhi {
					at = blk.Preds[j].B
				}
				uses[a] = compat.Append(uses[a], phiUse{at, v.Op == ssa.OpPhi})
			}
		}
		for _, a := range blk.Controls {
			if a.Op == ssa.OpPhi && a.Block.Kind == ssa.BlockIf {
				uses[a] = compat.Append(uses[a], phiUse{blk, false})
			}
		}
	}
	return uses
}

// phiUsesThreadable reports whether every use of b's phis outside b is one
// threading into t can repair, or one it does not affect: a use dominated
// by another successor whose only predecessor is b still has b above it.
func phiUsesThreadable(b, t *ssa.Block, sole bool, dom *ssa.DomTree, uses map[*ssa.Value][]phiUse) bool {
	check := func(a *ssa.Value, at *ssa.Block, phiEdge bool) bool {
		if a.Op != ssa.OpPhi || a.Block != b || at == b && !phiEdge {
			return true
		}
		if phiEdge && at == b {
			return true // an argument of a phi in a successor, on the edge from b
		}
		if sole && dom.Dominates(t, at) {
			return true
		}
		for _, e := range b.Succs {
			if u := e.B; u != t && len(u.Preds) == 1 && dom.Dominates(u, at) {
				return true
			}
		}
		return false
	}
	for _, v := range b.Values {
		for _, use := range uses[v] {
			if !check(v, use.at, use.phiEdge) {
				return false
			}
		}
	}
	return true
}

// usedUnder reports whether v is used in a block dominated by t (a phi
// argument counts as a use at the end of its predecessor).
func usedUnder(f *ssa.Func, v *ssa.Value, t *ssa.Block, dom *ssa.DomTree) bool {
	used := false
	for _, blk := range f.Blocks {
		for _, w := range blk.Values {
			for j, a := range w.Args {
				at := blk
				if w.Op == ssa.OpPhi {
					at = blk.Preds[j].B
				}
				if a == v && dom.Dominates(t, at) {
					used = true
				}
			}
		}
		for _, a := range blk.Controls {
			if a == v && dom.Dominates(t, blk) {
				used = true
			}
		}
	}
	return used
}

// replaceUsesUnder replaces the uses of each key of repl in blocks
// dominated by t with its value, except in the replacements themselves.
func replaceUsesUnder(f *ssa.Func, repl map[*ssa.Value]*ssa.Value, t *ssa.Block, dom *ssa.DomTree) {
	fresh := map[*ssa.Value]bool{}
	for _, w := range repl {
		fresh[w] = true
	}
	for _, blk := range f.Blocks {
		for _, w := range blk.Values {
			if fresh[w] {
				continue
			}
			for j, a := range w.Args {
				r, ok := repl[a]
				if !ok {
					continue
				}
				at := blk
				if w.Op == ssa.OpPhi {
					at = blk.Preds[j].B
				}
				if dom.Dominates(t, at) {
					w.SetArg(j, r)
				}
			}
		}
		if dom.Dominates(t, blk) {
			cs := compat.Append([]*ssa.Value(nil), blk.Controls...)
			changed := false
			for j, a := range cs {
				if r, ok := repl[a]; ok {
					cs[j] = r
					changed = true
				}
			}
			if changed {
				blk.SetControl(cs...)
			}
		}
	}
}
