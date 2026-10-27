// Translated by go2pu from internal/escape2/eir/loops.go at the go-cutover tag; edited by hand since.

package eir

import (
	"pluto/sort"
	"compiler/compat"
)

// findLoops returns the natural loops of f's CFG, outermost first, and sets
// each block's innermost loop. A loop is the set of blocks of the back edges
// to one header (an edge to a block that dominates its source), so a loop
// made with goto is a loop as one made with for is.
func findLoops(f *Func) []Loop {
	n := len(f.Blocks)
	if n == 0 {
		return nil
	}
	idom := dominators(f)
	dominates := func(a, b BlockID) bool {
		for {
			if a == b {
				return true
			}
			if b == 0 || idom[b] < 0 {
				return false
			}
			b = BlockID(idom[b])
		}
	}
	bodies := map[BlockID]map[BlockID]bool{}
	for _, b := range f.Blocks {
		if idom[b.ID] < 0 && b.ID != 0 {
			continue // unreachable
		}
		for _, s := range b.Succs {
			if !dominates(s, b.ID) {
				continue
			}
			body := bodies[s]
			if body == nil {
				body = map[BlockID]bool{s: true}
				bodies[s] = body
			}
			stack := []BlockID{b.ID}
			for len(stack) > 0 {
				x := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if body[x] {
					continue
				}
				body[x] = true
				stack = compat.AppendPlain(stack, f.Blocks[x].Preds...)
			}
		}
	}
	irreducible(f, idom, bodies, dominates)
	var loops []Loop
	for h, body := range bodies {
		l := Loop{Header: h, Parent: -1}
		for x := range body {
			l.Blocks = compat.AppendPlain(l.Blocks, x)
		}
		sort.SortFunc(l.Blocks, func(a, b BlockID) bool { return a < b })
		loops = compat.Append(loops, l)
	}
	// Outermost first: larger loops contain smaller ones; ties by header.
	sort.SortFunc(loops, func(a, b Loop) bool {
		if len(a.Blocks) != len(b.Blocks) {
			return len(a.Blocks) > len(b.Blocks)
		}
		return a.Header < b.Header
	})
	for i := range loops {
		for j := i - 1; j >= 0; j-- {
			if contains(loops[j].Blocks, loops[i].Header) {
				loops[i].Parent = j
				break
			}
		}
	}
	for _, b := range f.Blocks {
		b.Loop = -1
	}
	for i, l := range loops {
		for _, x := range l.Blocks {
			b := f.Blocks[x]
			if b.Loop < 0 || len(loops[b.Loop].Blocks) >= len(l.Blocks) {
				b.Loop = i
			}
		}
	}
	return loops
}

// irreducible adds to bodies the loops that have no header dominating them.
// A goto into the middle of a loop made with goto enters it at two blocks
// (section 12.17: a backward goto is a loop, and a forward goto may reach a
// label inside it), so no block dominates the back edge's source. Each edge
// to a block on the depth-first search's stack whose target does not
// dominate its source closes such a loop: its body is the blocks that are
// reached from the target and reach the source, and the target stands for
// its header.
func irreducible(f *Func, idom []int, bodies map[BlockID]map[BlockID]bool, dominates func(a, b BlockID) bool) {
	n := len(f.Blocks)
	onStack := compat.Make[bool](n)
	seen := compat.Make[bool](n)
	type frame struct {
		b BlockID
		i int
	}
	var retreat [][2]BlockID
	stack := []frame{{0, 0}}
	seen[0], onStack[0] = true, true
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		succs := f.Blocks[top.b].Succs
		if top.i < len(succs) {
			s := succs[top.i]
			top.i++
			if onStack[s] && !dominates(s, top.b) {
				retreat = compat.Append(retreat, [2]BlockID{top.b, s})
			}
			if !seen[s] {
				seen[s], onStack[s] = true, true
				stack = compat.Append(stack, frame{s, 0})
			}
			continue
		}
		onStack[top.b] = false
		stack = stack[:len(stack)-1]
	}
	for _, e := range retreat {
		src, h := e[0], e[1]
		ahead := map[BlockID]bool{h: true}
		work := []BlockID{h}
		for len(work) > 0 {
			x := work[len(work)-1]
			work = work[:len(work)-1]
			for _, s := range f.Blocks[x].Succs {
				if !ahead[s] && (idom[s] >= 0 || s == 0) {
					ahead[s] = true
					work = compat.AppendPlain(work, s)
				}
			}
		}
		body := bodies[h]
		if body == nil {
			body = map[BlockID]bool{h: true}
			bodies[h] = body
		}
		work = []BlockID{src}
		for len(work) > 0 {
			x := work[len(work)-1]
			work = work[:len(work)-1]
			if body[x] || !ahead[x] {
				continue
			}
			body[x] = true
			work = compat.AppendPlain(work, f.Blocks[x].Preds...)
		}
	}
}

