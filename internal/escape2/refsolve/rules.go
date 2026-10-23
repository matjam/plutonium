package refsolve

import (
	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/solve"
	"github.com/matjam/plutonium/internal/escape2/summary"
	"github.com/matjam/plutonium/internal/types2"
	"slices"
)

// One function per line of the table "One rule per instruction" in
// solve/rules.go, named after the instruction. apply dispatches.

func (s *solver) apply(b *eir.Block, in *eir.Instr) {
	switch in.Op {
	case eir.OpAddr:
		s.ruleAddr(in)
	case eir.OpHeap:
		s.ruleHeap(in)
	case eir.OpInput:
		s.ruleInput(in)
	case eir.OpLoad:
		s.ruleLoad(in)
	case eir.OpStore:
		s.store(s.val(in.X), s.val(in.Y), in.Pos, own)
	case eir.OpField:
		s.ruleField(in)
	case eir.OpElem:
		s.ruleElem(in)
	case eir.OpSelect:
		s.ruleSelect(in)
	case eir.OpCompose:
		s.ruleCompose(in)
	case eir.OpCopy:
		s.def(in.Dst, s.val(in.X)) // OpCopy: Dst ⊇ X
	case eir.OpAssert:
		// Dst ⊇ X; to a direct type, without pointers to interface boxes.
		v := s.val(in.X)
		if solve.DirectAssert(in.Type) {
			w := value{}
			for x := range v {
				if x.t.kind == kObj && x.t.id < len(s.f.Objects) && s.f.Objects[x.t.id].Kind == eir.ObjBox {
					continue
				}
				w.add(x)
			}
			v = w
		} else {
			// To an interface: only the boxes whose type implements it.
			w := value{}
			for x := range v {
				if x.t.kind == kObj && x.t.id < len(s.f.Objects) && !solve.BoxAsserts(&s.f.Objects[x.t.id], in.Type) {
					continue
				}
				w.add(x)
			}
			v = w
		}
		s.def(in.Dst, v)
	case eir.OpPhi:
		s.rulePhi(b, in)
	case eir.OpConv:
		s.ruleConv(in)
	case eir.OpClosure:
		s.ruleClosure(in)
	case eir.OpMethodVal:
		s.ruleMethodVal(in)
	case eir.OpSink:
		s.ruleSink(in)
	case eir.OpDrop:
		s.ruleDrop(b, in)
	case eir.OpCall:
		s.ruleCall(in)
	}
}

// objPtr is the pointer value Obj(o, Root), or Heap for an object of
// LifeHeap (rules.go, "Abstract values").
func (s *solver) objPtr(o eir.ObjID) value { return s.objAt(o, domain.Root, false) }

// objAt is the pointer value Obj(o, p, ge), or Heap for an object of LifeHeap.
func (s *solver) objAt(o eir.ObjID, p domain.PathID, ge bool) value {
	if o == eir.NoObj || s.objs[o].life == eir.LifeHeap {
		return single(ptr(tok{kind: kHeap})) // NoObj: a static func value
	}
	return single(ptr(tok{kind: kObj, id: int(o), path: p, ge: ge}))
}

// OpAddr: Dst ⊇ <Obj(Obj, Path, cut), Root, false> (Heap if LifeHeap).
func (s *solver) ruleAddr(in *eir.Instr) {
	if s.objs[in.Obj].life == eir.LifeHeap {
		s.def(in.Dst, single(ptr(tok{kind: kHeap})))
		return
	}
	p, cut := s.pathOf(in.Path)
	s.def(in.Dst, single(ptr(tok{kind: kObj, id: int(in.Obj), path: p, ge: cut})))
}

// OpHeap: Dst ⊇ <Heap, Root, false>; <CHeap, ...> for HeapCounted and HeapCountedCurrent.
func (s *solver) ruleHeap(in *eir.Instr) {
	k := kHeap
	if in.Heap == eir.HeapCounted || in.Heap == eir.HeapCountedCurrent {
		k = kCHeap
	}
	s.def(in.Dst, single(ptr(tok{kind: k})))
}

// OpInput: Dst ⊇ <In(Index, Root, false), Root, false>.
func (s *solver) ruleInput(in *eir.Instr) {
	s.def(in.Dst, single(ptr(tok{kind: kIn, id: in.Index})))
}

// OpLoad: Dst ⊇ load(X), or <Heap> when the location read is a heap-only
// field.
func (s *solver) ruleLoad(in *eir.Instr) {
	if s.heapOnlyField(in.X) {
		s.def(in.Dst, single(ptr(tok{kind: kHeap})))
		return
	}
	s.def(in.Dst, s.load(s.val(in.X)))
}

