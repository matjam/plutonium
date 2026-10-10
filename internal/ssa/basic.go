package ssa

// CopyElim replaces uses of OpCopy values (and of phis whose arguments are
// all the same value) with the copied value, and removes trivial phis.
func CopyElim(f *Func) {
	resolve := func(v *Value) *Value {
		// Follow copy chains; bounded to guard against cycles of copies,
		// which can only arise in unreachable code.
		for i := 0; v.Op == OpCopy && i < 1000; i++ {
			v = v.Args[0]
		}
		return v
	}
	for changed := true; changed; {
		changed = false
		for _, b := range f.Blocks {
			for _, v := range b.Values {
				for i, a := range v.Args {
					if r := resolve(a); r != a {
						v.SetArg(i, r)
						changed = true
					}
				}
				if v.Op == OpPhi {
					if same := trivialPhi(v); same != nil {
						v.CopyOf(same)
						changed = true
					}
				}
			}
			for i, c := range b.Controls {
				if r := resolve(c); r != c {
					c.Uses--
					b.Controls[i] = r
					r.Uses++
					changed = true
				}
			}
		}
	}
}

// trivialPhi returns the single value a phi merges (ignoring self
// references), or nil if it merges several.
func trivialPhi(v *Value) *Value {
	var same *Value
	for _, a := range v.Args {
		if a == v || a == same {
			continue
		}
		if same != nil {
			return nil
		}
		same = a
	}
	return same
}

// DeadCode removes unreachable blocks and values whose results are unused
// and that have no side effects.
func DeadCode(f *Func) {
	f.RemoveDeadBlocks()

	live := make([]bool, f.NumValues())
	var work []*Value
	mark := func(v *Value) {
		if !live[v.ID] {
			live[v.ID] = true
			work = append(work, v)
		}
	}
	for _, b := range f.Blocks {
		for _, c := range b.Controls {
			mark(c)
		}
		for _, v := range b.Values {
			if v.Op.HasSideEffect() || v.IsVolatile() {
				mark(v)
			}
		}
	}
	for len(work) > 0 {
		v := work[len(work)-1]
		work = work[:len(work)-1]
		for _, a := range v.Args {
			mark(a)
		}
	}
	for _, b := range f.Blocks {
		keep := b.Values[:0]
		for _, v := range b.Values {
			if live[v.ID] {
				keep = append(keep, v)
			} else {
				v.ResetArgs()
			}
		}
		clear(b.Values[len(keep):])
		b.Values = keep
	}
}

// SplitCriticalEdges inserts an empty block on every edge from a block with
// several successors to a block with several predecessors, so that phi
// copies have a place to go.
func SplitCriticalEdges(f *Func) {
	for _, b := range append([]*Block(nil), f.Blocks...) {
		if len(b.Succs) < 2 {
			continue
		}
		for i, e := range b.Succs {
			c := e.B
			if len(c.Preds) < 2 {
				continue
			}
			mid := f.NewBlock(BlockPlain)
			mid.Pos = b.Pos
			// Rewire b -> c into b -> mid -> c, keeping c's predecessor index
			// so its phis stay aligned.
			j := e.I
			b.Succs[i] = Edge{mid, 0}
			mid.Preds = append(mid.Preds, Edge{b, i})
			mid.Succs = append(mid.Succs, Edge{c, j})
			c.Preds[j] = Edge{mid, 0}
		}
	}
	f.InvalidateCFG()
}

// Schedule orders the values of each block: phis first, then every value
// after its arguments, memory operations in memory-chain order, and the
// original order otherwise.
func Schedule(f *Func) {
	pos := make([]int, f.NumValues())
	for _, b := range f.Blocks {
		for i, v := range b.Values {
			pos[v.ID] = i
		}
	}
	for _, b := range f.Blocks {
		inBlock := make(map[*Value]bool, len(b.Values))
		for _, v := range b.Values {
			inBlock[v] = true
		}
		// readers[m] lists the values that read memory state m without
		// producing a new one (loads). They must run before any operation
		// that consumes m to produce the next state.
		readers := map[*Value][]*Value{}
		for _, v := range b.Values {
			if m := v.MemArg(); m != nil && !v.Op.HasMemResult() {
				readers[m] = append(readers[m], v)
			}
		}
		done := make(map[*Value]bool, len(b.Values))
		out := make([]*Value, 0, len(b.Values))
		var visit func(v *Value)
		visit = func(v *Value) {
			if done[v] {
				return
			}
			done[v] = true
			if v.Op != OpPhi {
				for _, a := range v.Args {
					if inBlock[a] {
						visit(a)
					}
				}
				if v.Op.HasMemResult() {
					for _, r := range readers[v.MemArg()] {
						visit(r)
					}
				}
			}
			out = append(out, v)
		}
		for _, v := range b.Values {
			if v.Op == OpPhi {
				visit(v)
			}
		}
		for _, v := range b.Values {
			visit(v)
		}
		b.Values = out
	}
}
