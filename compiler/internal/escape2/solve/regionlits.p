// Translated by go2pu from internal/escape2/solve/regionlits.go at the go-cutover tag; edited by hand since.

package solve

import (
	"compiler/compat"
	"compiler/internal/escape2/domain"
	"compiler/internal/escape2/eir"
	"compiler/internal/escape2/summary"
	"pluto/slices"
)

// Closures a callee returns (summary.Region.Lits; rules.go, "Calls of
// function values"). A function that returns a closure builds its record in
// the region its caller provides for the call, Reg(site): the caller's token
// of the result is Obj(Reg(site), Root, true). The callee's summary names
// the literals whose closures the region may hold: those of its own records
// that live in the caller's memory, and those of the regions of its own
// calls that it returns. A deferred call whose function value is a token of
// a region then applies each of those literals, with its record bound to
// the region's token (whose places hold what the callee may have put in the
// region, Reg(s)), instead of the per-signature join of the call's
// candidates. A region whose literals are not all named (eir.NoFunc among
// them) falls back to the join.

// regionUse is a dcall (or, with fcall, an fcall) whose function value
// held a token of a region.
type regionUse struct {
	i     int
	t     tok
	fcall bool
}

// noteRegionLits adds the literals a callee's summary names to region reg,
// and applies the new ones at the dcalls that called into it already.
func (s *solver) noteRegionLits(reg uint32, lits []eir.FuncID) {
	if s.regionLits == nil {
		s.regionLits = map[uint32][]eir.FuncID{}
	}
	if len(lits) == 0 {
		if _, ok := s.regionLits[reg]; !ok {
			s.regionLits[reg] = nil
		}
		return
	}
	old := s.regionLits[reg]
	var added []eir.FuncID
	for _, l := range lits {
		if !slices.Contains(old, l) && !slices.Contains(added, l) {
			added = compat.AppendPlain(added, l)
		}
	}
	if len(added) == 0 {
		return
	}
	s.regionLits[reg] = compat.AppendPlain(old, added...)
	for _, u := range s.regionUses[reg] {
		if u.fcall {
			s.growFcall(u.i, u.t, added)
			continue
		}
		s.applyRegionLits(u.i, u.t, added)
	}
}

// regionKnown returns the literals a region token t may be a record of,
// when the callees' summaries name them all and the solve may read each:
// an fcall then applies them in place of its candidates (fireFRec, xNoRec).
func (s *solver) regionKnown(t tok) ([]eir.FuncID, bool) {
	if t.kind != tObj || !s.isRegion(t.base) {
		return nil, false
	}
	lits, ok := s.regionLits[t.base]
	if !ok || len(lits) == 0 {
		return nil, false
	}
	for _, fn := range lits {
		if fn == eir.NoFunc || int(fn) >= len(s.prog.Funcs) || !s.env.CanRead(s.f, fn) {
			return nil, false
		}
	}
	return lits, true
}

// regionFcall applies, at fcall i, each literal region token t may be a
// record of, with its record bound to t.
func (s *solver) regionFcall(i int, t tok, lits []eir.FuncID) {
	if s.regionUses == nil {
		s.regionUses = map[uint32][]regionUse{}
	}
	u := regionUse{i, t, true}
	if !slices.Contains(s.regionUses[t.base], u) {
		s.regionUses[t.base] = compat.Append(s.regionUses[t.base], u)
	}
	for _, fn := range lits {
		s.applyFcallLit(i, t, fn)
	}
}

// growFcall applies literals added to region token t's region at fcall i;
// one it cannot apply (not named, or not readable) puts t back in the rest
// of the function value, which applies the candidates.
func (s *solver) growFcall(i int, t tok, added []eir.FuncID) {
	for _, fn := range added {
		if fn == eir.NoFunc || int(fn) >= len(s.prog.Funcs) || !s.env.CanRead(s.f, fn) {
			s.addTok(s.fcalls[i].rest, t)
			continue
		}
		s.applyFcallLit(i, t, fn)
	}
}