// heapOnlyField says whether the location the pointer register x was made
// from is a field class Env.HeapOnly calls heap-only. The field class comes
// from x's definition: OpAddr by Objects[o].Type along Path, OpField by its
// Type along Path.
func (s *solver) heapOnlyField(x eir.Reg) bool {
	if x == 0 || s.defs[x] == nil {
		return false
	}
	d := s.defs[x]
	var t types2.Type
	switch d.Op {
	case eir.OpAddr:
		t = s.f.Objects[d.Obj].Type
	case eir.OpField:
		t = d.Type
	default:
		return false
	}
	n := len(d.Path)
	if n == 0 || d.Path[n-1] < 0 {
		return false
	}
	for _, st := range d.Path[:n-1] {
		if t = walkType(t, st); t == nil {
			return false
		}
	}
	if t == nil {
		return false
	}
	if _, ok := t.Underlying().(*types2.Struct); !ok {
		return false
	}
	return s.env.HeapOnly(t, d.Path[n-1])
}

// walkType is the type of one path step into t: a field or an element.
func walkType(t types2.Type, step int) types2.Type {
	if t == nil {
		return nil
	}
	switch u := t.Underlying().(type) {
	case *types2.Struct:
		if step >= 0 && step < u.NumFields() {
			return u.Field(step).Type()
		}
	case *types2.Array:
		if step == eir.ElemStep {
			return u.Elem()
		}
	case *types2.Slice:
		if step == eir.ElemStep {
			return u.Elem()
		}
	}
	return nil
}

// offsetAll is Dst ⊇ off(t, Q) for each token t of X.
func (s *solver) offsetAll(in *eir.Instr, q domain.PathID, qcut bool) {
	out := value{}
	for x := range s.val(in.X) {
		out.add(ptr(s.off(x.t, q, qcut)))
	}
	s.def(in.Dst, out)
}

// OpField: Dst ⊇ off(t, Path) for each token t of X.
func (s *solver) ruleField(in *eir.Instr) {
	if slices.Contains(in.Path, eir.AnyStep) {
		// An unknown offset: anywhere in the object.
		s.def(in.Dst, s.anyOffset(s.val(in.X)))
		return
	}
	q, c := s.pathOf(in.Path)
	s.offsetAll(in, q, c)
}

// OpElem: Dst ⊇ off(t, [ElemStep]) for each token t of X.
func (s *solver) ruleElem(in *eir.Instr) {
	q, c := s.ps.Extend(domain.Root, domain.StepElem)
	s.offsetAll(in, q, c)
}

// OpSelect: Dst ⊇ sel(X, Path).
func (s *solver) ruleSelect(in *eir.Instr) {
	q, c := s.pathOf(in.Path)
	s.def(in.Dst, s.sel(s.val(in.X), q, c))
}

// OpCompose: Dst ⊇ pre(Args[i], Fields[i]) for each i.
func (s *solver) ruleCompose(in *eir.Instr) {
	for i, a := range in.Args {
		q, c := s.pathOf(in.Fields[i])
		s.def(in.Dst, s.pre(s.val(a), q, c))
	}
}

// OpPhi: Dst ⊇ Args[k] for each k; and the back-edge check.
func (s *solver) rulePhi(b *eir.Block, in *eir.Instr) {
	for _, a := range in.Args {
		s.def(in.Dst, s.val(a))
	}
	s.backEdge(b, in)
}

// Back-edge check (section 18.4, "checked where the next iteration starts"):
// for an OpPhi in the header of loop L, for each Args[k] whose predecessor is
// a block of L, every Obj(o2, ...) in it with o2 of LifeIter whose loop is L
// or nested in L (a region Reg(s) of a call in L included), or of LifeStmt,
// is a finding E3006 (E3101 for a region, E3009 for LifeStmt) at the phi.
func (s *solver) backEdge(b *eir.Block, in *eir.Instr) {
	for l, loop := range s.f.Loops {
		if loop.Header != b.ID {
			continue
		}
		for k, a := range in.Args {
			if k >= len(b.Preds) || !inLoop(loop, b.Preds[k]) {
				continue
			}
			for x := range s.val(a) {
				if x.t.kind != kObj {
					continue
				}
				o := s.objs[x.t.id]
				if (o.life == eir.LifeIter && s.nestedIn(o.loop, l)) || o.life == eir.LifeStmt {
					s.finding(s.storeCode(x.t.id), in.Pos, x.t.id, 0, own)
				}
			}
		}
	}
}

func inLoop(l eir.Loop, b eir.BlockID) bool {
	for _, x := range l.Blocks {
		if x == b {
			return true
		}
	}
	return false
}

// OpConv: Obj = NoObj: Dst ⊇ X. Otherwise store(Obj(Obj, Root), X, own) and
// Dst ⊇ <Obj(Obj, Root), Root, false>.
func (s *solver) ruleConv(in *eir.Instr) {
	if in.Obj == eir.NoObj || !solve.BoxNeeded(s.env, &s.f.Objects[in.Obj]) {
		s.def(in.Dst, s.val(in.X))
		return
	}
	s.store(s.objPtr(in.Obj), s.val(in.X), in.Pos, own)
	s.def(in.Dst, s.objPtr(in.Obj))
}

