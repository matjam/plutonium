package refsolve

import (
	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/solve"
	"github.com/matjam/plutonium/internal/escape2/summary"
)

// OpCall: for each candidate c, with s its summary and β its input binding,
// apply each entry of s (rules.go, OpCall).
func (s *solver) ruleCall(in *eir.Instr) {
	c := in.Call
	s.initRegion(in)
	if ownDeferrable(in) && s.env.Deferred(s.f, in.Call) {
		s.ownDcall(in) // defer.go
		return
	}
	switch c.Kind {
	case eir.CallStatic:
		s.applyCandidate(in, c.Callee)
	case eir.CallContract:
		sm, ok := s.env.Contract(c.Contract)
		if !ok {
			panic("internal compiler error: escape2: no contract table entry for " + c.Contract)
		}
		s.bindLent = s.lentInputs(c, nil, false)
		s.applySummary(in, sm, s.binder(c, nil), eir.NoFunc)
		s.bindLent = nil
	case eir.CallFunc:
		if c.Target != 0 {
			s.funcCall(in) // funccall.go
			return
		}
		for _, id := range s.env.Candidates(s.f, c) {
			s.applyCandidate(in, id)
		}
	default: // CallIface, CallTypeParam: all candidates, by union
		for _, id := range s.env.Candidates(s.f, c) {
			s.applyCandidate(in, id)
		}
	}
}

// applyCandidate applies the summary of function id at the call of in: nil is
// the bottom summary, except for a bodyless function, whose default keeps
// every input.
//
// A callee the program does not have (eir.NoFunc) is bodyless, with as many
// inputs as arguments. An ID past Program.Funcs is a virtual function the
// environment defines by its summary alone (rules.go, OpCall).
func (s *solver) applyCandidate(in *eir.Instr, id eir.FuncID) {
	s.applyCandidateRec(in, id, nil)
}

// applyCandidateRec is applyCandidate with the record input of a CallFunc
// candidate bound to rec, when rec is not nil (funccall.go).
func (s *solver) applyCandidateRec(in *eir.Instr, id eir.FuncID, rec value) {
	funcs := s.env.Program().Funcs
	var cfOf *eir.Func
	if id != eir.NoFunc && int(id) < len(funcs) {
		cfOf = funcs[id]
	}
	s.bindLent = s.lentInputs(in.Call, cfOf, rec != nil)
	defer func() { s.bindLent = nil }()
	binder := s.binder
	if rec != nil {
		binder = func(c *eir.Call, cf *eir.Func) func(int) value { return s.recBinder(c, cf, rec) }
	}
	if id == eir.NoFunc {
		c := in.Call
		cf := &eir.Func{Inputs: make([]eir.Input, len(c.Args)), Results: len(c.Results), Bodyless: true}
		s.applySummary(in, bodylessSummary(cf), s.binder(c, nil), id) // a bodyless NoFunc has no record input
		return
	}
	if int(id) >= len(funcs) {
		sm := s.env.Summary(id)
		if sm == nil {
			panic("internal compiler error: escape2: a virtual candidate without a summary")
		}
		cf := &eir.Func{Inputs: make([]eir.Input, sm.Inputs)}
		s.applySummary(in, sm, binder(in.Call, cf), id)
		return
	}
	cf := funcs[id]
	sm := s.env.Summary(id)
	if sm == nil {
		if !cf.Bodyless {
			return
		}
		sm = bodylessSummary(cf)
	}
	s.applySummary(in, sm, binder(in.Call, cf), id)
}

// bodylessSummary is the default for a function without a body: Esc (i, Root,
// GE) and Res (i, Root, GE) for every input i, and Heap in every result.
func bodylessSummary(cf *eir.Func) *summary.Summary {
	sm := &summary.Summary{Inputs: len(cf.Inputs), Res: make([]summary.Result, cf.Results)}
	for i := range cf.Inputs {
		k := domain.MakeKey(i, domain.Root, domain.KeyFlags{GE: true})
		sm.Esc = append(sm.Esc, k.WithStrict()) // kept where no runtime check follows it
		for r := range sm.Res {
			sm.Res[r].From = append(sm.Res[r].From, summary.ResultKey{Key: k, Field: domain.Root})
		}
	}
	for r := range sm.Res {
		sm.Res[r].Heap = []domain.PathID{domain.Root}
	}
	return sm
}