// IsEntry reports whether block b is an entry of loop l: its header, or a
// block of an irreducible loop that a block outside it jumps to.
func (f *Func) IsEntry(l int, b BlockID) bool {
	loop := f.Loops[l]
	if loop.Header == b {
		return true
	}
	if !contains(loop.Blocks, b) {
		return false
	}
	for _, p := range f.Blocks[b].Preds {
		if !contains(loop.Blocks, p) {
			return true
		}
	}
	return false
}

func contains(s []BlockID, x BlockID) bool {
	i := sort.Search(len(s), func(i int) bool { return s[i] >= x })
	return i < len(s) && s[i] == x
}

// dominators returns the immediate dominator of each block (Cooper, Harvey
// and Kennedy, "A Simple, Fast Dominance Algorithm"); -1 for the entry and
// for unreachable blocks.
func dominators(f *Func) []int {
	n := len(f.Blocks)
	order := compat.MakeCap[BlockID](0, n) // reverse postorder
	num := compat.Make[int](n)
	for i := range num {
		num[i] = -1
	}
	seen := compat.Make[bool](n)
	var post []BlockID
	type frame struct {
		b BlockID
		i int
	}
	stack := []frame{{0, 0}}
	seen[0] = true
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		succs := f.Blocks[top.b].Succs
		if top.i < len(succs) {
			s := succs[top.i]
			top.i++
			if !seen[s] {
				seen[s] = true
				stack = compat.Append(stack, frame{s, 0})
			}
			continue
		}
		post = compat.AppendPlain(post, top.b)
		stack = stack[:len(stack)-1]
	}
	for i := len(post) - 1; i >= 0; i-- {
		num[post[i]] = len(order)
		order = compat.AppendPlain(order, post[i])
	}
	idom := compat.Make[int](n)
	for i := range idom {
		idom[i] = -1
	}
	idom[0] = 0
	intersect := func(a, b int) int {
		for a != b {
			for num[a] > num[b] {
				a = idom[a]
			}
			for num[b] > num[a] {
				b = idom[b]
			}
		}
		return a
	}
	for changed := true; changed; {
		changed = false
		for _, b := range order[1:] {
			nd := -1
			for _, p := range f.Blocks[b].Preds {
				if idom[p] < 0 {
					continue
				}
				if nd < 0 {
					nd = int(p)
				} else {
					nd = intersect(int(p), nd)
				}
			}
			if nd >= 0 && idom[b] != nd {
				idom[b] = nd
				changed = true
			}
		}
	}
	idom[0] = -1
	return idom
}

// removeUnreachable removes the blocks that cannot be reached from the
// entry, as the lowerer does (ssa.Func.RemoveDeadBlocks): code after a
// return, a branch or a call that never returns never runs. The phis of
// the blocks that remain lose the operands of removed predecessors.
func removeUnreachable(f *Func) {
	n := len(f.Blocks)
	live := compat.Make[bool](n)
	stack := []BlockID{0}
	live[0] = true
	for len(stack) > 0 {
		b := f.Blocks[stack[len(stack)-1]]
		stack = stack[:len(stack)-1]
		for _, s := range b.Succs {
			if !live[s] {
				live[s] = true
				stack = compat.AppendPlain(stack, s)
			}
		}
	}
	newID := compat.Make[BlockID](n)
	var blocks []*Block
	for _, b := range f.Blocks {
		if live[b.ID] {
			newID[b.ID] = BlockID(len(blocks))
			blocks = compat.Append(blocks, b)
		}
	}
	if len(blocks) == n {
		return
	}
	for _, b := range blocks {
		var keep []int
		var preds []BlockID
		for i, p := range b.Preds {
			if live[p] {
				keep = compat.AppendPlain(keep, i)
				preds = compat.AppendPlain(preds, newID[p])
			}
		}
		for i := range b.Instrs {
			in := &b.Instrs[i]
			if in.Op != OpPhi || len(in.Args) != len(b.Preds) {
				continue
			}
			args := compat.Make[Reg](len(keep))
			for j, k := range keep {
				args[j] = in.Args[k]
			}
			in.Args = args
		}
		b.Preds = preds
		succs := b.Succs[:0]
		for _, s := range b.Succs {
			succs = compat.AppendPlain(succs, newID[s])
		}
		b.Succs = succs
		b.ID = newID[b.ID]
	}
	f.Blocks = blocks
}
