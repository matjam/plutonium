package solve

import (
	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/summary"
)

// call applies the summaries of a call's candidates (rules.go, OpCall).
// Dynamic calls take the union over every candidate Env.Candidates gives:
// how candidates are chosen, and any rule for dynamic calls, is the
// environment's business, so that such a rule plugs in without changes here.
func (s *solver) call(b eir.BlockID, in *eir.Instr) {
	c := in.Call
	if c == nil {
		panic("internal compiler error: escape2/solve: OpCall without a call")
	}
	switch c.Kind {
	case eir.CallStatic:
		s.applyCandidate(in, c.Callee)
	case eir.CallContract:
		sum, ok := s.env.Contract(c.Contract)
		if !ok {
			panic("internal compiler error: escape2/solve: no contract entry for " + c.Contract)
		}
		s.apply(in, s.binding(c, len(c.Args), eir.NoFunc), sum, eir.NoFunc)
	case eir.CallIface, eir.CallTypeParam, eir.CallFunc:
		for _, cand := range s.env.Candidates(s.f, c) {
			s.applyCandidate(in, cand)
		}
	default:
		panic("internal compiler error: escape2/solve: unknown call kind")
	}
}

// applyCandidate applies the summary of one function the call may reach.
func (s *solver) applyCandidate(in *eir.Instr, cand eir.FuncID) {
	c := in.Call
	inputs := len(c.Args)
	bodyless := cand == eir.NoFunc
	var sum *summary.Summary
	switch {
	case bodyless:
	case int(cand) < len(s.prog.Funcs):
		fn := s.prog.Funcs[cand]
		inputs = len(fn.Inputs)
		bodyless = fn.Bodyless
		sum = s.env.Summary(cand)
	default:
		// A virtual function, defined by its summary alone (rules.go,
		// OpCall): a rule for dynamic calls plugs in this way.
		sum = s.env.Summary(cand)
		if sum == nil {
			panic("internal compiler error: escape2/solve: a virtual candidate without a summary")
		}
		inputs = sum.Inputs
	}
	bind := s.binding(c, inputs, cand)
	if sum == nil {
		if bodyless {
			s.applyDefault(in, bind, cand)
		}
		return // the bottom summary of a function of this SCC not yet solved
	}
	s.apply(in, bind, sum, cand)
}

// binding returns the node bound to each input of a candidate with n inputs
// (rules.go, "Input binding"); 0 for an input with no argument.
func (s *solver) binding(c *eir.Call, n int, cand eir.FuncID) []uint64 {
	bind := s.a.Alloc(n)
	args := c.Args
	k := 0
	switch c.Kind {
	case eir.CallIface, eir.CallTypeParam:
		if c.Target != 0 && n > 0 {
			bind[0] = uint64(c.Target)
			k = 1
		}
	}
	for _, a := range args {
		if k >= n {
			break
		}
		bind[k] = uint64(a)
		k++
	}
	if c.Kind == eir.CallFunc && n == len(args)+1 {
		bind[n-1] = uint64(c.Target)
	}
	return bind
}

