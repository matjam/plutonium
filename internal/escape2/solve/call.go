package solve

import (
	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/summary"
	"github.com/matjam/plutonium/internal/types2"
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
	case eir.CallFunc:
		if c.Target != 0 {
			s.funcCall(in) // funccall.go
			return
		}
		for _, cand := range s.env.Candidates(s.f, c) {
			s.applyCandidate(in, cand)
		}
	case eir.CallIface, eir.CallTypeParam:
		for _, cand := range s.env.Candidates(s.f, c) {
			s.applyCandidate(in, cand)
		}
	default:
		panic("internal compiler error: escape2/solve: unknown call kind")
	}
}

// applyCandidate applies the summary of one function the call may reach.
func (s *solver) applyCandidate(in *eir.Instr, cand eir.FuncID) {
	s.applyCandidateRec(in, cand, 0)
}

// applyCandidateRec is applyCandidate with the record input of a CallFunc
// candidate (one input more than the arguments) bound to node rec, when
// rec is not 0 (funccall.go).
func (s *solver) applyCandidateRec(in *eir.Instr, cand eir.FuncID, rec uint32) {
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
	if rec != 0 && c.Kind == eir.CallFunc && inputs == len(c.Args)+1 {
		bind[inputs-1] = uint64(rec)
	}
	if !bodyless {
		// A resumed solve applies what this summary gains (State.Resume).
		s.applied = append(s.applied, application{in: in, cand: cand, bind: bind, sum: sum, skip: -1})
	}
	if sum == nil {
		if bodyless {
			s.applyDefault(in, bind, cand)
		}
		return // the bottom summary of a function of this SCC not yet solved
	}
	s.apply(in, bind, sum, cand)
}

// application is a summary a call applied, for State.Resume.
type application struct {
	in   *eir.Instr
	cand eir.FuncID
	bind []uint64
	sum  *summary.Summary
	skip int // applySkip's skipped input, or -1
}

// reapply applies again the summaries of callees that changed since they
// were applied. Each new summary covers the old one (the driver joins an
// iterate with the previous one), so applying it in full adds what it
// gains: the constraints and edges the old one made are already there, and
// are not made twice (whys are interned).
func (s *solver) reapply() {
	for i := range s.applied {
		ap := &s.applied[i]
		sum := s.env.Summary(ap.cand)
		if sum == ap.sum || sum == nil {
			continue
		}
		ap.sum = sum
		s.applySkip(ap.in, ap.bind, sum, ap.cand, ap.skip)
	}
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
	for i, r := range bind {
		bind[i] = uint64(s.canonBase(eir.Reg(r)))
	}
	if n > 0 && c.Target == 0 && len(args) > 0 {
		switch RecvChoice(s.env, s.prog, c, cand) {
		case RecvAddr:
			bind[0] = uint64(s.canonBase(c.RecvAddr))
		case RecvBoth:
			bind[0] = uint64(s.joinNode(s.canonBase(c.RecvAddr), uint32(bind[0])))
		}
	}
	return bind
}

// joinNode returns a node holding what nodes a and b hold.
func (s *solver) joinNode(a, b uint32) uint32 {
	if n, ok := s.derived.get(dJoin|uint64(a)<<20, uint64(b)); ok {
		return uint32(n)
	}
	n := s.newNode(nkPinned)
	s.derived.put(s.a, dJoin|uint64(a)<<20, uint64(b), uint64(n))
	s.addEdge(a, n, xw(xID, 0, false), 0)
	s.addEdge(b, n, xw(xID, 0, false), 0)
	return n
}

// The receivers a CallTypeParam candidate may be given (eir.Call.RecvAddr).
const (
	RecvValue = iota // Args[0]
	RecvAddr         // the operand's address
	RecvBoth         // either, by instance
)

// RecvChoice is the receiver a candidate of a call takes: RecvAddr when the
// candidate's method has a pointer receiver and the type parameter's
// instances are not pointers (the call takes &operand), Args[0] when they
// are pointers or the method has a value receiver, and both when the
// instances are unknown or mixed (rules.go, "Input binding").
func RecvChoice(env Env, p *eir.Program, c *eir.Call, cand eir.FuncID) int {
	if c.Kind != eir.CallTypeParam || c.RecvAddr == 0 || cand == eir.NoFunc || int(cand) >= len(p.Funcs) {
		return RecvValue
	}
	fn := p.Funcs[cand]
	if len(fn.Inputs) == 0 || fn.Inputs[0].Type == nil {
		return RecvBoth
	}
	if _, ptr := fn.Inputs[0].Type.Underlying().(*types2.Pointer); !ptr {
		return RecvValue
	}
	tp, ok := types2.Unalias(c.Iface).(*types2.TypeParam)
	if !ok {
		return RecvBoth
	}
	insts, known := env.Instantiated(tp)
	if !known {
		return RecvBoth
	}
	pointers, values := false, false
	for _, T := range insts {
		if _, ptr := T.Underlying().(*types2.Pointer); ptr {
			pointers = true
		} else {
			values = true
		}
	}
	switch {
	case pointers && values:
		return RecvBoth
	case values:
		return RecvAddr
	}
	return RecvValue
}

