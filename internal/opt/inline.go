package opt

import (
	"github.com/matjam/plutonium/internal/ssa"
)

// Inlining replaces calls to small functions with a copy of their body.
//
// Functions are processed bottom-up through the call graph, so a callee has
// already had its own calls inlined and been simplified when its size is
// judged. Recursive functions (any function in a cycle of the call graph)
// are never inlined into their own cycle.

// inlineBudget is the largest callee, in counted values, inlined at an
// ordinary call site.
const inlineBudget = 80

// maxCallerSize stops inlining into a caller that has grown this large.
const maxCallerSize = 20000

// Inline runs the inliner over a program, then cleans up each function it
// changed.
func Inline(p *ssa.Program) {
	funcs := map[*ssa.Sym]*ssa.Func{}
	for _, f := range p.Funcs {
		funcs[f.Sym] = f
	}
	sccs := callGraphSCCs(p, funcs)
	inCycle := map[*ssa.Func]bool{}
	for _, scc := range sccs {
		if len(scc) > 1 {
			for _, f := range scc {
				inCycle[f] = true
			}
		} else if callsSelf(scc[0]) {
			inCycle[scc[0]] = true
		}
	}
	sizes := map[*ssa.Func]int{}
	for _, scc := range sccs { // callees first
		for _, f := range scc {
			if inlineInto(f, funcs, sizes, inCycle, scc) {
				cleanup(f)
			}
			sizes[f] = funcSize(f)
		}
	}
}

// cleanup runs the simplifications that make an inlined body fit in.
func cleanup(f *ssa.Func) {
	ssa.CopyElim(f)
	if f.Pragmas&ssa.PragmaGenCheck != 0 {
		// Generation checks on what are now frame addresses fold away;
		// without that, their address arithmetic keeps SROA from
		// splitting the frame slots of inlined struct arguments.
		Simplify(f)
	}
	SimplifyCFG(f)
	ssa.Schedule(f)
	SROA(f)
	Mem2Reg(f)
	Simplify(f)
	SimplifyCFG(f)
	CSE(f)
	ssa.DeadCode(f)
}

func callsSelf(f *ssa.Func) bool {
	for _, b := range f.Blocks {
		for _, v := range b.Values {
			if v.Op == ssa.OpCall && v.Aux.(*ssa.Sym) == f.Sym {
				return true
			}
		}
	}
	return false
}

// funcSize counts the values that become instructions.
func funcSize(f *ssa.Func) int {
	n := 0
	for _, b := range f.Blocks {
		for _, v := range b.Values {
			switch v.Op {
			case ssa.OpConst, ssa.OpConstFloat, ssa.OpPhi, ssa.OpCopy, ssa.OpArg, ssa.OpInitMem,
				ssa.OpSlotAddr, ssa.OpSymAddr, ssa.OpOffPtr, ssa.OpSelectN, ssa.OpVarDef, ssa.OpVolatileRead:
				continue
			case ssa.OpCall, ssa.OpCallInd:
				n += 5
			}
			n++
		}
		n++ // the branch
	}
	return n
}

// inlineInto inlines eligible calls in f, reporting whether it changed f.
func inlineInto(f *ssa.Func, funcs map[*ssa.Sym]*ssa.Func, sizes map[*ssa.Func]int, inCycle map[*ssa.Func]bool, scc []*ssa.Func) bool {
	changed := false
	sameSCC := map[*ssa.Func]bool{}
	for _, g := range scc {
		sameSCC[g] = true
	}
	for rounds := 0; rounds < 4; rounds++ {
		var sites []*ssa.Value
		doomed := ssa.DoomedBlocks(f)
		for _, b := range f.Blocks {
			if doomed[b.ID] {
				// A panic path: inlining here only grows the code.
				continue
			}
			for _, v := range b.Values {
				if v.Op != ssa.OpCall {
					continue
				}
				g := funcs[v.Aux.(*ssa.Sym)]
				if g == nil || g == f || sameSCC[g] && inCycle[g] {
					continue
				}
				if f.NoSSE && !g.NoSSE {
					// g may use the FPU (//pu:fpu); its body must not
					// end up in a function that has no FPU state.
					continue
				}
				if v.Call.ABI != g.ABI {
					// A C call to a Plutonium function: its arguments
					// are laid out for a different convention.
					continue
				}
				if !shouldInline(g, sizes[g]) {
					continue
				}
				sites = append(sites, v)
			}
		}
		if len(sites) == 0 {
			break
		}
		for _, v := range sites {
			if funcSize(f) > maxCallerSize {
				return changed
			}
			g := funcs[v.Aux.(*ssa.Sym)]
			inlineCall(f, v, g)
			changed = true
			if checkSSA {
				if err := verify(f); err != nil {
					panic("after inlining " + g.Name + " into " + f.Name + ": " + err.Error())
				}
			}
		}
	}
	return changed
}

