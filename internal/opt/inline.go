package opt

import (
	"fmt"
	"sort"

	"github.com/matjam/plutonium/internal/scratch"
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

// singleUseBudget is the largest callee inlined at the only call site of a
// closure made in one place in the program, such as a loop body over a
// function: the copy replaces the original rather than adding to it.
const singleUseBudget = 600

// Each caller has a growth budget: inlining into it stops once it would
// grow past growthFactor times its size before inlining, or growthMin
// values for a small one, and never past maxCallerSize. This applies to
// //pu:inline callees and to calls through closures too. Without it, a
// function making many calls to inlinable functions grows without bound,
// and register allocation, which is superlinear in the size of a function,
// comes to dominate the compile. The values were chosen by measurement:
// none of the benchmarks reaches the budget, and an inline-heavy fuzzer
// program that grew functions from 1,000 to 20,000 values compiles in a
// tenth of the time.
//
// A caller's size before inlining counts the closures used once that it
// makes (the bodies of its loops over functions, say), since they are
// part of its source: inlining them moves code rather than copying it.
const (
	growthFactor  = 4
	growthMin     = 5000
	maxCallerSize = 20000
)

// callSize is what funcSize counts for a call, which inlining replaces.
const callSize = 6

// Inline runs the inliner over a program, then cleans up each function it
// changed.
func Inline(p *ssa.Program) {
	inline(p, nil)
}

// inline is Inline. If why is not nil, it records in it why each call to
// a //pu:inline function was left out of line, the last time the call was
// considered (inlinenotes.go).
func inline(p *ssa.Program, why map[*ssa.Value]string) {
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
	// The functions referred to once in the whole program (closures,
	// mostly, such as the bodies of loops over functions): inlining such
	// a function at its only call site does not duplicate it.
	// A function called statically is not counted as used once.
	refs := map[*ssa.Sym]int{}
	for _, f := range p.Funcs {
		for _, b := range f.Blocks {
			for _, v := range b.Values {
				switch v.Op {
				case ssa.OpFuncAddr:
					refs[v.Aux.(*ssa.Sym)]++
				case ssa.OpCall:
					refs[v.Aux.(*ssa.Sym)] += 2
				}
			}
		}
		if len(f.Exports) > 0 {
			refs[f.Sym] += 2
		}
	}
	sizes := map[*ssa.Func]int{}
	var step inlineStep
	step.funcs, step.sizes, step.inCycle, step.refs = funcs, sizes, inCycle, refs
	for _, scc := range sccs { // callees first
		for _, f := range scc {
			if why == nil {
				step.f, step.scc = f, scc
				scratch.Do(&step)
			} else if inlineInto(f, funcs, sizes, inCycle, scc, refs, why) {
				cleanup(f)
			}
			sizes[f] = funcSize(f)
		}
	}
	scratch.Trim()
}

// inlineStep inlines into one caller and cleans it up as a scratch step,
// then copies the caller out of scratch memory (ssa.Copier), so that the
// copies and cleanup leave behind only the function they made. The step
// only reads the tables it is given. Without why: the notes would hold on
// to values made in the step.
type inlineStep struct {
	f       *ssa.Func
	scc     []*ssa.Func
	funcs   map[*ssa.Sym]*ssa.Func
	sizes   map[*ssa.Func]int
	inCycle map[*ssa.Func]bool
	refs    map[*ssa.Sym]int
	cp      *ssa.Copier // made in the step's memory, for ScratchKeep
}

func (s *inlineStep) ScratchWork() {
	used := scratch.Used()
	if inlineInto(s.f, s.funcs, s.sizes, s.inCycle, s.scc, s.refs, nil) {
		cleanup(s.f)
	}
	// A caller with nothing to inline allocated nothing, and copying
	// would only duplicate it.
	if scratch.Used() != used {
		s.cp = ssa.NewCopier(s.f)
	}
}

func (s *inlineStep) ScratchKeep() {
	if s.cp != nil {
		s.cp.Copy(s.f)
		s.cp = nil
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
			case ssa.OpConst, ssa.OpConstFloat, ssa.OpPhi, ssa.OpCopy, ssa.OpRegion, ssa.OpArg, ssa.OpInitMem,
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

// sourceSize is f's size before inlining for its growth budget: its own
// size and that of each closure used once that it makes.
func sourceSize(f *ssa.Func, funcs map[*ssa.Sym]*ssa.Func, sizes map[*ssa.Func]int, refs map[*ssa.Sym]int) int {
	n := funcSize(f)
	for _, b := range f.Blocks {
		for _, v := range b.Values {
			if v.Op != ssa.OpFuncAddr || refs[v.Aux.(*ssa.Sym)] != 1 {
				continue
			}
			if g := funcs[v.Aux.(*ssa.Sym)]; g != nil && g != f {
				n += sizes[g]
			}
		}
	}
	return n
}

// inlineInto inlines eligible calls in f, reporting whether it changed f.
func inlineInto(f *ssa.Func, funcs map[*ssa.Sym]*ssa.Func, sizes map[*ssa.Func]int, inCycle map[*ssa.Func]bool, scc []*ssa.Func, refs map[*ssa.Sym]int, why map[*ssa.Value]string) bool {
	changed := false
	sameSCC := map[*ssa.Func]bool{}
	for _, g := range scc {
		sameSCC[g] = true
	}
	limit := min(maxCallerSize, max(growthMin, growthFactor*sourceSize(f, funcs, sizes, refs)))
	// Each round can expose more: a closure inlined in one round makes the
	// calls through the closures it was given direct in the next (a loop
	// over a function: first its iterator, then the loop body).
	for rounds := 0; rounds < maxInlineRounds; rounds++ {
		if SplitRegions(f) {
			changed = true
		}
		if ForwardClosureLoads(f) {
			changed = true
		}
		var candidates []*ssa.Value
		calls := map[*ssa.Func]int{}
		doomed := ssa.DoomedBlocks(f)
		for _, b := range f.Blocks {
			if doomed[b.ID] {
				// A panic path: inlining here only grows the code.
				noteBlock(why, b, funcs, "the call is on a path that panics")
				continue
			}
			for _, v := range b.Values {
				g := callTarget(v, funcs)
				if g == nil {
					continue
				}
				if g == f || sameSCC[g] && inCycle[g] {
					note(why, v, g, "the call is recursive")
					continue
				}
				if f.NoSSE && !g.NoSSE {
					// g may use the FPU (//pu:fpu); its body must not
					// end up in a function that has no FPU state.
					note(why, v, g, "it may use the FPU, and the caller may not")
					continue
				}
				if v.Call.ABI != g.ABI {
					// A C call to a Plutonium function: its arguments
					// are laid out for a different convention.
					note(why, v, g, "the call uses another calling convention")
					continue
				}
				candidates = append(candidates, v)
				calls[g]++
			}
		}
		var sites []*ssa.Value
		for _, v := range candidates {
			g := callTarget(v, funcs)
			budget := inlineBudget
			if refs[g.Sym] == 1 && calls[g] == 1 {
				budget = singleUseBudget
			}
			if shouldInline(g, sizes[g], budget) {
				sites = append(sites, v)
			} else {
				note(why, v, g, notInlinable(g))
			}
		}
		if len(sites) == 0 {
			break
		}
		// Smallest callees first, so that when the budget runs out the
		// calls left are the ones that would have cost the most to copy.
		// The size is kept up to date by adding each callee's size
		// rather than recounting, which would be quadratic.
		sort.SliceStable(sites, func(i, j int) bool {
			return sizes[callTarget(sites[i], funcs)] < sizes[callTarget(sites[j], funcs)]
		})
		size := funcSize(f)
		for _, v := range sites {
			g := callTarget(v, funcs)
			if size+sizes[g]-callSize > limit {
				if why != nil {
					note(why, v, g, fmt.Sprintf("%s would grow past its limit of %d values", f.Name, limit))
				}
				continue
			}
			inlineCall(f, v, g)
			size += sizes[g] - callSize
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

// callTarget returns the function a call calls, if it is known and
// compiled with the program: the target of a static call, or of a call
// through a closure whose code pointer is known.
func callTarget(v *ssa.Value, funcs map[*ssa.Sym]*ssa.Func) *ssa.Func {
	switch v.Op {
	case ssa.OpCall:
		return funcs[v.Aux.(*ssa.Sym)]
	case ssa.OpCallInd:
		if sym := closureCallee(v); sym != nil {
			return funcs[sym]
		}
	}
	return nil
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

func shouldInline(g *ssa.Func, size, budget int) bool {
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
	return size <= budget
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
		ns.Closure = s.Closure
		ns.Parts = s.Parts
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
	var ctx *ssa.Value
	if v.Op == ssa.OpCallInd {
		// A call through a closure: the code pointer, then the record.
		ctx = args[1]
		args = args[2:]
	}
	for _, gb := range g.Blocks {
		nb := blockMap[gb]
		for _, gv := range gb.Values {
			switch gv.Op {
			case ssa.OpArg:
				a := args[gv.AuxInt]
				if a.Op == ssa.OpRegion {
					// The region is no longer passed to a call: the
					// inlined body's accesses mark its parts.
					a = a.Args[0]
				}
				valMap[gv] = a
				continue
			case ssa.OpInitMem:
				valMap[gv] = mem
				continue
			case ssa.OpClosurePtr:
				if ctx != nil {
					valMap[gv] = ctx
					continue
				}
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
				// A function whose address f takes may be called
				// through a closure f makes: count it as a callee, so
				// that it is optimised before f.
				if v.Op != ssa.OpCall && v.Op != ssa.OpFuncAddr {
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