// lentInputs reports, for a call c to candidate cf, which inputs are bound
// to registers OpLend defines (binder's binding; solve's keyNode looks at the
// node bound, which for these is the register's). A receiver bound to
// RecvAddr, a call's function value and a closure record are none.
func (s *solver) lentInputs(c *eir.Call, cf *eir.Func, rec bool) func(int) bool {
	if len(s.lentRegs) == 0 {
		return nil
	}
	recv := solve.RecvValue
	if cf != nil && c.Target == 0 {
		recv = solve.RecvChoice(s.env, s.env.Program(), c, cf.ID)
	}
	arg := func(i int) bool {
		if i == 0 && recv != solve.RecvValue {
			return false
		}
		return i >= 0 && i < len(c.Args) && s.lentRegs[c.Args[i]]
	}
	switch c.Kind {
	case eir.CallIface, eir.CallTypeParam:
		if c.Target != 0 {
			return func(i int) bool { return i > 0 && arg(i-1) }
		}
	case eir.CallFunc:
		if cf != nil && (rec || len(cf.Inputs) == len(c.Args)+1) {
			return func(i int) bool { return i < len(c.Args) && arg(i) }
		}
	}
	return arg
}

// binder is the input binding β of a call to candidate cf (nil for a contract
// entry). CallStatic and CallContract bind input i to Args[i]. CallIface and
// CallTypeParam bind input 0 to Target and input i+1 to Args[i]; when Target
// is 0 the receiver is Args[0] and inputs bind as for a static call.
// CallFunc binds input i to Args[i] and, when the candidate has one input
// more than Args, the last input to Target. An input with no argument binds
// to the empty value.
func (s *solver) binder(c *eir.Call, cf *eir.Func) func(int) value {
	recv := solve.RecvValue
	if cf != nil && c.Target == 0 {
		recv = solve.RecvChoice(s.env, s.env.Program(), c, cf.ID)
	}
	arg := func(i int) value {
		if i == 0 && recv != solve.RecvValue {
			v := value{}
			v.addAll(s.val(c.RecvAddr))
			if recv == solve.RecvBoth && len(c.Args) > 0 {
				v.addAll(s.val(c.Args[0]))
			}
			return v
		}
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
					v := s.val(c.Target)
					if fs := s.env.Program().Funcs; c.Kind == eir.CallIface && cf != nil && int(cf.ID) < len(fs) && fs[cf.ID] == cf {
						// Only a receiver of its type reaches the
						// candidate (D51, solve.RecvFilter).
						if id := solve.RecvFilterID(s.ps.Filters(), s.env.Program(), cf.ID, true); id != 0 {
							v = s.filter(v, id)
						}
					}
					return v
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

// fieldPath is a summary's result field as a tag: a path with a load step is
// no field path, and is taken as Root, anywhere (rules.go, OpCall).
func (s *solver) fieldPath(f domain.PathID, ge bool) (domain.PathID, bool) {
	if s.ps.Loads(f) > 0 {
		return domain.Root, true
	}
	return f, ge
}

// applySummary applies every entry of sm at the call of in.
func (s *solver) applySummary(in *eir.Instr, sm *summary.Summary, bind func(int) value, callee eir.FuncID) {
	s.applySummarySkip(in, sm, bind, callee, -1)
}

// applySummarySkip is applySummary less the entries that name input skip
// alone, when skip >= 0 (solve/call.go, applySkip).
func (s *solver) applySummarySkip(in *eir.Instr, sm *summary.Summary, bind func(int) value, callee eir.FuncID, skip int) {
	c := in.Call
	ctx := func(entry int) callCtx {
		return callCtx{isCall: true, site: c.Site, callee: callee, entry: entry}
	}
	entry := 0
	for _, k := range sm.Esc { // Esc k: sink(val(k, β), E3003)
		if k.Input() == skip {
			entry++
			continue
		}
		v := s.valueOfKey(k, bind)
		if c.Implicit == eir.ImplTask {
			// A task starter: a local sync.WaitGroup is E3004 (12.12).
			wg, other := value{}, value{}
			for x := range v {
				if x.t.kind == kObj && s.objs[x.t.id].wg {
					wg.add(x)
				} else {
					other.add(x)
				}
			}
			s.sink(wg, diag.GoFrameView, 0, in.Pos, ctx(entry))
			v = other
		}
		task := entry < len(sm.Reasons) && len(sm.Reasons) == len(sm.Esc)+len(sm.Store) && sm.Reasons[entry].Sink == uint8(eir.SinkTask)
		if k.Strict() || c.Implicit == eir.ImplTask || task {
			s.sink(v, diag.PassFrameView, 0, in.Pos, ctx(entry))
		} else {
			// Kept in heap memory or by a callee: section 18.9's runtime
			// checks follow an arena view there (solve's cHeap).
			s.heapSink(v, diag.PassFrameView, in.Pos, ctx(entry))
		}
		entry++
	}
	for _, p := range sm.Store { // Store (a, b): store(val(b, β), val(a, β), call)
		if p.Src.Input() == skip && p.Dst.Input() == skip {
			entry++
			continue
		}
		v := s.valueOfKey(p.Src, bind)
		if p.Dst.FieldsGE() {
			v = anywhere(v)
		}
		s.store(s.valueOfDst(p.Dst, bind), v, in.Pos, ctx(entry))
		entry++
	}
	heap := single(ptr(tok{kind: kHeap}))
	for _, k := range sm.HeapStore { // HeapStore k: store(val(k, β), <Heap>, call)
		if k.Input() == skip {
			continue
		}
		v := heap
		if k.FieldsGE() {
			v = anywhere(v)
		}
		s.store(s.valueOfDst(k, bind), v, in.Pos, ctx(-1))
	}
	for r, res := range sm.Res {
		if r >= len(c.Results) || c.Results[r] == 0 {
			continue
		}
		dst := c.Results[r]
		for _, rk := range res.From { // Results[r] ⊇ pre(val(k, β), F)
			if rk.Key.Input() == skip {
				continue
			}
			f, ge := s.fieldPath(rk.Field, rk.FieldGE)
			s.def(dst, s.pre(s.valueOfKey(rk.Key, bind), f, ge))
		}
		for _, f := range res.Heap { // Results[r] ⊇ <Heap, F, true>
			f, _ = s.fieldPath(f, true)
			s.def(dst, single(tv{t: tok{kind: kHeap}, tag: f, g: true}))
		}
		for _, f := range res.CountedHeap { // Results[r] ⊇ <CHeap, F, true>
			f, _ = s.fieldPath(f, true)
			s.def(dst, single(tv{t: tok{kind: kCHeap}, tag: f, g: true}))
		}
	}
	s.deferredOf(in, sm, bind, skip)           // defer.go
	for _, r := range sm.Region.OuterResults { // Results[r] ⊇ <Obj(Reg(site), Root, true), Root, true>
		if r < len(c.Results) && c.Results[r] != 0 && s.f.Key.Init != nil {
			// A package's initialization: the region is static memory.
			held := value{}
			for _, a := range append(append([]eir.Reg{}, c.Args...), c.Target) {
				if a != 0 {
					held.addAll(s.reach(s.val(a)))
				}
			}
			s.sink(held, diag.StoreFrameView, 0, in.Pos, own)
			s.def(c.Results[r], single(tv{t: tok{kind: kHeap}, tag: domain.Root, g: true}))
			continue
		}
		if r < len(c.Results) && c.Results[r] != 0 {
			s.noteRegionLits(s.regObj(c.Site), sm.Region.Lits)
			if sm.Region.Self && !s.regionSelf[s.regObj(c.Site)] {
				if s.regionSelf == nil {
					s.regionSelf = map[int]bool{}
				}
				s.regionSelf[s.regObj(c.Site)] = true
				s.changed = true
			}
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
	if s.regionSelf[reg] {
		// The region's objects may point to one another (Region.Self).
		init.add(tv{t: tok{kind: kObj, id: reg, ge: true}, tag: domain.Root, g: true})
	}
	init.add(tv{t: tok{kind: kHeap}, tag: domain.Root, g: true})
	s.putPlace(place{reg, domain.Root, true}, init)
}