// returns reports whether g has a return block.
func returns(g *ssa.Func) bool {
	for _, b := range g.Blocks {
		if b.Kind == ssa.BlockRet {
			return true
		}
	}
	return false
}

func shouldInline(g *ssa.Func, size int) bool {
	switch {
	case g.Pragmas&ssa.PragmaNoInline != 0, g.Pragmas&ssa.PragmaInterrupt != 0:
		return false
	case !returns(g):
		// Never returns (panics, exits): keep the call out of line.
		return false
	case g.Pragmas&ssa.PragmaInline != 0:
		return true
	case len(g.Exports) > 0:
		// Exported functions are still inlinable; their symbol stays too.
	}
	return size <= inlineBudget
}

// inlineCall replaces call v in f with a copy of g's body.
func inlineCall(f *ssa.Func, v *ssa.Value, g *ssa.Func) {
	b := v.Block

	// Split b after v: everything after the call, and b's ending, move to
	// a new block that the inlined returns jump to.
	after := f.NewBlock(b.Kind)
	idx := 0
	for idx < len(b.Values) && b.Values[idx] != v {
		idx++
	}
	rest := b.Values[idx+1:]
	b.Values = b.Values[:idx+1]
	for _, w := range rest {
		w.Block = after
	}
	after.Values = append(after.Values, rest...)
	after.Likely = b.Likely
	after.CopyPos(b)
	after.SetControl(b.Controls...)
	b.SetControl()
	for len(b.Succs) > 0 {
		e := b.Succs[0]
		s := e.B
		after.Succs = append(after.Succs, ssa.Edge{B: s, I: e.I})
		s.Preds[e.I] = ssa.Edge{B: after, I: len(after.Succs) - 1}
		b.Succs = b.Succs[1:]
	}

	// Copy g's slots.
	slotMap := map[*ssa.Slot]*ssa.Slot{}
	for _, s := range g.Slots {
		ns := f.NewSlot(s.Name, s.Size, s.Align)
		ns.Leaves = s.Leaves
		ns.Pads = s.Pads
		slotMap[s] = ns
	}

	// Copy g's blocks and values.
	inl := f.AddInline(v, g)
	blockMap := map[*ssa.Block]*ssa.Block{}
	for _, gb := range g.Blocks {
		nb := f.NewBlock(gb.Kind)
		nb.Likely = gb.Likely
		nb.Pos, nb.Inl = gb.Pos, inl.Map(gb.Inl)
		blockMap[gb] = nb
	}
	valMap := map[*ssa.Value]*ssa.Value{}
	args := v.Args[:len(v.Args)-1]
	mem := v.Args[len(v.Args)-1]
	for _, gb := range g.Blocks {
		nb := blockMap[gb]
		for _, gv := range gb.Values {
			switch gv.Op {
			case ssa.OpArg:
				valMap[gv] = args[gv.AuxInt]
				continue
			case ssa.OpInitMem:
				valMap[gv] = mem
				continue
			}
			op := gv.Op
			if op == ssa.OpReturnAddr {
				// g's return address would have pointed just past
				// this call, which is where g's code now is.
				op = ssa.OpCodeAddr
			}
			nv := nb.NewValue(op, gv.Type)
			nv.AuxInt = gv.AuxInt
			nv.Aux = gv.Aux
			if s, ok := gv.Aux.(*ssa.Slot); ok {
				nv.Aux = slotMap[s]
			}
			if gv.Call != nil {
				c := *gv.Call
				nv.Call = &c
			}
			nv.Pos, nv.Inl = gv.Pos, inl.Map(gv.Inl)
			if op == ssa.OpCodeAddr && gv.Op == ssa.OpReturnAddr {
				// It stands for a return address, which names the call:
				// give it the call's position, so that the address minus
				// one symbolises to the line of the call in f rather than
				// to a line of g.
				nv.CopyPos(v)
			}
			valMap[gv] = nv
		}
	}
	lookup := func(gv *ssa.Value) *ssa.Value {
		if nv, ok := valMap[gv]; ok {
			return nv
		}
		panic("inline: value " + gv.String() + " of " + g.Name + " has no copy")
	}
	for _, gb := range g.Blocks {
		for _, gv := range gb.Values {
			nv := valMap[gv]
			if gv.Op == ssa.OpArg || gv.Op == ssa.OpInitMem {
				continue
			}
			for _, a := range gv.Args {
				nv.AddArg(lookup(a))
			}
		}
	}

	// Wire up control flow. Returns jump to after; their memory and results
	// merge there through phis when there are several.
	// Copy every edge with the same indices as in g, so that each copied
	// phi's arguments stay in the order of its block's predecessors.
	var rets []*ssa.Block
	for _, gb := range g.Blocks {
		nb := blockMap[gb]
		for _, e := range gb.Succs {
			nb.Succs = append(nb.Succs, ssa.Edge{B: blockMap[e.B], I: e.I})
		}
		for _, e := range gb.Preds {
			nb.Preds = append(nb.Preds, ssa.Edge{B: blockMap[e.B], I: e.I})
		}
		switch gb.Kind {
		case ssa.BlockRet:
			nb.Kind = ssa.BlockPlain
			rets = append(rets, gb)
		default:
			var cs []*ssa.Value
			for _, c := range gb.Controls {
				cs = append(cs, lookup(c))
			}
			nb.SetControl(cs...)
		}
	}

	// b jumps into the copy of g's entry.
	b.Kind = ssa.BlockPlain
	b.AddEdgeTo(blockMap[g.Entry])

	// Results.
	nres := len(g.Results)
	var outMem *ssa.Value
	outs := make([]*ssa.Value, nres)
	switch len(rets) {
	case 0:
		// g never returns: after is unreachable.
	case 1:
		rb := blockMap[rets[0]]
		rb.AddEdgeTo(after)
		outMem = lookup(rets[0].Controls[0])
		for i := 0; i < nres; i++ {
			outs[i] = lookup(rets[0].Controls[1+i])
		}
	default:
		outMem = after.NewValue(ssa.OpPhi, ssa.TMem)
		for i := 0; i < nres; i++ {
			outs[i] = after.NewValue(ssa.OpPhi, g.Results[i])
		}
		// Move the new phis to the front of after.
		n := 1 + nres
		vs := after.Values
		phis := append([]*ssa.Value(nil), vs[len(vs)-n:]...)
		copy(vs[n:], vs[:len(vs)-n])
		copy(vs, phis)
		for _, gr := range rets {
			rb := blockMap[gr]
			rb.AddEdgeTo(after)
			outMem.AddArg(lookup(gr.Controls[0]))
			for i := 0; i < nres; i++ {
				outs[i].AddArg(lookup(gr.Controls[1+i]))
			}
		}
	}

	// Replace the call's results.
	for _, ab := range f.Blocks {
		for _, w := range ab.Values {
			if w.Op == ssa.OpSelectN && len(w.Args) == 1 && w.Args[0] == v {
				if outMem == nil {
					w.Reset(ssa.OpUnreachableValue)
					continue
				}
				if w.AuxInt == 0 {
					w.CopyOf(outMem)
				} else {
					w.CopyOf(outs[w.AuxInt-1])
				}
			}
		}
	}
	// The call itself disappears.
	v.Reset(ssa.OpUnreachableValue)
	v.Type = ssa.TVoid
	f.InvalidateCFG()
	f.RemoveDeadBlocks()
}

