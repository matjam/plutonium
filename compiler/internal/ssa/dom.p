// Translated by go2pu from internal/ssa/dom.go at the go-cutover tag; edited by hand since.

package ssa

import (
	"compiler/compat"
	"compiler/pu"
)

// PostOrder returns the reachable blocks in postorder.
func (f *Func) PostOrder() []*Block {
	if f.cachedOrder != nil {
		return f.cachedOrder
	}
	seen := compat.Make[bool](f.NumBlocks())
	order := compat.MakeCap[*Block](0, len(f.Blocks))
	type frame struct {
		b *Block
		i int
	}
	stack := []frame{{f.Entry, 0}}
	seen[f.Entry.ID] = true
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		if top.i < len(top.b.Succs) {
			s := top.b.Succs[top.i].B
			top.i++
			if !seen[s.ID] {
				seen[s.ID] = true
				stack = compat.Append(stack, frame{s, 0})
			}
			continue
		}
		order = compat.Append(order, top.b)
		stack = stack[:len(stack)-1]
	}
	f.cachedOrder = order
	return order
}

// ReversePostOrder returns the reachable blocks in reverse postorder: every
// block comes before its successors except along back edges.
func (f *Func) ReversePostOrder() []*Block {
	po := f.PostOrder()
	rpo := compat.Make[*Block](len(po))
	for i, b := range po {
		rpo[len(po)-1-i] = b
	}
	return rpo
}

// A DomTree is the dominator tree of a function.
type DomTree struct {
	idom []*Block // immediate dominator by block ID; nil for the entry and unreachable blocks
	// The children of the block with ID i in the dominator tree are
	// kids[kidStart[i]:kidStart[i+1]], in reverse postorder: one array
	// for all the blocks instead of a slice for each.
	kids      []*Block
	kidStart  []int32
	pre, post []int32 // preorder and postorder numbers in the tree, for O(1) dominance tests
	rpoNum    []int32 // reverse postorder number by block ID; -1 if unreachable
}

// Dom returns the dominator tree, computing it if needed.
func (f *Func) Dom() *DomTree {
	if f.cachedDom == nil {
		f.cachedDom = computeDom(f)
	}
	return f.cachedDom
}

// computeDom uses the iterative algorithm of Cooper, Harvey and Kennedy,
// "A Simple, Fast Dominance Algorithm".
func computeDom(f *Func) *DomTree {
	n := f.NumBlocks()
	// The blocks in reverse postorder are po read backwards; the cached
	// postorder is not copied.
	po := f.PostOrder()
	d := pu.New(DomTree{
		idom:     compat.Make[*Block](n),
		kidStart: compat.Make[int32](n + 1),
		pre:      compat.Make[int32](n),
		post:     compat.Make[int32](n),
		rpoNum:   compat.Make[int32](n),
	})
	for i := range d.rpoNum {
		d.rpoNum[i] = -1
	}
	for i, b := range po {
		d.rpoNum[b.ID] = int32(len(po) - 1 - i)
	}

	entry := f.Entry
	d.idom[entry.ID] = entry
	intersect := func(a, b *Block) *Block {
		for a != b {
			for d.rpoNum[a.ID] > d.rpoNum[b.ID] {
				a = d.idom[a.ID]
			}
			for d.rpoNum[b.ID] > d.rpoNum[a.ID] {
				b = d.idom[b.ID]
			}
		}
		return a
	}
	for changed := true; changed; {
		changed = false
		for i := len(po) - 2; i >= 0; i-- {
			b := po[i]
			var newIdom *Block
			for _, e := range b.Preds {
				p := e.B
				if d.rpoNum[p.ID] < 0 || d.idom[p.ID] == nil {
					continue
				}
				if newIdom == nil {
					newIdom = p
				} else {
					newIdom = intersect(p, newIdom)
				}
			}
			if d.idom[b.ID] != newIdom {
				d.idom[b.ID] = newIdom
				changed = true
			}
		}
	}
	d.idom[entry.ID] = nil
	// Count each block's children, turn the counts into starts, and fill
	// the children in reverse postorder.
	for i := len(po) - 2; i >= 0; i-- {
		if p := d.idom[po[i].ID]; p != nil {
			d.kidStart[p.ID+1]++
		}
	}
	for i := 1; i <= n; i++ {
		d.kidStart[i] += d.kidStart[i-1]
	}
	d.kids = compat.Make[*Block](int(d.kidStart[n]))
	fill := compat.Make[int32](n)
	copy(fill, d.kidStart[:n])
	for i := len(po) - 2; i >= 0; i-- {
		b := po[i]
		if p := d.idom[b.ID]; p != nil {
			d.kids[fill[p.ID]] = b
			fill[p.ID]++
		}
	}

	// Number the tree for constant-time dominance queries.
	var pre, post int32
	type frame struct {
		b *Block
		i int
	}
	stack := []frame{{entry, 0}}
	d.pre[entry.ID] = pre
	pre++
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		kids := d.Children(top.b)
		if top.i < len(kids) {
			c := kids[top.i]
			top.i++
			d.pre[c.ID] = pre
			pre++
			stack = compat.Append(stack, frame{c, 0})
			continue
		}
		d.post[top.b.ID] = post
		post++
		stack = stack[:len(stack)-1]
	}
	return d
}

