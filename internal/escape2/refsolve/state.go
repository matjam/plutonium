package refsolve

import (
	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/solve"
	"github.com/matjam/plutonium/internal/escape2/summary"
	"github.com/matjam/plutonium/internal/syntax/token"
)

// objInfo is what the rules need to know of a frame object: the objects of
// the function, then Reg(s) for each call site s (rules.go, "Abstract
// values").
type objInfo struct {
	life     eir.Lifetime
	loop     int // the loop of a LifeIter object, or -1
	destLife eir.Lifetime
	destLoop int  // life and loop as a store destination (rules.go, outlives)
	isReg    bool // Reg(s)
	kind     eir.ObjKind
	wg       bool // a sync.WaitGroup
}

type findingKey struct {
	code diag.Code
	obj  eir.ObjID
	pos  token.Pos
}

type resAcc struct {
	from  map[summary.ResultKey]bool
	heap  map[domain.PathID]bool
	cheap map[domain.PathID]bool
}

// solver is the state of one run: the values of the registers, the places of
// the frame objects, and what the rules have put in the summary so far.
type solver struct {
	fbDepth int // inside the application of a fallback group (defer.go, dcall.own)

	f   *eir.Func
	env solve.Env
	ps  domain.Paths

	objs []objInfo
	defs []*eir.Instr // by register: its one definition
	// where each instruction lives, for liveness and loops
	blockOf map[*eir.Instr]eir.BlockID
	indexOf map[*eir.Instr]int

	regs   []value
	places map[place]value

	esc      map[domain.Key]bool
	stores   map[summary.StorePair]bool
	storeCut [domain.MaxInputs]bool // inputs past summary.MaxRawStorePerInput
	hstore   map[domain.Key]bool
	res      []resAcc
	builds   bool
	outer    map[int]bool

	findings map[findingKey]solve.Finding

	closureOf map[int]eir.FuncID // a closure record object to its function, plus one
	dcalls    []*dcall
	dcallIdx  map[string]*dcall

	changed bool
}

func newSolver(f *eir.Func, env solve.Env) *solver {
	s := &solver{
		f:         f,
		env:       env,
		ps:        env.Paths(),
		regs:      make([]value, f.NumRegs+1),
		places:    map[place]value{},
		esc:       map[domain.Key]bool{},
		stores:    map[summary.StorePair]bool{},
		hstore:    map[domain.Key]bool{},
		res:       make([]resAcc, f.Results),
		outer:     map[int]bool{},
		findings:  map[findingKey]solve.Finding{},
		closureOf: map[int]eir.FuncID{},
		dcallIdx:  map[string]*dcall{},
		defs:      make([]*eir.Instr, f.NumRegs+1),
		blockOf:   map[*eir.Instr]eir.BlockID{},
		indexOf:   map[*eir.Instr]int{},
	}
	for i := range s.regs {
		s.regs[i] = value{}
	}
	for i := range s.res {
		s.res[i] = resAcc{map[summary.ResultKey]bool{}, map[domain.PathID]bool{}, map[domain.PathID]bool{}}
	}
	for _, o := range f.Objects {
		life := o.Life
		if life == eir.LifeBlock {
			life = eir.LifeFrame // rules.go, outlives: a block's variables live in the frame
		}
		dl, dlp := life, o.Loop
		if life == eir.LifeHeader {
			// One iteration as a source; across the loop's iterations as a
			// destination.
			life = eir.LifeIter
			dl, dlp = eir.LifeFrame, -1
			if o.Loop >= 0 && f.Loops[o.Loop].Parent >= 0 {
				dl, dlp = eir.LifeIter, f.Loops[o.Loop].Parent
			}
		}
		wg := o.Type != nil && solve.IsWaitGroupType(o.Type)
		s.objs = append(s.objs, objInfo{life: life, loop: o.Loop, destLife: dl, destLoop: dlp, kind: o.Kind, wg: wg})
	}
	// Reg(s) for every call site: the lifetime of its call.
	regs := make([]objInfo, f.NumSites)
	for _, b := range f.Blocks {
		for i := range b.Instrs {
			in := &b.Instrs[i]
			s.blockOf[in] = b.ID
			s.indexOf[in] = i
			if in.Dst != 0 {
				s.defs[in.Dst] = in
			}
			if in.Op == eir.OpClosure && in.Obj != eir.NoObj {
				s.closureOf[int(in.Obj)] = in.Callee + 1
			}
			if in.Op == eir.OpCall {
				for _, r := range in.Call.Results {
					if r != 0 {
						s.defs[r] = in
					}
				}
				ri := objInfo{life: eir.LifeFrame, loop: -1, isReg: true}
				if b.Loop >= 0 {
					ri.life, ri.loop = eir.LifeIter, b.Loop
				}
				ri.destLife, ri.destLoop = ri.life, ri.loop
				regs[in.Call.Site] = ri
			}
		}
	}
	s.objs = append(s.objs, regs...)
	return s
}

