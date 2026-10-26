package refsolve

import (
	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/summary"
	"github.com/matjam/plutonium/internal/syntax/token"
)

// callCtx says where a store or sink comes from: the function's own code
// (callee eir.NoFunc) or an entry of a callee's summary.
type callCtx struct {
	isCall bool
	site   eir.SiteID
	callee eir.FuncID
	entry  int
}

var own = callCtx{callee: eir.NoFunc}

// nestedIn reports whether loop l is loop anc or nested in it.
func (s *solver) nestedIn(l, anc int) bool {
	for ; l >= 0; l = s.f.Loops[l].Parent {
		if l == anc {
			return true
		}
	}
	return l == anc
}

// outlives is outlives(o2, o) of rules.go ("The store check").
func (s *solver) outlives(o2, o int) bool {
	if o2 == o {
		return true
	}
	a, b := s.objs[o2], s.objs[o]
	if a.isReg && a.life == eir.LifeFrame && b.destLife == eir.LifeCaller {
		return true // section 11.5: the caller's region holds the call's
	}
	switch {
	case a.life == eir.LifeIter && b.destLife == eir.LifeIter:
		return s.nestedIn(b.destLoop, a.loop)
	case a.life > b.destLife:
		return true
	case a.life == b.destLife:
		return true
	}
	return false
}

// storeCode is the code of a frame object that does not outlive the place it
// is stored in.
func (s *solver) storeCode(o2 int) diag.Code {
	switch a := s.objs[o2]; {
	case a.isReg && a.life == eir.LifeIter:
		return diag.ClosureRegionLoop
	case a.life == eir.LifeIter:
		return diag.LoopFrameView
	case a.life == eir.LifeStmt:
		return diag.TempViewKept
	}
	return diag.StoreFrameView
}

// store is store(D, v, why) of rules.go, "The store check": v is stored
// through every pointer token of D.
func (s *solver) store(d, v value, pos token.Pos, c callCtx) {
	for dx := range d {
		switch dt := dx.t; dt.kind {
		case kObj:
			if s.objs[dt.id].kind == eir.ObjBoxView {
				// A box view is heap memory as a store destination.
				s.store(single(ptr(tok{kind: kHeap})), v, pos, c)
				continue
			}
			s.putPlace(place{dt.id, dt.path, dt.ge}, v)
			for x := range v {
				if x.t.kind == kObj && s.callerMemory(dt.id) && s.callerMemory(x.t.id) && !s.selfStore {
					s.selfStore = true // solve's checkStore
					s.changed = true
				}
				if x.t.kind == kObj && !s.outlives(x.t.id, dt.id) {
					s.finding(s.storeCode(x.t.id), pos, x.t.id, 0, c)
				}
			}
		case kIn:
			for x := range v {
				// D' = off(d, T), FieldsGE when the tag is GE: a GE key
				// stays as it is.
				d2 := s.off(dt, x.tag, false)
				dk := keyOf(d2).NoFilter() // a destination keeps no type filter (D51)
				if x.g {
					dk = dk.WithFieldsGE()
				}
				s.storeInto(dk, x.t, pos, c)
			}
		case kHeap, kCHeap:
			s.heapSink(v, s.frameCode(c), pos, c)
		}
	}
}

// storeInto is the d = In case of store, for one stored token u.
func (s *solver) storeInto(dk domain.Key, u tok, pos token.Pos, c callCtx) {
	switch {
	case s.isFrame(u):
		s.finding(s.frameCode(c), pos, u.id, 0, c)
	case u.kind == kIn:
		s.addStore(summary.StorePair{Src: keyOf(u), Dst: dk})
	case u.kind == kHeap || u.kind == kCHeap:
		if !s.hstore[dk] {
			s.hstore[dk] = true
			s.changed = true
		}
	}
}

// frameCode is E3002, or E3003 when the pointer goes through a call.
func (s *solver) frameCode(c callCtx) diag.Code {
	if c.isCall {
		return diag.PassFrameView
	}
	return diag.StoreFrameView
}

// sink is sink(v, code): v is kept where it outlives the call and no
// runtime lifetime check follows it (a package variable, a channel, a task,
// code outside Plutonium): an In token's key is Strict, without arn.
func (s *solver) sink(v value, code diag.Code, kind eir.SinkKind, pos token.Pos, c callCtx) {
	for x := range v {
		switch {
		case s.isFrame(x.t):
			s.finding(code, pos, x.t.id, kind, c)
		case x.t.kind == kIn:
			t := x.t
			t.arn = false
			s.addEsc(keyOf(t).WithStrict())
		}
	}
}

// heapSink is sink(v, code) for heap memory or a callee that keeps v
// (solve's cHeap): a view of an arena's memory kept there keeps its arena
// lifetime, which a checked build checks at run time (section 18.9), so
// what it reaches is checked in its place; an In token's key is as it is.
func (s *solver) heapSink(v value, code diag.Code, pos token.Pos, c callCtx) {
	all, mem := value{}, value{}
	for x := range v {
		all.add(x)
		if s.arenaMemTok(x.t) {
			mem.add(normal(x)) // reach ignores the tag
		}
	}
	if len(mem) > 0 {
		all.addAll(s.reach(mem))
	}
	for x := range all {
		switch {
		case s.arenaMemTok(x.t):
		case s.isFrame(x.t):
			s.finding(code, pos, x.t.id, 0, c)
		case x.t.kind == kIn:
			s.addEsc(keyOf(x.t))
		}
	}
}

// arenaMemTok reports whether t points into an arena's memory: an arn Obj
// token or one of an object eir.Func.ArenaMemoryObjects marks.
func (s *solver) arenaMemTok(t tok) bool {
	if t.kind != kObj {
		return false
	}
	if t.arn {
		return true
	}
	if s.arenaMem == nil {
		s.arenaMem = s.f.ArenaMemoryObjects()
	}
	return t.id < len(s.arenaMem) && s.arenaMem[t.id]
}

func (s *solver) addEsc(k domain.Key) {
	if !s.esc[k] {
		s.esc[k] = true
		s.changed = true
	}
}

func (s *solver) addStore(p summary.StorePair) {
	in := p.Src.Input()
	if s.storeCut[in] {
		p = summary.StorePair{Src: summary.CutKey(s.ps, p.Src), Dst: summary.CutKey(s.ps, p.Dst)}
	}
	if s.stores[p] {
		return
	}
	s.stores[p] = true
	s.changed = true
	if s.storeCut[in] {
		return
	}
	n := 0
	for q := range s.stores {
		if q.Src.Input() == in {
			n++
		}
	}
	if n > summary.MaxRawStorePerInput {
		// rules.go, "The summary": past the raw cap, the input's pairs
		// are cut, those so far and those to come.
		s.storeCut[in] = true
		for q := range s.stores {
			if q.Src.Input() == in {
				delete(s.stores, q)
				s.stores[summary.StorePair{Src: summary.CutKey(s.ps, q.Src), Dst: summary.CutKey(s.ps, q.Dst)}] = true
			}
		}
	}
}
