package refsolve

import (
	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/summary"
)

// OpCall: for each candidate c, with s its summary and β its input binding,
// apply each entry of s (rules.go, OpCall).
func (s *solver) ruleCall(in *eir.Instr) {
	c := in.Call
	s.initRegion(in)
	switch c.Kind {
	case eir.CallStatic:
		if c.Callee != eir.NoFunc {
			s.applyCandidate(in, c.Callee)
		}
	case eir.CallContract:
		sm, ok := s.env.Contract(c.Contract)
		if !ok {
			panic("internal compiler error: escape2: no contract table entry for " + c.Contract)
		}
		s.applySummary(in, sm, s.binder(c, nil), eir.NoFunc)
	default: // CallIface, CallTypeParam, CallFunc: all candidates, by union
		for _, id := range s.env.Candidates(s.f, c) {
			s.applyCandidate(in, id)
		}
	}
}

// applyCandidate applies the summary of function id at the call of in: nil is
// the bottom summary, except for a bodyless function, whose default keeps
// every input.
func (s *solver) applyCandidate(in *eir.Instr, id eir.FuncID) {
	cf := s.env.Program().Funcs[id]
	sm := s.env.Summary(id)
	if sm == nil {
		if !cf.Bodyless {
			return
		}
		sm = bodylessSummary(cf)
	}
	s.applySummary(in, sm, s.binder(in.Call, cf), id)
}

// bodylessSummary is the default for a function without a body: Esc (i, Root,
// GE) and Res (i, Root, GE) for every input i, and Heap in every result.
func bodylessSummary(cf *eir.Func) *summary.Summary {
	sm := &summary.Summary{Inputs: len(cf.Inputs), Res: make([]summary.Result, cf.Results)}
	for i := range cf.Inputs {
		k := domain.MakeKey(i, domain.Root, domain.KeyFlags{GE: true})
		sm.Esc = append(sm.Esc, k)
		for r := range sm.Res {
			sm.Res[r].From = append(sm.Res[r].From, summary.ResultKey{Key: k, Field: domain.Root})
		}
	}
	for r := range sm.Res {
		sm.Res[r].Heap = []domain.PathID{domain.Root}
	}
	return sm
}

// binder is the input binding β of a call to candidate cf (nil for a contract
// entry). CallStatic and CallContract bind input i to Args[i]. CallIface and
// CallTypeParam bind input 0 to Target and input i+1 to Args[i]; when Target
// is 0 the receiver is Args[0] and inputs bind as for a static call.
// CallFunc binds input i to Args[i] and, when the candidate has one input
// more than Args, the last input to Target. An input with no argument binds
// to the empty value.
func (s *solver) binder(c *eir.Call, cf *eir.Func) func(int) value {
	arg := func(i int) value {
		if i >= 0 && i < len(c.Args) {
			return s.val(c.Args[i])
		}
		return value{}
	}
	switch c.Kind {
	case eir.CallIface, eir.CallTypeParam:
		if c.Target != 0 {
			return func(i int) value {
				if i == 0 {
					return s.val(c.Target)
				}
				return arg(i - 1)
			}
		}
	case eir.CallFunc:
		if cf != nil && len(cf.Inputs) == len(c.Args)+1 {
			return func(i int) value {
				if i == len(c.Args) {
					return s.val(c.Target)
				}
				return arg(i)
			}
		}
	}
	return arg
}

// applySummary applies every entry of sm at the call of in.
func (s *solver) applySummary(in *eir.Instr, sm *summary.Summary, bind func(int) value, callee eir.FuncID) {
	c := in.Call
	ctx := func(entry int) callCtx {
		return callCtx{isCall: true, site: c.Site, callee: callee, entry: entry}
	}
	entry := 0
	for _, k := range sm.Esc { // Esc k: sink(val(k, β), E3003)
		s.sink(s.valueOfKey(k, bind), diag.PassFrameView, 0, in.Pos, ctx(entry))
		entry++
	}
	for _, p := range sm.Store { // Store (a, b): store(val(b, β), val(a, β), call)
		s.store(s.valueOfKey(p.Dst, bind), s.valueOfKey(p.Src, bind), in.Pos, ctx(entry))
		entry++
	}
	heap := single(ptr(tok{kind: kHeap}))
	for _, k := range sm.HeapStore { // HeapStore k: store(val(k, β), <Heap>, call)
		s.store(s.valueOfKey(k, bind), heap, in.Pos, ctx(entry))
		entry++
	}
	for r, res := range sm.Res {
		if r >= len(c.Results) || c.Results[r] == 0 {
			continue
		}
		dst := c.Results[r]
		for _, rk := range res.From { // Results[r] ⊇ pre(val(k, β), F)
			s.def(dst, s.pre(s.valueOfKey(rk.Key, bind), rk.Field, false))
		}
		for _, f := range res.Heap { // Results[r] ⊇ <Heap, F, false>
			s.def(dst, single(tv{t: tok{kind: kHeap}, tag: f}))
		}
		for _, f := range res.CountedHeap { // Results[r] ⊇ <CHeap, F, false>
			s.def(dst, single(tv{t: tok{kind: kCHeap}, tag: f}))
		}
	}
	for _, r := range sm.Region.OuterResults { // Results[r] ⊇ <Obj(Reg(site), Root, true), Root, true>
		if r < len(c.Results) && c.Results[r] != 0 {
			t := tok{kind: kObj, id: s.regObj(c.Site), ge: true}
			s.def(c.Results[r], single(tv{t: t, tag: domain.Root, g: true}))
		}
	}
}

// initRegion gives the place <Reg(s), Root, true> what the callee may have
// stored there: <t, Root, true> for every t in reach(β(i)) over the call's
// inputs (its arguments and its target), Obj(Reg(s), Root, true) itself, and
// Heap.
func (s *solver) initRegion(in *eir.Instr) {
	c := in.Call
	reg := s.regObj(c.Site)
	init := value{}
	args := append([]eir.Reg{}, c.Args...)
	if c.Target != 0 {
		args = append(args, c.Target)
	}
	for _, a := range args {
		init.addAll(s.reach(s.val(a)))
	}
	init.add(tv{t: tok{kind: kObj, id: reg, ge: true}, tag: domain.Root, g: true})
	init.add(tv{t: tok{kind: kHeap}, tag: domain.Root, g: true})
	s.putPlace(place{reg, domain.Root, true}, init)
}