// regObj is the object number of Reg(site).
func (s *solver) regObj(site eir.SiteID) int { return len(s.f.Objects) + int(site) }

func (s *solver) val(r eir.Reg) value {
	if r == 0 {
		return value{}
	}
	return s.regs[r]
}

// def adds v to register r.
func (s *solver) def(r eir.Reg, v value) {
	if r == 0 {
		return
	}
	if s.addCovered(s.regs[r], v) {
		s.changed = true
	}
}

// covers reports whether tagged token c covers x (rules.go, "GE
// subsumption"): both In tokens of one input, c GE with a path that is a
// prefix of x's, or Deep with such a path and x's path with more loads (or x
// Deep too), and c's tag holding x's.
func (s *solver) covers(c, x tv) bool {
	if c == x || c.t.kind != kIn || x.t.kind != kIn || !c.t.ge && !c.t.deep || c.t.id != x.t.id || !s.ps.HasPrefix(x.t.path, c.t.path) {
		return false
	}
	if c.t.deep && !x.t.deep && s.ps.Loads(x.t.path) == s.ps.Loads(c.t.path) {
		return false
	}
	if c.tag == x.tag {
		return c.g || !x.g
	}
	return c.g && s.ps.HasPrefix(x.tag, c.tag)
}

// addCovered adds the elements of w to v that no GE element of v covers,
// and drops the elements of v a new GE element covers. It reports whether v
// changed.
func (s *solver) addCovered(v, w value) bool {
	changed := false
	for x := range w {
		if v[x] {
			continue
		}
		covered := false
		for c := range v {
			if s.covers(c, x) {
				covered = true
				break
			}
		}
		if covered {
			continue
		}
		if x.t.kind == kIn && (x.t.ge || x.t.deep) {
			for y := range v {
				if s.covers(x, y) {
					delete(v, y)
				}
			}
		}
		v[x] = true
		changed = true
	}
	return changed
}

// putPlace adds v to the place pl.
func (s *solver) putPlace(pl place, v value) {
	cur := s.places[pl]
	if cur == nil {
		cur = value{}
		s.places[pl] = cur
		s.changed = true
	}
	if s.addCovered(cur, v) {
		s.changed = true
	}
}

func (s *solver) finding(code diag.Code, pos token.Pos, obj int, sink eir.SinkKind, c callCtx) {
	k := findingKey{code, eir.ObjID(obj), pos}
	if _, ok := s.findings[k]; ok {
		return
	}
	fd := solve.Finding{
		Code: code, Pos: pos, Func: s.f.ID, Obj: eir.ObjID(obj), Sink: sink,
		Site: c.site, Callee: c.callee, Entry: c.entry,
	}
	if obj >= len(s.f.Objects) {
		// A region Reg(s) is no object of the function: Obj is NoObj and
		// Site names its call (rules.go, "Findings").
		fd.Obj, fd.Site = eir.NoObj, eir.SiteID(obj-len(s.f.Objects))
	}
	s.findings[k] = fd
	s.changed = true
}

// isFrame reports whether t is a frame token: Obj(o, ...) of an object whose
// lifetime is not LifeHeap (LifeCaller objects and regions included).
func (s *solver) isFrame(t tok) bool {
	return t.kind == kObj && s.objs[t.id].life != eir.LifeHeap
}

// keyOf is the key of an In token.
func keyOf(t tok) domain.Key {
	return domain.MakeKey(t.id, t.path, domain.KeyFlags{GE: t.ge, FieldsGE: t.fge, Deep: t.deep})
}