// apply applies summary sum of callee cand at the call in.
func (s *solver) apply(in *eir.Instr, bind []uint64, sum *summary.Summary, cand eir.FuncID) {
	c := in.Call
	why := func(entry int) uint64 { return s.why(in.Pos, whyCall, 0, c.Site, cand, entry) }
	val := func(k domain.Key) (uint32, bool) {
		i := k.Input()
		if i >= len(bind) || bind[i] == 0 {
			return 0, false
		}
		return s.valNode(uint32(bind[i]), k.Path(), k.GE()), true
	}
	for e, k := range sum.Esc {
		if v, ok := val(k); ok {
			s.addCons(v, cSink, cE3003, why(e))
		}
	}
	for j, p := range sum.Store {
		dst, ok1 := val(p.Dst)
		src, ok2 := val(p.Src)
		if ok1 && ok2 {
			s.addCons(dst, cStore, uint64(src), why(len(sum.Esc)+j))
		}
	}
	for _, k := range sum.HeapStore {
		if dst, ok := val(k); ok {
			s.addCons(dst, cStore, uint64(s.heapNode), why(-1))
		}
	}
	for r, res := range sum.Res {
		if r >= len(c.Results) || c.Results[r] == 0 {
			continue
		}
		dst := uint32(c.Results[r])
		for _, rk := range res.From {
			if v, ok := val(rk.Key); ok {
				f, ge := s.fieldPath(rk.Field, rk.FieldGE)
				s.addEdge(v, dst, xw(xPre, f, ge), 0)
			}
		}
		// Heap at field F, anywhere under it: the summary does not say
		// how deep the callee's heap pointer sat.
		for _, f := range res.Heap {
			f, _ = s.fieldPath(f, true)
			s.addTok(dst, tok{kind: tHeap, tag: f, tagGE: true})
		}
		for _, f := range res.CountedHeap {
			f, _ = s.fieldPath(f, true)
			s.addTok(dst, tok{kind: tCHeap, tag: f, tagGE: true})
		}
	}
	for _, r := range sum.Region.OuterResults {
		if r >= len(c.Results) || c.Results[r] == 0 {
			continue
		}
		if s.f.Key.Init != nil {
			// A package's initialization gives the closures of its calls
			// static memory (section 15.2): what the callee may have put
			// there is kept there, and the closures are static.
			s.staticRegion(in)
			s.addTok(uint32(c.Results[r]), tok{kind: tHeap, tagGE: true})
			continue
		}
		reg := s.region(c)
		s.addTok(uint32(c.Results[r]), tok{kind: tObj, base: reg, ge: true, tagGE: true})
	}
}

// fieldPath is a summary's result field as a tag: a path with a load step
// is no field path, and is taken as Root, anywhere (rules.go, OpCall).
func (s *solver) fieldPath(f domain.PathID, ge bool) (domain.PathID, bool) {
	if s.ps.Loads(f) > 0 {
		return domain.Root, true
	}
	return f, ge
}

// applyDefault applies the default of a bodyless function without a
// summary: it keeps every input, and its results hold every input and heap
// memory (section 18.3, "Facts the compiler cannot see").
func (s *solver) applyDefault(in *eir.Instr, bind []uint64, cand eir.FuncID) {
	c := in.Call
	w := s.why(in.Pos, whyCall, 0, c.Site, cand, -1)
	results := c.Results
	if cand != eir.NoFunc {
		results = results[:min(len(results), s.prog.Funcs[cand].Results)]
	}
	for _, n := range bind {
		if n == 0 {
			continue
		}
		r := s.reachNode(uint32(n))
		s.addCons(r, cSink, cE3003, w)
		for _, res := range results {
			if res != 0 {
				s.addEdge(r, uint32(res), xw(xID, 0, false), 0)
			}
		}
	}
	for _, res := range results {
		if res != 0 {
			// Res Heap Root, applied as any summary's (rules.go, OpCall).
			s.addTok(uint32(res), tok{kind: tHeap, tagGE: true})
		}
	}
}

// staticRegion keeps what a call of a package's initialization may have put
// in the region of its closures, which is static memory: reach of its Args
// and Target, as a store into package memory (E3002 at the call).
func (s *solver) staticRegion(in *eir.Instr) {
	c := in.Call
	w := s.why(in.Pos, whyOwn, 0, 0, 0, 0)
	for _, a := range append(c.Args[:len(c.Args):len(c.Args)], c.Target) {
		if a != 0 {
			s.addCons(s.reachNode(uint32(a)), cSink, cE3002, w)
		}
	}
}

// region returns the object of the region the function provides at call c,
// seeding it with what the callee may have put there (rules.go, Reg(s)).
func (s *solver) region(c *eir.Call) uint32 {
	o := uint32(len(s.f.Objects)) + uint32(c.Site)
	pn := s.place(o, domain.Root, true)
	s.addTok(pn, tok{kind: tObj, base: o, ge: true, tagGE: true})
	s.addTok(pn, tok{kind: tHeap, tagGE: true})
	seed := func(r eir.Reg) {
		if r != 0 {
			s.addEdge(s.reachNode(uint32(r)), pn, xw(xID, 0, false), 0)
		}
	}
	for _, a := range c.Args {
		seed(a)
	}
	seed(c.Target)
	return o
}
