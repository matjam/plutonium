package solve

import (
	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
)

// Calls of function values (rules.go, "Calls of function values"). A token
// of the function value that is a closure record this function made, of a
// closure whose summary the solve may read, is that closure: the call
// applies the closure's summary with its record bound to that token alone.
// The call's candidates apply with their record bound to the rest of the
// value, and only once the rest holds a token: a value that holds nothing
// but this function's own closures calls nothing else. So a candidate's
// record never reads another closure's record.

// fcall is a CallFunc of the function's own code that is not a dcall.
type fcall struct {
	in     *eir.Instr
	rest   uint32 // the function value less the closures it resolves
	joined bool   // the candidates applied
}

// ownRecord returns the closure whose record t is, when t is a record this
// function made, of a closure the solve may read.
func (s *solver) ownRecord(t tok) (eir.FuncID, bool) {
	if t.kind != tObj || t.ge || t.path != domain.Root {
		return eir.NoFunc, false
	}
	fn, ok := s.closureOf[t.base]
	if !ok || !s.env.CanRead(s.f, fn) {
		return eir.NoFunc, false
	}
	return fn, true
}

// restNode returns the node of what function value node n holds less the
// closure records ownRecord resolves.
func (s *solver) restNode(n uint32) uint32 {
	if r, ok := s.derived.get(dRest|uint64(n)<<20, 0); ok {
		return uint32(r)
	}
	r := s.newNode(nkPinned)
	s.derived.put(s.a, dRest|uint64(n)<<20, 0, uint64(r))
	s.addEdge(n, r, xw(xNoRec, 0, false), 0)
	return r
}

// funcCall handles CallFunc in of the function's own code (not a dcall).
func (s *solver) funcCall(in *eir.Instr) {
	c := in.Call
	target := s.canonBase(c.Target)
	i := len(s.fcalls)
	s.fcalls = append(s.fcalls, &fcall{in: in, rest: s.restNode(target)})
	s.addCons(target, cFRec, uint64(i), 0)
	s.addCons(s.fcalls[i].rest, cFRest, uint64(i), 0)
}

// fireFRec handles token t of the function value of fcall i: a closure
// record of this function applies that closure, once per record.
func (s *solver) fireFRec(i int, t tok) {
	fn, ok := s.ownRecord(t)
	if !ok {
		return
	}
	fc := s.fcalls[i]
	t = asPointer(t) // the record, whatever field of a value it came from
	k := [2]uint64{uint64(i) | 1<<62, s.intern(t)}
	if s.dcallDone[k] {
		return
	}
	s.dcallDone[k] = true
	c := fc.in.Call
	n := len(s.prog.Funcs[fn].Inputs)
	bind := s.binding(c, n, fn)
	if n == len(c.Args)+1 {
		bind[n-1] = uint64(s.tokNode(t))
	}
	s.applyBound(fc.in, fn, bind, -1)
}

// fireFRest applies the candidates of fcall i, once, when the rest of its
// function value holds a token.
func (s *solver) fireFRest(i int) {
	fc := s.fcalls[i]
	if fc.joined {
		return
	}
	fc.joined = true
	c := fc.in.Call
	for _, cand := range s.env.Candidates(s.f, c) {
		s.applyCandidateRec(fc.in, cand, fc.rest)
	}
}