// canonBase is the node a call binds to argument register r. A register
// defined by OpAddr, OpHeap or OpInput holds one token for good (SSA, no
// edges into it), so every such register of the same token shares one node,
// and the nodes a call derives from its argument (the values of the
// callee's keys) are shared by every call that passes the same pointer.
func (s *solver) canonBase(r eir.Reg) uint32 {
	if r == 0 || int(r) >= len(s.defs.s) {
		return uint32(r)
	}
	d := s.defs.s[r]
	if d == 0 {
		return uint32(r)
	}
	in := &s.f.Blocks[d>>32].Instrs[uint32(d)-1]
	switch in.Op {
	case eir.OpAddr, eir.OpHeap, eir.OpInput:
		if ids := s.pts(s.find(uint32(r))); len(ids) == 1 {
			return s.tokNode(s.tokAt(ids[0]))
		}
	}
	return uint32(r)
}

// apply applies summary sum of callee cand at the call in.
func (s *solver) apply(in *eir.Instr, bind []uint64, sum *summary.Summary, cand eir.FuncID) {
	s.applySkip(in, bind, sum, cand, -1)
}

// applySkip is apply, less the entries that name input skip alone (when
// skip >= 0): a deferred call's caller applies those (defer.go, fallback).
func (s *solver) applySkip(in *eir.Instr, bind []uint64, sum *summary.Summary, cand eir.FuncID, skip int) {
	c := in.Call
	why := func(entry int) uint64 { return s.why(in.Pos, whyCall, 0, c.Site, cand, entry) }
	val := func(k domain.Key) (uint32, bool) {
		i := k.Input()
		if i >= len(bind) || bind[i] == 0 {
			return 0, false
		}
		return s.keyNode(uint32(bind[i]), k), true
	}
	// A FieldsGE destination: the places its path's pointers point at, a
	// value stored anywhere under them by offsets (retagNode).
	dstVal := func(k domain.Key) (uint32, bool) {
		if !k.FieldsGE() {
			return val(k)
		}
		i := k.Input()
		if i >= len(bind) || bind[i] == 0 {
			return 0, false
		}
		return s.valNode(uint32(bind[i]), k.Path(), false), true
	}
	for e, k := range sum.Esc {
		if k.Input() == skip {
			continue
		}
		if v, ok := val(k); ok {
			// A key kept by starting a task, here or in the callee (its
			// reason says so): a local sync.WaitGroup is E3004 (12.12).
			code, w := uint64(cE3003), why(e)
			if c.Implicit == eir.ImplTask || (e < len(sum.Reasons) && len(sum.Reasons) == len(sum.Esc)+len(sum.Store) && sum.Reasons[e].Sink == uint8(eir.SinkTask)) {
				code, w = cTask, s.why(in.Pos, whyCall, eir.SinkTask, c.Site, cand, e)
			}
			s.addCons(v, cSink, code, w)
		}
	}
	for j, p := range sum.Store {
		if p.Src.Input() == skip && p.Dst.Input() == skip {
			continue
		}
		dst, ok1 := dstVal(p.Dst)
		src, ok2 := val(p.Src)
		if ok1 && ok2 {
			if p.Dst.FieldsGE() {
				src = s.retagNode(src)
			}
			s.addCons(dst, cStore, uint64(src), why(len(sum.Esc)+j))
		}
	}
	for _, k := range sum.HeapStore {
		if k.Input() == skip {
			continue
		}
		if dst, ok := dstVal(k); ok {
			src := s.heapNode
			if k.FieldsGE() {
				src = s.retagNode(src)
			}
			s.addCons(dst, cStore, uint64(src), why(-1))
		}
	}
	for r, res := range sum.Res {
		if r >= len(c.Results) || c.Results[r] == 0 {
			continue
		}
		dst := uint32(c.Results[r])
		for _, rk := range res.From {
			if rk.Key.Input() == skip {
				continue
			}
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
	s.applyDeferred(in, bind, sum, skip) // defer.go
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

// retagNode is node n with every tag made (Root, GE): stored through a
// FieldsGE destination key, a value lands anywhere under the place the
// key's pointers point at, by field and element steps (rules.go, OpCall).
func (s *solver) retagNode(n uint32) uint32 {
	if m, ok := s.derived.get(dRetag|uint64(n), 0); ok {
		return uint32(m)
	}
	m := s.newNode(nkPinned)
	s.derived.put(s.a, dRetag|uint64(n), 0, uint64(m))
	s.addEdge(n, m, xw(xRetag, 0, false), 0)
	return m
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
