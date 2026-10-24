package refsolve

import (
	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
)

// Calls of function values (rules.go, "Calls of function values"; the
// solver's solve/funccall.go).

// ownRecord returns the closure whose record x is, when x is a record this
// function made, of a closure the solve may read.
func (s *solver) ownRecord(x tv) (eir.FuncID, bool) {
	t := x.t
	if t.kind != kObj || t.ge || t.path != domain.Root || s.closureOf[t.id] == 0 {
		return eir.NoFunc, false
	}
	fn := s.closureOf[t.id] - 1
	if !s.env.CanRead(s.f, fn) {
		return eir.NoFunc, false
	}
	return fn, true
}

// regionRecord returns the literals a region token x may be a record of,
// when its callee's summary names them all and the solve may read each
// (solve's regionKnown).
func (s *solver) regionRecord(x tv) ([]eir.FuncID, bool) {
	t := x.t
	if t.kind != kObj || !s.objs[t.id].isReg {
		return nil, false
	}
	lits, ok := s.regionLits[t.id]
	if !ok || len(lits) == 0 {
		return nil, false
	}
	for _, fn := range lits {
		if fn == eir.NoFunc || int(fn) >= len(s.env.Program().Funcs) || !s.env.CanRead(s.f, fn) {
			return nil, false
		}
	}
	return lits, true
}

// rest is function value v less the closure records ownRecord resolves
// and the regions regionRecord does.
func (s *solver) rest(v value) value {
	r := value{}
	for x := range v {
		if _, ok := s.ownRecord(x); ok {
			continue
		}
		if _, ok := s.regionRecord(x); ok {
			continue
		}
		r.add(x)
	}
	return r
}

// funcCall applies CallFunc in of the function's own code (not a dcall):
// each closure record of this function in the function value applies that
// closure with its record bound to it alone; the candidates apply with
// their record bound to the rest of the value, when it holds anything.
func (s *solver) funcCall(in *eir.Instr) {
	c := in.Call
	target := s.val(c.Target)
	funcs := s.env.Program().Funcs
	for x := range target {
		if fn, ok := s.ownRecord(x); ok {
			s.applyBound(in, fn, s.recBinder(c, funcs[fn], single(ptr(x.t))), -1)
		}
		if lits, ok := s.regionRecord(x); ok {
			for _, fn := range lits {
				s.applyBound(in, fn, s.recBinder(c, funcs[fn], single(ptr(x.t))), -1)
			}
		}
	}
	rest := s.rest(target)
	if len(rest) == 0 {
		return
	}
	for _, id := range s.env.Candidates(s.f, c) {
		s.applyCandidateRec(in, id, rest)
	}
}

// recBinder is binder with the record input (a CallFunc candidate's input
// past the arguments) bound to rec.
func (s *solver) recBinder(c *eir.Call, cf *eir.Func, rec value) func(int) value {
	b := s.binder(c, cf)
	if c.Kind != eir.CallFunc || len(cf.Inputs) != len(c.Args)+1 {
		return b
	}
	return func(i int) value {
		if i == len(c.Args) {
			return rec
		}
		return b(i)
	}
}