// OpClosure: store(Obj(Obj, [k]), Args[k], own) for each k;
// Dst ⊇ <Obj(Obj, Root), Root, false>.
func (s *solver) ruleClosure(in *eir.Instr) {
	for k, a := range in.Args {
		p, c := s.ps.Extend(domain.Root, domain.Step(k))
		s.store(s.objAt(in.Obj, p, c), s.val(a), in.Pos, own)
	}
	s.def(in.Dst, s.objPtr(in.Obj))
}

// OpMethodVal: store(Obj(Obj, [0]), X, own); Dst ⊇ <Obj(Obj, Root), Root,
// false>.
func (s *solver) ruleMethodVal(in *eir.Instr) {
	p, c := s.ps.Extend(domain.Root, domain.Step(0))
	s.store(s.objAt(in.Obj, p, c), s.val(in.X), in.Pos, own)
	s.def(in.Dst, s.objPtr(in.Obj))
}

// OpSink: see ruleReturn for SinkReturn; SinkGlobal: sink(X, E3002); SinkSend:
// sink(X, E3005); SinkTask: sink(X, E3004); SinkExternal: sink(X, E3003).
func (s *solver) ruleSink(in *eir.Instr) {
	x := s.val(in.X)
	switch in.Sink {
	case eir.SinkReturn:
		s.ruleReturn(in, x)
	case eir.SinkGlobal:
		s.sink(x, diag.StoreFrameView, in.Sink, in.Pos, own)
	case eir.SinkSend:
		s.sink(x, diag.SendFrameView, in.Sink, in.Pos, own)
	case eir.SinkTask:
		s.sink(x, diag.GoFrameView, in.Sink, in.Pos, own)
	case eir.SinkExternal:
		s.sink(x, diag.PassFrameView, in.Sink, in.Pos, own)
	}
}

// SinkReturn: for each <u,T,g> in X ... (rules.go, OpSink).
func (s *solver) ruleReturn(in *eir.Instr, x value) {
	if in.Index < 0 || in.Index >= len(s.res) {
		return
	}
	r := s.res[in.Index]
	for v := range x {
		u := v.t
		switch {
		case u.kind == kObj && s.objs[u.id].isReg:
			// Obj(Reg(s), ...): the result holds the region's closures.
			if !s.outer[in.Index] {
				s.outer[in.Index] = true
				s.changed = true
			}
			s.regionOutlives(u.id, in)
		case u.kind == kObj && s.objs[u.id].life == eir.LifeCaller:
			if !s.builds || !s.outer[in.Index] {
				s.builds, s.outer[in.Index] = true, true
				s.changed = true
			}
			// The regions of calls held in it get the check of Reg(s).
			held := value{}
			for pl, v := range s.places {
				if pl.obj == u.id {
					held.addAll(v)
				}
			}
			for y := range s.reach(held) {
				if y.t.kind == kObj && s.objs[y.t.id].isReg {
					s.regionOutlives(y.t.id, in)
				}
			}
		case u.kind == kObj && s.objs[u.id].life != eir.LifeHeap:
			s.finding(diag.ReturnFrameView, in.Pos, u.id, in.Sink, own)
		case u.kind == kIn:
			k, f := keyOf(u), v.tag
			if rk := (summary.ResultKey{Key: k, Field: f, FieldGE: v.g && !k.GE() && !k.Deep()}); !r.from[rk] {
				r.from[rk] = true
				s.changed = true
			}
		case u.kind == kHeap || u.kind == kCHeap:
			f := v.tag
			set := r.heap
			if u.kind == kCHeap {
				set = r.cheap
			}
			if !set[f] {
				set[f] = true
				s.changed = true
			}
		}
	}
}

// regionOutlives: every frame token in reach(pt(Reg(s))) other than a
// LifeCaller object or a region is a finding E3001 (the region would outlive
// it).
func (s *solver) regionOutlives(reg int, in *eir.Instr) {
	held := value{}
	for pl, v := range s.places {
		if pl.obj == reg {
			held.addAll(v)
		}
	}
	for x := range s.reach(held) {
		if !s.isFrame(x.t) {
			continue
		}
		if o := s.objs[x.t.id]; o.life == eir.LifeCaller || o.isReg {
			continue
		}
		s.finding(diag.ReturnFrameView, in.Pos, x.t.id, in.Sink, own)
	}
}

// OpDrop: for an object o of LifeStmt, every register live after the drop
// whose value holds Obj(o, ...) is a finding E3009 at the drop.
func (s *solver) ruleDrop(b *eir.Block, in *eir.Instr) {
	if s.objs[in.Obj].life != eir.LifeStmt {
		return
	}
	for r := 1; r <= s.f.NumRegs; r++ {
		holds := false
		for x := range s.regs[r] {
			if x.t.kind == kObj && x.t.id == int(in.Obj) {
				holds = true
			}
		}
		if holds && s.liveAfter(b, in, eir.Reg(r)) {
			s.finding(diag.TempViewKept, in.Pos, int(in.Obj), 0, own)
		}
	}
}