// callGraphSCCs returns the strongly connected components of the call
// graph in reverse topological order (callees before callers), by Tarjan's
// algorithm.
func callGraphSCCs(p *ssa.Program, funcs map[*ssa.Sym]*ssa.Func) [][]*ssa.Func {
	index := map[*ssa.Func]int{}
	low := map[*ssa.Func]int{}
	onStack := map[*ssa.Func]bool{}
	var stack []*ssa.Func
	var out [][]*ssa.Func
	next := 0
	var strong func(f *ssa.Func)
	strong = func(f *ssa.Func) {
		index[f] = next
		low[f] = next
		next++
		stack = append(stack, f)
		onStack[f] = true
		for _, b := range f.Blocks {
			for _, v := range b.Values {
				if v.Op != ssa.OpCall {
					continue
				}
				g := funcs[v.Aux.(*ssa.Sym)]
				if g == nil {
					continue
				}
				if _, seen := index[g]; !seen {
					strong(g)
					low[f] = min(low[f], low[g])
				} else if onStack[g] {
					low[f] = min(low[f], index[g])
				}
			}
		}
		if low[f] == index[f] {
			var scc []*ssa.Func
			for {
				g := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[g] = false
				scc = append(scc, g)
				if g == f {
					break
				}
			}
			out = append(out, scc)
		}
	}
	for _, f := range p.Funcs {
		if _, seen := index[f]; !seen {
			strong(f)
		}
	}
	return out
}
