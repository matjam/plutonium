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
	if a.isReg && a.life == eir.LifeFrame && b.life == eir.LifeCaller {
		return true // section 11.5: the caller's region holds the call's
	}
	switch {
	case a.life == eir.LifeIter && b.life == eir.LifeIter:
		return s.nestedIn(b.loop, a.loop)
	case a.life > b.life:
		return true
	case a.life == b.life:
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
			s.putPlace(place{dt.id, dt.path, dt.ge}, v)
			for x := range v {
				if x.t.kind == kObj && !s.outlives(x.t.id, dt.id) {
					s.finding(s.storeCode(x.t.id), pos, x.t.id, 0, c)
				}
			}
		case kIn:
			for x := range v {
				p, cut := s.concat(dt.path, x.tag)
				dk := domain.MakeKey(dt.id, p, domain.KeyFlags{GE: dt.ge || x.g || cut})
				s.storeInto(dk, x.t, pos, c)
			}
		case kHeap, kCHeap:
			for x := range v {
				switch {
				case s.isFrame(x.t):
					s.finding(s.frameCode(c), pos, x.t.id, 0, c)
				case x.t.kind == kIn:
					s.addEsc(keyOf(x.t))
				}
			}
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

// sink is sink(v, code): v is kept where it outlives the call.
func (s *solver) sink(v value, code diag.Code, kind eir.SinkKind, pos token.Pos, c callCtx) {
	for x := range v {
		switch {
		case s.isFrame(x.t):
			s.finding(code, pos, x.t.id, kind, c)
		case x.t.kind == kIn:
			s.addEsc(keyOf(x.t))
		}
	}
}

func (s *solver) addEsc(k domain.Key) {
	if !s.esc[k] {
		s.esc[k] = true
		s.changed = true
	}
}

func (s *solver) addStore(p summary.StorePair) {
	if !s.stores[p] {
		s.stores[p] = true
		s.changed = true
	}
}