// applyFcallLit applies literal fn at fcall i with its record bound to t,
// once.
func (s *solver) applyFcallLit(i int, t tok, fn eir.FuncID) {
	k := [2]uint64{uint64(i) | 1<<62 | uint64(fn)<<32, s.intern(t)}
	if s.dcallDone[k] {
		return
	}
	s.dcallDone[k] = true
	fc := s.fcalls[i]
	c := fc.in.Call
	n := len(s.prog.Funcs[fn].Inputs)
	bind := s.binding(c, n, fn)
	if n == len(c.Args)+1 {
		bind[n-1] = uint64(s.tokNode(t))
	}
	s.applyBound(fc.in, fn, bind, -1)
}

// regionDcall handles token t of a region, of the function value of dcall
// i: each literal the region may hold applies, now and as more are noted.
func (s *solver) regionDcall(i int, t tok) {
	if s.regionUses == nil {
		s.regionUses = map[uint32][]regionUse{}
	}
	for _, u := range s.regionUses[t.base] {
		if u.i == i && u.t == t && !u.fcall {
			return
		}
	}
	s.regionUses[t.base] = compat.Append(s.regionUses[t.base], regionUse{i, t, false})
	lits, ok := s.regionLits[t.base]
	if !ok {
		s.fallback(i) // a region no summary names the literals of
		return
	}
	s.applyRegionLits(i, t, lits)
}

func (s *solver) applyRegionLits(i int, t tok, lits []eir.FuncID) {
	for _, fn := range lits {
		if fn == eir.NoFunc || int(fn) >= len(s.prog.Funcs) {
			s.fallback(i)
			continue
		}
		s.applyClosure(i, fn, t)
	}
}

// noteOuterRegion notes that the function returns region reg (or a closure
// of its own that holds it): reg's literals are its region's.
func (s *solver) noteOuterRegion(reg uint32) {
	if s.outerRegs == nil {
		s.outerRegs = map[uint32]bool{}
	}
	s.outerRegs[reg] = true
}

// regionLitsOut is Region.Lits of the summary: the literals of the
// function's own records that live in its caller's memory (a method
// value's record, whose wrapper the summary does not name, is eir.NoFunc),
// and those of the regions of its calls that it returns.
func (s *solver) regionLitsOut() []eir.FuncID {
	var lits []eir.FuncID
	for o := range s.f.Objects {
		ob := &s.f.Objects[o]
		if ob.Life != eir.LifeCaller {
			continue
		}
		switch ob.Kind {
		case eir.ObjClosure:
			if fn, ok := s.closureOf[uint32(o)]; ok {
				lits = compat.AppendPlain(lits, fn)
			} else {
				lits = compat.AppendPlain(lits, eir.NoFunc)
			}
		case eir.ObjMethodVal:
			lits = compat.AppendPlain(lits, eir.NoFunc)
		}
	}
	for reg := range s.outerRegs {
		ls, ok := s.regionLits[reg]
		if !ok {
			ls = []eir.FuncID{eir.NoFunc}
		}
		lits = compat.AppendPlain(lits, ls...)
	}
	return summary.SortedLits(lits)
}

// callerMemory reports whether object o is in the memory the function's
// caller provides: a record that lives there, or the region of a call.
func (s *solver) callerMemory(o uint32) bool {
	if s.isRegion(o) {
		return true
	}
	return int(o) < len(s.f.Objects) && s.f.Objects[o].Life == eir.LifeCaller
}

// noteRegionSelf notes that a callee's region may point into itself: its
// place holds the region's token (call.go, region).
func (s *solver) noteRegionSelf(reg uint32) {
	if s.regionSelf == nil {
		s.regionSelf = map[uint32]bool{}
	}
	if s.regionSelf[reg] {
		return
	}
	s.regionSelf[reg] = true
	s.addTok(s.place(reg, domain.Root, true), tok{kind: tObj, base: reg, ge: true, tagGE: true})
}

// regionSelfOut is Region.Self of the summary: the function stored a
// pointer to its caller's memory there, or returns a region that has Self.
func (s *solver) regionSelfOut() bool {
	if s.selfStore {
		return true
	}
	for reg := range s.outerRegs {
		if s.regionSelf[reg] {
			return true
		}
	}
	return false
}
