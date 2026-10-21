package eir

import "sort"

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
				stack = append(stack, f.Blocks[x].Preds...)
			}
		}
	}
	var loops []Loop
	for h, body := range bodies {
		l := Loop{Header: h, Parent: -1}
		for x := range body {
			l.Blocks = append(l.Blocks, x)
		}
		sort.Slice(l.Blocks, func(i, j int) bool { return l.Blocks[i] < l.Blocks[j] })
		loops = append(loops, l)
	}
	// Outermost first: larger loops contain smaller ones; ties by header.
	sort.Slice(loops, func(i, j int) bool {
		if len(loops[i].Blocks) != len(loops[j].Blocks) {
			return len(loops[i].Blocks) > len(loops[j].Blocks)
		}
		return loops[i].Header < loops[j].Header
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

func contains(s []BlockID, x BlockID) bool {
	i := sort.Search(len(s), func(i int) bool { return s[i] >= x })
	return i < len(s) && s[i] == x
}

// dominators returns the immediate dominator of each block (Cooper, Harvey
// and Kennedy, "A Simple, Fast Dominance Algorithm"); -1 for the entry and
// for unreachable blocks.
func dominators(f *Func) []int {
	n := len(f.Blocks)
	order := make([]BlockID, 0, n) // reverse postorder
	num := make([]int, n)
	for i := range num {
		num[i] = -1
	}
	seen := make([]bool, n)
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
				stack = append(stack, frame{s, 0})
			}
			continue
		}
		post = append(post, top.b)
		stack = stack[:len(stack)-1]
	}
	for i := len(post) - 1; i >= 0; i-- {
		num[post[i]] = len(order)
		order = append(order, post[i])
	}
	idom := make([]int, n)
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
	live := make([]bool, n)
	stack := []BlockID{0}
	live[0] = true
	for len(stack) > 0 {
		b := f.Blocks[stack[len(stack)-1]]
		stack = stack[:len(stack)-1]
		for _, s := range b.Succs {
			if !live[s] {
				live[s] = true
				stack = append(stack, s)
			}
		}
	}
	newID := make([]BlockID, n)
	var blocks []*Block
	for _, b := range f.Blocks {
		if live[b.ID] {
			newID[b.ID] = BlockID(len(blocks))
			blocks = append(blocks, b)
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
				keep = append(keep, i)
				preds = append(preds, newID[p])
			}
		}
		for i := range b.Instrs {
			in := &b.Instrs[i]
			if in.Op != OpPhi || len(in.Args) != len(b.Preds) {
				continue
			}
			args := make([]Reg, len(keep))
			for j, k := range keep {
				args[j] = in.Args[k]
			}
			in.Args = args
		}
		b.Preds = preds
		succs := b.Succs[:0]
		for _, s := range b.Succs {
			succs = append(succs, newID[s])
		}
		b.Succs = succs
		b.ID = newID[b.ID]
	}
	f.Blocks = blocks
}