// Idom returns b's immediate dominator, or nil for the entry.
func (d *DomTree) Idom(b *Block) *Block { return d.idom[b.ID] }

// Children returns the blocks b immediately dominates.
func (d *DomTree) Children(b *Block) []*Block {
	lo, hi := d.kidStart[b.ID], d.kidStart[b.ID+1]
	return d.kids[lo:hi:hi]
}

// Reachable reports whether b is reachable from the entry.
func (d *DomTree) Reachable(b *Block) bool { return d.rpoNum[b.ID] >= 0 }

// RPONum returns b's reverse postorder number.
func (d *DomTree) RPONum(b *Block) int { return int(d.rpoNum[b.ID]) }

// Dominates reports whether a dominates b (a block dominates itself).
func (d *DomTree) Dominates(a, b *Block) bool {
	if !d.Reachable(a) || !d.Reachable(b) {
		return false
	}
	return d.pre[a.ID] <= d.pre[b.ID] && d.post[b.ID] <= d.post[a.ID]
}

// Frontiers returns the dominance frontier of every block, by block ID.
func (d *DomTree) Frontiers(f *Func) [][]*Block {
	df := compat.Make[[]*Block](f.NumBlocks())
	for _, b := range f.Blocks {
		if len(b.Preds) < 2 || !d.Reachable(b) {
			continue
		}
		for _, e := range b.Preds {
			runner := e.B
			if !d.Reachable(runner) {
				continue
			}
			for runner != d.idom[b.ID] && runner != nil {
				list := df[runner.ID]
				if len(list) == 0 || list[len(list)-1] != b {
					df[runner.ID] = compat.Append(list, b)
				}
				runner = d.idom[runner.ID]
			}
		}
	}
	return df
}

// A Loop is a natural loop.
type Loop struct {
	Header *Block
	Outer  *Loop
	Blocks []*Block // all blocks in the loop, including nested loops
	Depth  int      // 1 for an outermost loop
	Exits  []*Block // blocks outside the loop with a predecessor inside it
}

// A LoopNest describes the loops of a function.
type LoopNest struct {
	Loops []*Loop
	inner []*Loop // innermost loop containing each block, by block ID
}

// Loops returns the loop nest, computing it if needed.
func (f *Func) Loops() *LoopNest {
	if f.cachedLoops == nil {
		f.cachedLoops = computeLoops(f)
	}
	return f.cachedLoops
}

// LoopOf returns the innermost loop containing b, or nil.
func (ln *LoopNest) LoopOf(b *Block) *Loop { return ln.inner[b.ID] }

// Depth returns the loop depth of b (0 outside any loop).
func (ln *LoopNest) Depth(b *Block) int {
	if l := ln.inner[b.ID]; l != nil {
		return l.Depth
	}
	return 0
}

// Contains reports whether l contains b.
func (l *Loop) Contains(ln *LoopNest, b *Block) bool {
	for m := ln.inner[b.ID]; m != nil; m = m.Outer {
		if m == l {
			return true
		}
	}
	return false
}

func computeLoops(f *Func) *LoopNest {
	d := f.Dom()
	ln := pu.New(LoopNest{inner: compat.Make[*Loop](f.NumBlocks())})
	byHeader := map[*Block]*Loop{}

	// Find back edges p -> h where h dominates p, and collect each loop body by
	// walking predecessors back from p to h. Visit headers in postorder so inner
	// loops are found before the loops that contain them.
	for _, h := range f.PostOrder() {
		var body []*Block
		for _, e := range h.Preds {
			if d.Dominates(h, e.B) {
				body = compat.Append(body, e.B)
			}
		}
		if len(body) == 0 {
			continue
		}
		l := pu.New(Loop{Header: h})
		byHeader[h] = l
		ln.Loops = compat.Append(ln.Loops, l)
		inLoop := map[*Block]bool{h: true}
		work := body
		for len(work) > 0 {
			b := work[len(work)-1]
			work = work[:len(work)-1]
			if inLoop[b] || !d.Reachable(b) {
				continue
			}
			inLoop[b] = true
			for _, e := range b.Preds {
				work = compat.Append(work, e.B)
			}
		}
		for _, b := range f.Blocks {
			if inLoop[b] {
				l.Blocks = compat.Append(l.Blocks, b)
				if inner := ln.inner[b.ID]; inner == nil {
					ln.inner[b.ID] = l
				} else {
					// b is in an inner loop found earlier: make l its outermost parent.
					for inner.Outer != nil {
						inner = inner.Outer
					}
					if inner != l {
						inner.Outer = l
					}
				}
			}
		}
	}
	for _, l := range ln.Loops {
		depth := 0
		for m := l; m != nil; m = m.Outer {
			depth++
		}
		l.Depth = depth
		seen := map[*Block]bool{}
		for _, b := range l.Blocks {
			for _, e := range b.Succs {
				if !l.Contains(ln, e.B) && !seen[e.B] {
					seen[e.B] = true
					l.Exits = compat.Append(l.Exits, e.B)
				}
			}
		}
	}
	return ln
}
