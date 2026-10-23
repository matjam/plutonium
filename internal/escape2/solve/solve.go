package solve

import (
	"slices"

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/summary"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// Solve solves f against env in ws and returns its summary and findings, by
// the rules of rules.go. Findings are complete only when every callee's
// summary is final; the scheduler reports them once f's SCC has converged.
//
// Everything Solve builds lives in ws.Arena and ws.Sets, except the result:
// the summary and the findings, which the caller keeps (and copies to the
// SCC or program arena) before it resets the workspace.
func Solve(f *eir.Func, env Env, ws *Workspace) Result {
	r, _ := SolveKeep(f, env, ws)
	return r
}

// SolveDerivs is Solve, and gives every derivation of each entry of the
// summary (Result.Derivs).
func SolveDerivs(f *eir.Func, env Env, ws *Workspace) Result {
	s := newSolver(f, env, ws)
	s.derivs = true
	s.build()
	return s.finish()
}

// SolveCheck is Solve with no store pairs cut as they are collected, and
// gives the summary before the caps as well (Result.Unwidened): what a
// resumed solve must cover. Caps and cuts are not monotone in the order of
// the derivations, so a resumed solve's widened summary need not cover the
// widened summary of a solve from scratch, but it covers every entry that
// solve derives. It is for checks (PLUTO_ESCAPE2_RESUMECHECK), not for the
// compiler: without the cut, a solve's memory is not bounded.
func SolveCheck(f *eir.Func, env Env, ws *Workspace) Result {
	s := newSolver(f, env, ws)
	s.check = true
	s.build()
	r := s.finish()
	r.Unwidened = s.unwidened
	return r
}

// SolveKeep is Solve, and returns the solver's state as well, kept in ws,
// for a later solve of f to resume (State.Resume): the caller must not
// reset ws while it holds the state.
func SolveKeep(f *eir.Func, env Env, ws *Workspace) (Result, *State) {
	s := newSolver(f, env, ws)
	s.build()
	return s.finish(), &State{s}
}

// finish runs the solve to its fixed point and returns its result.
func (s *solver) finish() Result {
	s.run()
	if s.limit != nil {
		return Result{Limit: s.limit}
	}
	s.checkDrops()
	s.measure()
	sum := s.summary()
	s.dump(sum)
	return Result{Summary: sum, Findings: append([]Finding(nil), s.findings...), Derivs: s.derivOut}
}

// State is a solve kept to be resumed.
type State struct{ s *solver }

// Resume solves f again against env, whose summaries of the functions f
// calls each cover the one the kept solve applied (the scheduler joins an
// SCC's iterate with the previous one, a virtual join only gains members'
// entries, and the summaries of earlier SCCs are final): it applies what the
// summaries gained and goes on from the kept fixed point, which gives the
// result a solve from scratch gives. The limits apply to the work of each
// resume and to the memory of the whole state.
func (st *State) Resume(env Env) Result {
	s := st.s
	s.env = env
	// The paths of the env it resumes with: the parallel driver gives each
	// solve paths of its own (sched/overlay.go).
	s.ps = env.Paths()
	s.nsteps = 0
	s.reapply()
	return s.finish()
}

// Bytes is what the state holds: its arena and set table bytes.
func (st *State) Bytes() int { return st.s.a.Bytes() - st.s.base + st.s.sets.Bytes() }

// solver is the state of one solve.
type solver struct {
	f    *eir.Func
	env  Env
	ps   domain.Paths
	sets domain.Sets
	a    domain.Arena
	prog *eir.Program

	nobj   int // objects of f, then one region per call site
	base   int // the arena's bytes when the solve began
	nsteps int // constraint firings and edge transforms, for MaxFuncSteps

	toks   vec // two words per tagged token
	tokIdx table

	lists, bits                                                               []vec // per node: the tokens it holds, and their bitset
	done, nodeRep, nodeEdges, nodeCons, nodeInfo, nodeInfo2, nodeNext, inWork vec

	edges, cons recs
	work        vec
	head        int

	edgeSeen, consSeen table
	places             table // (o<<1|pg, path) -> node
	derived            table // (node<<2|kind, path) -> node
	objPlaces, objSubs vec   // per object: list heads (+1)
	subs               vec   // node, path, flags, next

	defs      vec // per register: block<<32 | instr+1
	siteBlock vec // per call site: its block+1

	whys vec // whyWords per entry

	// summary entries, de-duplicated
	sumSeen                 table
	esc, store, heapSt, res vec
	resHeap                 vec
	builds                  bool
	outer                   vec
	findSeen                table
	findings                []Finding
	drops                   vec // block<<32 | instr, of drops of statement temporaries
	heapNode                uint32
	buf                     vec
	merged                  int
	noCollapse              bool          // tests: solve without cycle collapse
	assertT                 []types2.Type // the interface types of xImpl edges, by index
	storeRaw                [domain.MaxInputs]int
	storeCut                [domain.MaxInputs]bool
	tjIndex, tjLow, tjStack vec
	tjSeen                  table
	tjCounter               uint64
	stepBuf                 []domain.Step
	applied                 []application // the summaries the calls applied (State.Resume)
	limit                   *LimitError   // the solve went past the limits (Result.Limit)
	derivs                  bool          // collect every derivation (SolveDerivs)
	check                   bool          // SolveCheck: no store cut, and keep unwidened
	unwidened               *summary.Summary
	escDerivs, storeDerivs  vec // key words and why, of every Esc and Store derivation
	derivSeen               table
	derivOut                [][]summary.Reason    // Result.Derivs
	dcalls                  []*dcall              // calls of function values (defer.go)
	closureOf               map[uint32]eir.FuncID // closure record objects of f (OpClosure) to their function
	dcallIdx                map[string]int
	dcallDone               map[[2]uint64]bool
	fcalls                  []*fcall             // calls of function values (funccall.go)
	argBuf                  []byte               // argNode's key
	deferOrigins            map[int][]uint64     // capDeferred: the origin calls with Deferred keys, by input, sorted
	closApps                map[closKey][]uint32 // applyClosure's applications: their argument nodes
	fbGroups                map[fbKey]*fbGroup   // fallback groups (defer.go)
	argNodes                map[string]uint32
	whyIdx                  table // whys by content, so that a constraint is made once
}

func newSolver(f *eir.Func, env Env, ws *Workspace) *solver {
	s := &solver{
		f:    f,
		env:  env,
		ps:   env.Paths(),
		sets: ws.Sets,
		a:    ws.Arena,
		prog: env.Program(),
		base: ws.Arena.Bytes(),
	}
	s.edges.w, s.cons.w = edgeWords, consWords
	s.nobj = len(f.Objects) + f.NumSites
	s.objPlaces.resize(s.a, s.nobj)
	s.objSubs.resize(s.a, s.nobj)
	s.defs.resize(s.a, f.NumRegs+1)
	s.siteBlock.resize(s.a, f.NumSites)
	s.whys.resize(s.a, whyWords) // why 0 is none
	for r := 0; r <= f.NumRegs; r++ {
		s.newNode(nkReg)
	}
	s.heapNode = s.newNode(nkPinned)
	s.addTok(s.heapNode, heapTok)
	s.closureOf = map[uint32]eir.FuncID{}
	for _, blk := range f.Blocks {
		for i := range blk.Instrs {
			if in := &blk.Instrs[i]; in.Op == eir.OpClosure && in.Obj != eir.NoObj {
				s.closureOf[uint32(in.Obj)] = in.Callee
			}
		}
	}
	s.dcallIdx = map[string]int{}
	s.dcallDone = map[[2]uint64]bool{}
	s.closApps = map[closKey][]uint32{}
	s.fbGroups = map[fbKey]*fbGroup{}
	s.argNodes = map[string]uint32{}
	return s
}

// Whys: what a constraint came from, for findings and reasons. Words: pos,
// site+1 (0: the function's own code), callee+1, entry+1, kind | sink<<8.
const whyWords = 5

const (
	whyOwn  = 0
	whyCall = 1
)

func (s *solver) why(pos token.Pos, kind int, sink eir.SinkKind, site eir.SiteID, callee eir.FuncID, entry int) uint64 {
	// The same why is the same id: a summary applied again (State.Resume)
	// makes no constraint twice.
	var k1, k2 uint64
	interned := uint64(site) < 1<<22 && uint64(callee)+1 < 1<<22 && entry >= -1 && entry < 1<<19
	if interned {
		k1 = uint64(pos)<<16 | uint64(kind)<<8 | uint64(sink)
		if kind == whyCall {
			k2 = uint64(site) + 1 | (uint64(callee)+1)<<22 | uint64(int64(entry)+1)<<44
		}
		if id, ok := s.whyIdx.get(k1, k2); ok {
			return id
		}
	}
	id := uint64(len(s.whys.s) / whyWords)
	if interned {
		s.whyIdx.put(s.a, k1, k2, id)
	}
	s.whys.push(s.a, uint64(pos))
	if kind == whyCall {
		s.whys.push(s.a, uint64(site)+1)
		s.whys.push(s.a, uint64(callee)+1)
		s.whys.push(s.a, uint64(int64(entry)+1))
	} else {
		s.whys.push(s.a, 0)
		s.whys.push(s.a, 0)
		s.whys.push(s.a, 0)
	}
	s.whys.push(s.a, uint64(kind)|uint64(sink)<<8)
	return id
}

func (s *solver) whyAt(id uint64) (pos token.Pos, kind int, sink eir.SinkKind, site eir.SiteID, callee eir.FuncID, entry int) {
	w := s.whys.s[id*whyWords : id*whyWords+whyWords]
	pos = token.Pos(w[0])
	kind = int(w[4] & 0xff)
	sink = eir.SinkKind(w[4] >> 8)
	site, callee, entry = ^eir.SiteID(0), eir.NoFunc, -1
	if w[1] != 0 {
		site = eir.SiteID(w[1] - 1)
	}
	if w[2] != 0 {
		callee = eir.FuncID(w[2] - 1)
	}
	if w[3] != 0 {
		entry = int(int64(w[3]) - 1)
	}
	return
}

// codes are the finding codes, by index in constraint words.
var codes = []diag.Code{
	diag.ReturnFrameView,   // 0 E3001
	diag.StoreFrameView,    // 1 E3002
	diag.PassFrameView,     // 2 E3003
	diag.GoFrameView,       // 3 E3004
	diag.SendFrameView,     // 4 E3005
	diag.LoopFrameView,     // 5 E3006
	diag.TempViewKept,      // 6 E3009
	diag.ClosureRegionLoop, // 7 E3101
}

const (
	cE3001 = iota
	cE3002
	cE3003
	cE3004
	cE3005
	cE3006
	cE3009
	cE3101
	cTask // at a task starter (ImplTask): E3004 for a local sync.WaitGroup, else E3003
)

// isWaitGroup reports whether frame object o is a sync.WaitGroup: passing
// its address to a function that starts a task with it is E3004 (section
// 12.12).
func (s *solver) isWaitGroup(o uint32) bool {
	if int(o) >= len(s.f.Objects) || s.f.Objects[o].Type == nil {
		return false
	}
	return isWaitGroupType(s.f.Objects[o].Type)
}

// isWaitGroupType reports whether T is pluto/sync.WaitGroup.
func isWaitGroupType(T types2.Type) bool {
	n, ok := types2.Unalias(T).(*types2.Named)
	return ok && n.Obj().Name() == "WaitGroup" && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == "pluto/sync"
}

// IsWaitGroupType is isWaitGroupType, for the reference solver.
func IsWaitGroupType(T types2.Type) bool { return isWaitGroupType(T) }

// life returns the lifetime and loop of object o (rules.go: a region of
// call site s is object len(Objects)+s).
func (s *solver) life(o uint32) (eir.Lifetime, int) {
	if int(o) < len(s.f.Objects) {
		ob := &s.f.Objects[o]
		switch ob.Life {
		case eir.LifeBlock:
			return eir.LifeFrame, ob.Loop // rules.go, outlives
		case eir.LifeHeader:
			return eir.LifeIter, ob.Loop // as a source: one iteration
		}
		return ob.Life, ob.Loop
	}
	site := int(o) - len(s.f.Objects)
	loop := -1
	if b := s.siteBlock.s[site]; b != 0 {
		loop = s.f.Blocks[b-1].Loop
	}
	if loop >= 0 {
		return eir.LifeIter, loop
	}
	return eir.LifeFrame, -1
}

func (s *solver) isRegion(o uint32) bool { return int(o) >= len(s.f.Objects) }

// destLife is the lifetime of object o as the place a pointer is stored in:
// its life, except that a loop's header variable lives across the loop's
// iterations, within an iteration of the enclosing loop (rules.go,
// outlives; section 18.4).
func (s *solver) destLife(o uint32) (eir.Lifetime, int) {
	if int(o) < len(s.f.Objects) {
		if ob := &s.f.Objects[o]; ob.Life == eir.LifeHeader {
			if ob.Loop >= 0 && s.f.Loops[ob.Loop].Parent >= 0 {
				return eir.LifeIter, s.f.Loops[ob.Loop].Parent
			}
			return eir.LifeFrame, -1
		}
	}
	return s.life(o)
}

// isBoxView reports whether o is a box view (eir.ObjBoxView): a frame
// pointer as a source, heap memory as a store destination and for loads.
func (s *solver) isBoxView(o uint32) bool {
	return int(o) < len(s.f.Objects) && s.f.Objects[o].Kind == eir.ObjBoxView
}

// nested reports whether loop l is loop outer or nested in it.
func (s *solver) nested(l, outer int) bool {
	for l >= 0 {
		if l == outer {
			return true
		}
		l = s.f.Loops[l].Parent
	}
	return false
}

// outlives reports whether object o2 lives at least as long as object o
// (rules.go, outlives).
func (s *solver) outlives(o2, o uint32) bool {
	if o2 == o {
		return true
	}
	l2, lp2 := s.life(o2)
	l, lp := s.destLife(o)
	switch {
	case l2 > l:
		return true
	case l2 < l:
		return false
	case l2 == eir.LifeIter:
		return s.nested(lp, lp2)
	}
	return true
}

// regionOutlives is the exception of section 11.5 to the lifetime order: the
// region of a call outside any loop may be held by the region the function's
// own caller provides, since a function that returns the closures of a call
// passes part of its own region down for them.
func (s *solver) regionOutlives(o2, o uint32) bool {
	if !s.isRegion(o2) {
		return false
	}
	l2, _ := s.life(o2)
	l, _ := s.destLife(o)
	return l2 == eir.LifeFrame && l == eir.LifeCaller
}

// shortCode is the code for storing a pointer to o2 where it outlives o2.
func (s *solver) shortCode(o2 uint32) int {
	l2, _ := s.life(o2)
	switch {
	case s.isRegion(o2) && l2 == eir.LifeIter:
		return cE3101
	case l2 == eir.LifeIter:
		return cE3006
	case l2 == eir.LifeStmt:
		return cE3009
	}
	return cE3002
}

// find records a finding once per (code, object, position).
func (s *solver) report(code int, o uint32, why uint64) {
	pos, _, sink, site, callee, entry := s.whyAt(why)
	if !s.findSeen.add(s.a, uint64(code)<<32|uint64(o), uint64(pos)) {
		return
	}
	obj := eir.ObjID(o)
	if s.isRegion(o) {
		// A region is no object of the function; Site names the call
		// that returned the closures in it.
		obj = eir.NoObj
		site = eir.SiteID(int(o) - len(s.f.Objects))
	}
	s.findings = append(s.findings, Finding{
		Code:   codes[code],
		Pos:    pos,
		Func:   s.f.ID,
		Obj:    obj,
		Sink:   sink,
		Site:   site,
		Callee: callee,
		Entry:  entry,
	})
}

// --- places and subscriptions ---

const (
	subLoad  = 0
	subReach = 1
)

// place returns the node of place <o, p, pg>, making it and linking it to
// the object's other places and subscriptions.
func (s *solver) place(o uint32, p domain.PathID, pg bool) uint32 {
	k1 := uint64(o) << 1
	if pg {
		k1 |= 1
	}
	if n, ok := s.places.get(k1, uint64(p)); ok {
		return uint32(n)
	}
	n := s.newNode(nkPlace)
	s.places.put(s.a, k1, uint64(p), uint64(n))
	s.nodeInfo.s[n] = nkPlace | k1<<8
	s.nodeInfo2.s[n] = uint64(p)
	s.nodeNext.s[n] = s.objPlaces.s[o]
	s.objPlaces.s[o] = uint64(n) + 1
	for sb := s.objSubs.s[o]; sb != 0; sb = s.subs.s[(sb-1)*4+3] {
		w := s.subs.s[(sb-1)*4 : (sb-1)*4+4]
		s.linkSub(n, p, pg, uint32(w[0]), domain.PathID(w[1]), w[2]&1 != 0, int(w[2]>>1))
	}
	return n
}

// rel returns the path of p after its prefix r.
func (s *solver) rel(p, r domain.PathID) domain.PathID {
	q, _ := s.pathOf(s.suffix(p, r))
	return q
}

// subscribe makes node dst read the places of o a pointer Obj(o, p, pg)
// reaches: a load (kind subLoad) or a reach (subReach). The reading is done
// once per (o, p, pg, kind), by a shared node, which dst copies: every load
// or reach through the same pointer, at any call site, shares its links to
// the object's places instead of making its own.
func (s *solver) subscribe(o uint32, p domain.PathID, pg bool, dst uint32, kind int) {
	k1 := dShared | uint64(o)<<4 | uint64(kind)<<1
	if pg {
		k1 |= 1
	}
	sn, ok := s.derived.get(k1, uint64(p))
	if !ok {
		if kind == subReach {
			sn = uint64(s.newNode(nkReach))
		} else {
			sn = uint64(s.newNode(nkPinned))
		}
		s.derived.put(s.a, k1, uint64(p), sn)
		s.subscribeDirect(o, p, pg, uint32(sn), kind)
		if kind == subReach {
			// What the places hold is reached in turn.
			s.addCons(uint32(sn), cReach, 0, 0)
		}
	}
	if uint32(sn) != s.find(dst) {
		s.addEdge(uint32(sn), dst, xw(xID, 0, false), 0)
	}
}

// subscribeDirect links node dst to the places of o (subscribe).
func (s *solver) subscribeDirect(o uint32, p domain.PathID, pg bool, dst uint32, kind int) {
	flags := uint64(kind) << 1
	if pg {
		flags |= 1
	}
	if !s.consSeen.add(s.a, uint64(dst)<<8|0xff, uint64(o)<<34|flags<<32|uint64(p)) {
		return
	}
	i := uint64(len(s.subs.s) / 4)
	s.subs.push(s.a, uint64(dst))
	s.subs.push(s.a, uint64(p))
	s.subs.push(s.a, flags)
	s.subs.push(s.a, s.objSubs.s[o])
	s.objSubs.s[o] = i + 1
	// A load always reads the place it points at, so that later stores
	// through other pointers reach it by the place links.
	if kind == subLoad {
		s.place(o, p, pg)
	}
	for q := s.objPlaces.s[o]; q != 0; q = s.nodeNext.s[q-1] {
		m := uint32(q - 1)
		s.linkSub(m, domain.PathID(s.nodeInfo2.s[m]), s.nodeInfo.s[m]>>8&1 != 0, dst, p, pg, kind)
	}
}

// linkSub links place m = <o, r, rg> to a subscription of dst at <o, p, pg>
// (rules.go, load and reach).
func (s *solver) linkSub(m uint32, r domain.PathID, rg bool, dst uint32, p domain.PathID, pg bool, kind int) {
	under := s.ps.HasPrefix(r, p) // the place is at p+R
	over := s.ps.HasPrefix(p, r)  // the place is at a prefix of p
	if !under && !over {
		return
	}
	if kind == subReach {
		// reach is the closure under load, geified (rules.go, reach).
		if under {
			s.addEdge(m, dst, xw(xGe, 0, false), 0)
		} else {
			s.addEdge(m, dst, xw(xSelGe, s.rel(p, r), false), 0)
		}
		return
	}
	if under {
		R := s.rel(r, p)
		if pg {
			s.addEdge(m, dst, xw(xRetag, 0, false), 0)
		} else {
			s.addEdge(m, dst, xw(xPreLoad, R, rg), 0)
		}
		if !over {
			return
		}
	}
	if over && !under {
		s.addEdge(m, dst, xw(xSelLoad, s.rel(p, r), pg), 0)
	}
}

// storeInto stores the value of node val into object o at path p (a
// pointer Obj(o, p, false) the function makes itself).
func (s *solver) storeInto(o uint32, steps []domain.Step, val uint32, why uint64) {
	if val == 0 {
		return
	}
	if o == uint32(eir.NoObj) {
		s.addCons(val, cSink, cE3002, why) // a static record
		return
	}
	if l, _ := s.life(o); l == eir.LifeHeap {
		s.addCons(val, cSink, cE3002, why)
		return
	}
	p, cut := s.pathOf(steps)
	pn := s.place(o, p, cut)
	s.addEdge(val, pn, xw(xID, 0, false), why)
}

// checkStore is the store check of a token arriving in place m by a store
// (rules.go, store, the case d = Obj).
func (s *solver) checkStore(m uint32, u tok, why uint64) {
	if u.kind != tObj {
		return
	}
	o := uint32(s.nodeInfo.s[m] >> 9)
	if !s.outlives(u.base, o) && !s.regionOutlives(u.base, o) {
		s.report(s.shortCode(u.base), u.base, why)
	}
}

// --- derived nodes for summary keys ---

const (
	dVal        = 0
	dReach      = 1
	dTok        = 2
	dShared     = 1 << 62 // in the first word: a shared subscription node
	dJoin       = 1 << 61 // in the first word: a node joining two receivers
	dRetag      = 1 << 60 // in the first word: a node with its tags made (Root, GE)
	dRest       = 1 << 59 // in the first word: a function value less its own closures (funccall.go)
	dSplitIn    = 1 << 58 // in the first word: the In tokens of a node (defer.go, splitNode)
	dSplitOther = 1 << 57 // in the first word: the other tokens of a node
)

// valNode returns the node of val(k) for an input bound to node base
// (rules.go, val).
func (s *solver) valNode(base uint32, p domain.PathID, ge bool) uint32 {
	if ge {
		return s.reachNode(s.valNode(base, p, false))
	}
	if p == domain.Root {
		return base
	}
	if n, ok := s.derived.get(uint64(base)<<2|dVal, uint64(p)); ok {
		return uint32(n)
	}
	steps := s.ps.Steps(p)
	parentPath, _ := s.pathOf(steps[:len(steps)-1])
	parent := s.valNode(base, parentPath, false)
	n := s.newNode(nkReg)
	s.derived.put(s.a, uint64(base)<<2|dVal, uint64(p), uint64(n))
	last := steps[len(steps)-1]
	if last == domain.StepLoad {
		s.addCons(parent, cLoad, uint64(n), 0)
	} else {
		lp, _ := s.pathOf(steps[len(steps)-1:])
		s.addEdge(parent, n, xw(xStep, lp, false), 0)
	}
	return n
}

// reachNode returns the node of reach(n).
func (s *solver) reachNode(n uint32) uint32 {
	if r, ok := s.derived.get(uint64(n)<<2|dReach, 0); ok {
		return uint32(r)
	}
	r := s.newNode(nkReach)
	s.derived.put(s.a, uint64(n)<<2|dReach, 0, uint64(r))
	s.addEdge(n, r, xw(xGe, 0, false), 0)
	s.addCons(r, cReach, 0, 0)
	return r
}

// tokNode returns a node holding token t alone.
func (s *solver) tokNode(t tok) uint32 {
	id := s.intern(t)
	if n, ok := s.derived.get(dTok, id); ok {
		return uint32(n)
	}
	n := s.newNode(nkTok)
	s.derived.put(s.a, dTok, id, uint64(n))
	s.addTok(n, t)
	return n
}

// --- firing complex constraints ---

func (s *solver) fire(n uint32, c uint64, id uint64) {
	s.nsteps++
	w := s.cons.at(c)
	kind, a, b := int(w[0]), w[1], w[2]
	t := s.tokAt(id)
	switch kind {
	case cLoad:
		dst := uint32(a)
		if b != 0 {
			s.addTok(dst, heapTok)
			return
		}
		switch t.kind {
		case tObj:
			if s.isBoxView(t.base) {
				s.addTok(dst, heapTok) // heap memory for loads
				return
			}
			s.subscribe(t.base, t.path, t.ge, dst, subLoad)
		case tIn:
			u := asPointer(t)
			if !u.ge {
				u.path, u.ge = s.extend(u.path, domain.StepLoad)
			}
			s.addTok(dst, u)
		default:
			s.addTok(dst, heapTok)
		}
	case cReach:
		switch t.kind {
		case tObj:
			if s.isBoxView(t.base) {
				s.addTok(n, geify(heapTok))
				return
			}
			s.subscribe(t.base, t.path, t.ge, n, subReach)
		case tCHeap:
			// A load through a counted reference reads heap memory.
			s.addTok(n, geify(heapTok))
		}
	case cStore:
		val := uint32(a)
		switch t.kind {
		case tObj:
			if l, _ := s.life(t.base); l == eir.LifeHeap || s.isBoxView(t.base) {
				// Heap memory as a store destination (a box view too).
				s.addCons(val, cSink, uint64(s.storeCode(b)), b)
				return
			}
			pn := s.place(t.base, t.path, t.ge)
			s.addEdge(val, pn, xw(xID, 0, false), b)
		case tIn:
			s.addCons(val, cStoreIn, s.intern(asPointer(t)), b)
		default:
			s.addCons(val, cSink, uint64(s.storeCode(b)), b)
		}
	case cStoreIn:
		d := s.tokAt(a)
		switch t.kind {
		case tObj:
			s.report(s.storeCode(b), t.base, b)
		case tIn:
			s.addStorePair(key(asPointer(t)), s.storeDst(d, t), b)
		default:
			s.addHeapStore(s.storeDst(d, t))
		}
	case cSink:
		switch t.kind {
		case tObj:
			code := int(a)
			if code == cTask {
				code = cE3003
				if s.isWaitGroup(t.base) {
					code = cE3004 // section 12.12
				}
			}
			s.report(code, t.base, b)
		case tIn:
			s.addEsc(key(asPointer(t)), b)
		}
	case cReturn:
		r := int(a)
		switch t.kind {
		case tObj:
			l, _ := s.life(t.base)
			switch {
			case s.isRegion(t.base):
				s.addOuter(r)
				rn := s.reachNode(s.tokNode(asPointer(t)))
				s.addCons(rn, cRegionChk, a, b)
			case l == eir.LifeCaller:
				s.builds = true
				s.addOuter(r)
				// The regions of calls whose closures it holds are in it
				// too, with what they captured. What the function stored
				// in it itself had the store check.
				rn := s.reachNode(s.tokNode(asPointer(t)))
				s.addCons(rn, cRegionWalk, a, b)
			default:
				s.report(cE3001, t.base, b)
			}
		case tIn:
			// A GE key is anywhere under its field already (resultCovers).
			s.addRes(r, key(asPointer(t)), t.tag, t.tagGE && !t.ge)
		case tHeap:
			s.addResHeap(r, false, t.tag)
		case tCHeap:
			s.addResHeap(r, true, t.tag)
		}
	case cDCall:
		s.fireDcall(int(a), t)
	case cDArg:
		s.fireDarg(int(a), t)
	case cFRec:
		s.fireFRec(int(a), t)
	case cFRest:
		s.fireFRest(int(a))
	case cRegionWalk:
		if t.kind == tObj && s.isRegion(t.base) {
			rn := s.reachNode(s.tokNode(asPointer(t)))
			s.addCons(rn, cRegionChk, a, b)
		}
	case cRegionChk:
		if t.kind == tObj && !s.isRegion(t.base) {
			if l, _ := s.life(t.base); l < eir.LifeCaller {
				s.report(cE3001, t.base, b)
			}
		}
	case cBack:
		if t.kind != tObj {
			return
		}
		l2, lp2 := s.life(t.base)
		switch {
		case l2 == eir.LifeIter && s.nested(lp2, int(a)):
			if s.isRegion(t.base) {
				s.report(cE3101, t.base, b)
			} else {
				s.report(cE3006, t.base, b)
			}
		case l2 == eir.LifeStmt:
			s.report(cE3009, t.base, b)
		}
	}
}

// storeDst is the key of the place a store through In token d writes a
// token t of the value into: off(d, T) for t's tag T, and when the tag is
// GE, anywhere under it by field and element steps (FieldsGE), not past
// another load; a GE d stays GE (rules.go, "The store check").
func (s *solver) storeDst(d, t tok) domain.Key {
	dst := s.off(d, s.ps.Steps(t.tag))
	k := key(dst)
	if t.tagGE && !k.GE() {
		k = k.WithFieldsGE()
	}
	return k
}

// storeCode is the code of a frame pointer stored where it outlives the
// frame: E3002 by the function's own store, E3003 by a callee's.
func (s *solver) storeCode(why uint64) int {
	if _, kind, _, _, _, _ := s.whyAt(why); kind == whyCall {
		return cE3003
	}
	return cE3002
}

// --- summary entries ---

const (
	sEsc = iota + 1
	sStore
	sHeapStore
	sRes
	sResHeap
	sOuter
	sCutStore = 7 // in the top bits, as sStore
)

func (s *solver) addEsc(k domain.Key, why uint64) {
	if s.derivs && s.derivSeen.add(s.a, uint64(k)<<4|1, why) {
		s.escDerivs.push(s.a, uint64(k))
		s.escDerivs.push(s.a, why)
	}
	if s.sumSeen.add(s.a, sEsc, uint64(k)) {
		s.esc.push(s.a, uint64(k))
		s.esc.push(s.a, why)
	}
}

func (s *solver) addStorePair(src, dst domain.Key, why uint64) {
	if s.derivs && s.derivSeen.add(s.a, uint64(src)<<4|2, uint64(dst)<<20^why) {
		s.storeDerivs.push(s.a, uint64(src))
		s.storeDerivs.push(s.a, uint64(dst))
		s.storeDerivs.push(s.a, why)
	}
	in := src.Input()
	if s.storeCut[in] {
		s.addCutPair(src, dst, why)
		return
	}
	if !s.storeSeenAdd(src, dst) {
		return
	}
	s.store.push(s.a, uint64(src))
	s.store.push(s.a, uint64(dst))
	s.store.push(s.a, why)
	s.storeRaw[in]++
	if s.storeRaw[in] > summary.MaxRawStorePerInput && !s.check {
		// Cut the input's pairs from now on, and those collected so far.
		s.storeCut[in] = true
		old := s.store.s
		s.store = vec{}
		for i := 0; i < len(old); i += 3 {
			a, b := domain.Key(old[i]), domain.Key(old[i+1])
			if a.Input() == in {
				s.addCutPair(a, b, old[i+2])
			} else {
				s.store.push(s.a, old[i])
				s.store.push(s.a, old[i+1])
				s.store.push(s.a, old[i+2])
			}
		}
	}
}

// addCutPair adds a store pair of an input past MaxRawStorePerInput, cut.
func (s *solver) addCutPair(src, dst domain.Key, why uint64) {
	src, dst = summary.CutKey(s.ps, src), summary.CutKey(s.ps, dst)
	if s.sumSeen.add(s.a, uint64(src)|sCutStore<<60, uint64(dst)) {
		s.store.push(s.a, uint64(src))
		s.store.push(s.a, uint64(dst))
		s.store.push(s.a, why)
	}
}

// storeSeenAdd de-duplicates store pairs. Keys use 44 bits, so a pair fits
// two words with the kind in the top bits of the first.
func (s *solver) storeSeenAdd(src, dst domain.Key) bool {
	return s.sumSeen.add(s.a, uint64(src)|sStore<<60, uint64(dst))
}

func (s *solver) addHeapStore(k domain.Key) {
	if s.sumSeen.add(s.a, sHeapStore, uint64(k)) {
		s.heapSt.push(s.a, uint64(k))
	}
}

func (s *solver) addRes(r int, k domain.Key, field domain.PathID, fieldGE bool) {
	f := uint64(field)
	if fieldGE {
		f |= 1 << 32
	}
	if s.sumSeen.add(s.a, sRes|uint64(r)<<8|f<<27, uint64(k)) {
		s.res.push(s.a, uint64(r))
		s.res.push(s.a, uint64(k))
		s.res.push(s.a, f)
	}
}

func (s *solver) addResHeap(r int, counted bool, field domain.PathID) {
	c := uint64(0)
	if counted {
		c = 1
	}
	if s.sumSeen.add(s.a, sResHeap|uint64(r)<<8, c|uint64(field)<<1) {
		s.resHeap.push(s.a, uint64(r)<<1|c)
		s.resHeap.push(s.a, uint64(field))
	}
}

func (s *solver) addOuter(r int) {
	if s.sumSeen.add(s.a, sOuter|uint64(r)<<8, 0) {
		s.outer.push(s.a, uint64(r))
	}
}

// summary builds f's summary from the collected entries and widens it.
func (s *solver) summary() *summary.Summary {
	sum := &summary.Summary{Inputs: len(s.f.Inputs)}
	reason := func(why uint64) summary.Reason {
		pos, kind, sink, site, callee, entry := s.whyAt(why)
		if kind != whyCall {
			return summary.Reason{Site: ^uint32(0), Callee: ^uint32(0), Entry: ^uint32(0), Pos: uint32(pos), Sink: uint8(sink)}
		}
		return summary.Reason{Site: uint32(site), Callee: uint32(callee), Entry: uint32(int32(entry)), Pos: uint32(pos), Sink: uint8(sink)}
	}
	var escWhy, storeWhy []summary.Reason
	for i := 0; i < len(s.esc.s); i += 2 {
		sum.Esc = append(sum.Esc, domain.Key(s.esc.s[i]))
		escWhy = append(escWhy, reason(s.esc.s[i+1]))
	}
	for i := 0; i < len(s.store.s); i += 3 {
		sum.Store = append(sum.Store, summary.StorePair{Src: domain.Key(s.store.s[i]), Dst: domain.Key(s.store.s[i+1])})
		storeWhy = append(storeWhy, reason(s.store.s[i+2]))
	}
	for _, k := range s.heapSt.s {
		sum.HeapStore = append(sum.HeapStore, domain.Key(k))
	}
	if s.f.Results > 0 {
		sum.Res = make([]summary.Result, s.f.Results)
	}
	for i := 0; i < len(s.res.s); i += 3 {
		r := int(s.res.s[i])
		if r >= len(sum.Res) {
			panic("internal compiler error: escape2/solve: return of result past the function's results")
		}
		f := s.res.s[i+2]
		sum.Res[r].From = append(sum.Res[r].From, summary.ResultKey{Key: domain.Key(s.res.s[i+1]), Field: domain.PathID(uint32(f)), FieldGE: f>>32 != 0})
	}
	for i := 0; i < len(s.resHeap.s); i += 2 {
		w, field := s.resHeap.s[i], domain.PathID(s.resHeap.s[i+1])
		r := int(w >> 1)
		if w&1 != 0 {
			sum.Res[r].CountedHeap = append(sum.Res[r].CountedHeap, field)
		} else {
			sum.Res[r].Heap = append(sum.Res[r].Heap, field)
		}
	}
	sum.Region.Builds = s.builds
	for _, r := range s.outer.s {
		sum.Region.OuterResults = append(sum.Region.OuterResults, int(r))
	}
	sum.Deferred = s.deferredOut()
	if len(escWhy)+len(storeWhy) > 0 {
		sum.Reasons = append(escWhy, storeWhy...)
	}
	if s.check {
		s.unwidened = cloneSummary(sum)
	}
	summary.Widen(s.ps, sum)
	if s.derivs {
		s.derivOut = s.derivations(sum, reason)
	}
	return sum
}

// derivations gives each Esc and Store entry of sum the reasons of every
// derivation it covers (an entry Widen cut covers the ones it absorbed).
func (s *solver) derivations(sum *summary.Summary, reason func(uint64) summary.Reason) [][]summary.Reason {
	out := make([][]summary.Reason, 0, len(sum.Esc)+len(sum.Store))
	for _, k := range sum.Esc {
		var rs []summary.Reason
		for i := 0; i < len(s.escDerivs.s); i += 2 {
			if domain.Covers(s.ps, k, domain.Key(s.escDerivs.s[i])) {
				rs = append(rs, reason(s.escDerivs.s[i+1]))
			}
		}
		out = append(out, rs)
	}
	for _, p := range sum.Store {
		var rs []summary.Reason
		for i := 0; i < len(s.storeDerivs.s); i += 3 {
			a, b := domain.Key(s.storeDerivs.s[i]), domain.Key(s.storeDerivs.s[i+1])
			if domain.Covers(s.ps, p.Src, a) && domain.Covers(s.ps, p.Dst, b) {
				rs = append(rs, reason(s.storeDerivs.s[i+2]))
			}
		}
		out = append(out, rs)
	}
	return out
}

// --- building the constraints from the instructions ---

func (s *solver) steps(path []int) []domain.Step {
	s.stepBuf = eirSteps(path, s.stepBuf)
	return s.stepBuf
}

func (s *solver) build() {
	f := s.f
	for b, blk := range f.Blocks {
		for i := range blk.Instrs {
			in := &blk.Instrs[i]
			if in.Dst != 0 {
				s.defs.s[in.Dst] = uint64(b)<<32 | uint64(i+1)
			}
			if in.Op == eir.OpCall && in.Call != nil && int(in.Call.Site) < f.NumSites {
				s.siteBlock.s[in.Call.Site] = uint64(b) + 1
			}
		}
	}
	for b, blk := range f.Blocks {
		for i := range blk.Instrs {
			s.instr(eir.BlockID(b), i, &blk.Instrs[i])
		}
	}
}

func (s *solver) instr(b eir.BlockID, i int, in *eir.Instr) {
	dst := uint32(in.Dst)
	own := func(sink eir.SinkKind) uint64 { return s.why(in.Pos, whyOwn, sink, 0, 0, 0) }
	switch in.Op {
	case eir.OpAddr:
		o := uint32(in.Obj)
		if l, _ := s.life(o); l == eir.LifeHeap {
			s.addTok(dst, heapTok)
			return
		}
		p, cut := s.pathOf(s.steps(in.Path))
		s.addTok(dst, tok{kind: tObj, base: o, path: p, ge: cut})
	case eir.OpHeap:
		if in.Heap == eir.HeapCounted || in.Heap == eir.HeapCountedCurrent {
			s.addTok(dst, tok{kind: tCHeap})
		} else {
			s.addTok(dst, heapTok)
		}
	case eir.OpInput:
		s.addTok(dst, tok{kind: tIn, base: uint32(in.Index)})
	case eir.OpLoad:
		if in.X == 0 || dst == 0 {
			return
		}
		if s.heapOnlyLoad(in.X) {
			s.addTok(dst, heapTok) // whatever X holds (rules.go, OpLoad)
			return
		}
		s.addCons(uint32(in.X), cLoad, uint64(dst), 0)
	case eir.OpStore:
		if in.X == 0 || in.Y == 0 {
			return
		}
		s.addCons(uint32(in.X), cStore, uint64(in.Y), own(0))
	case eir.OpField:
		if slices.Contains(in.Path, eir.AnyStep) {
			// An offset that is not known (unsafe.Add, a reinterpreted
			// pointer): anywhere in the object (rules.go, OpField).
			s.edge(in.X, dst, xw(xAny, 0, false))
			return
		}
		p, cut := s.pathOf(s.steps(in.Path))
		s.edge(in.X, dst, xw(xOff, p, cut))
	case eir.OpElem:
		p, _ := s.pathOf([]domain.Step{domain.StepElem})
		s.edge(in.X, dst, xw(xOff, p, false))
	case eir.OpSelect:
		p, cut := s.pathOf(s.steps(in.Path))
		if cut {
			s.edge(in.X, dst, xw(xRetag, 0, false))
		} else {
			s.edge(in.X, dst, xw(xSel, p, false))
		}
	case eir.OpCompose:
		for k, a := range in.Args {
			var p domain.PathID
			cut := false
			if k < len(in.Fields) {
				p, cut = s.pathOf(s.steps(in.Fields[k]))
			}
			s.edge(a, dst, xw(xPre, p, cut))
		}
	case eir.OpCopy:
		s.edge(in.X, dst, xw(xID, 0, false))
	case eir.OpAssert:
		if directAssert(in.Type) {
			// The value is a direct one: never a box (rules.go, OpAssert).
			s.edge(in.X, dst, xw(xNoBox, 0, false))
		} else if assertIface(in.Type) != nil {
			// Only a box whose type implements the interface (rules.go,
			// OpAssert).
			s.assertT = append(s.assertT, in.Type)
			s.edge(in.X, dst, xw(xImpl, domain.PathID(len(s.assertT)-1), false))
		} else {
			s.edge(in.X, dst, xw(xID, 0, false))
		}
	case eir.OpPhi:
		for _, a := range in.Args {
			s.edge(a, dst, xw(xID, 0, false))
		}
		s.backEdges(b, in, own(0))
	case eir.OpConv:
		if in.Obj == eir.NoObj || !BoxNeeded(s.env, &s.f.Objects[in.Obj]) {
			s.edge(in.X, dst, xw(xID, 0, false))
			return
		}
		s.storeInto(uint32(in.Obj), nil, uint32(in.X), own(0))
		s.objResult(dst, uint32(in.Obj))
	case eir.OpClosure:
		for k, a := range in.Args {
			s.storeInto(uint32(in.Obj), []domain.Step{domain.Step(k)}, uint32(a), own(0))
		}
		s.objResult(dst, uint32(in.Obj))
	case eir.OpMethodVal:
		s.storeInto(uint32(in.Obj), []domain.Step{0}, uint32(in.X), own(0))
		s.objResult(dst, uint32(in.Obj))
	case eir.OpSink:
		if in.X == 0 {
			return
		}
		switch in.Sink {
		case eir.SinkReturn:
			s.addCons(uint32(in.X), cReturn, uint64(in.Index), own(in.Sink))
		case eir.SinkGlobal:
			s.addCons(uint32(in.X), cSink, cE3002, own(in.Sink))
		case eir.SinkSend:
			s.addCons(uint32(in.X), cSink, cE3005, own(in.Sink))
		case eir.SinkTask:
			s.addCons(uint32(in.X), cSink, cE3004, own(in.Sink))
		case eir.SinkExternal:
			s.addCons(uint32(in.X), cSink, cE3003, own(in.Sink))
		default:
			panic("internal compiler error: escape2/solve: unknown sink kind")
		}
	case eir.OpDrop:
		if int(in.Obj) < len(s.f.Objects) && s.f.Objects[in.Obj].Life == eir.LifeStmt {
			s.drops.push(s.a, uint64(b)<<32|uint64(i))
		}
	case eir.OpCall:
		if ownDeferrable(in) && s.env.Deferred(s.f, in.Call) {
			s.ownDcall(in) // defer.go
		} else {
			s.call(b, in)
		}
	default:
		panic("internal compiler error: escape2/solve: unknown EIR instruction")
	}
}

func (s *solver) edge(x eir.Reg, dst uint32, w uint64) {
	if x == 0 || dst == 0 {
		return
	}
	s.addEdge(uint32(x), dst, w, 0)
}

// objResult puts a pointer to the whole of object o in dst.
func (s *solver) objResult(dst uint32, o uint32) {
	if dst == 0 {
		return
	}
	if o == uint32(eir.NoObj) {
		s.addTok(dst, heapTok) // a func value with no record: static
		return
	}
	if l, _ := s.life(o); l == eir.LifeHeap {
		s.addTok(dst, heapTok)
		return
	}
	s.addTok(dst, tok{kind: tObj, base: o})
}

// backEdges attaches the back-edge check to the arguments of a phi in a
// loop header (rules.go, "Back-edge check").
func (s *solver) backEdges(b eir.BlockID, in *eir.Instr, why uint64) {
	blk := s.f.Blocks[b]
	for l, loop := range s.f.Loops {
		if loop.Header != b {
			continue
		}
		for k, a := range in.Args {
			if a == 0 || k >= len(blk.Preds) {
				continue
			}
			if s.nested(s.f.Blocks[blk.Preds[k]].Loop, l) {
				s.addCons(uint32(a), cBack, uint64(l), why)
			}
		}
	}
}

// BoxNeeded reports whether the box object of a conversion is made: an
// interface box (eir.ObjBox) whose type is a type parameter is made only for
// a non-direct instance (the lowerer converts each instance on its own), so
// when every instance the program makes is direct (pointer-shaped, or a
// scalar of at most a word) there is no box and the interface holds the
// value itself (rules.go, OpConv).
func BoxNeeded(env Env, o *eir.Object) bool {
	if o.Kind != eir.ObjBox {
		return true
	}
	tp, ok := types2.Unalias(o.Type).(*types2.TypeParam)
	if !ok {
		return true
	}
	insts, known := env.Instantiated(tp)
	if !known {
		return true
	}
	for _, T := range insts {
		if !directValue(T) {
			return true
		}
	}
	return false
}

// directValue reports whether an interface holds a value of type T in its
// data word, unboxed: pointer-shaped, or a scalar of at most a word; or
// whether T has no bytes, so its box holds no memory to outlive anything (a
// zero-sized value).
func directValue(T types2.Type) bool {
	if types2.PointerShaped(T) || zeroSized(T) {
		return true
	}
	b, ok := T.Underlying().(*types2.Basic)
	if !ok {
		return false
	}
	switch b.Kind() {
	case types2.String, types2.Complex64, types2.Complex128, types2.UntypedString, types2.UntypedComplex, types2.UntypedNil, types2.Invalid:
		return false
	}
	return true
}

// zeroSized reports whether a value of T has no bytes: an empty struct, an
// array of none, or of zero-sized elements.
func zeroSized(T types2.Type) bool {
	switch u := T.Underlying().(type) {
	case *types2.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if !zeroSized(u.Field(i).Type()) {
				return false
			}
		}
		return true
	case *types2.Array:
		return u.Len() == 0 || zeroSized(u.Elem())
	}
	return false
}

// directAssert reports whether an assertion to T yields the interface's data
// word itself, a pointer-shaped value, which can never be a box the
// conversion made (an interface holds a direct value unboxed).
func directAssert(T types2.Type) bool {
	if T == nil {
		return false
	}
	if _, ok := T.Underlying().(*types2.Interface); ok {
		return false
	}
	return types2.PointerShaped(T)
}

// DirectAssert is directAssert, for the reference solver.
func DirectAssert(T types2.Type) bool { return directAssert(T) }

// assertIface returns the interface an assertion to T asserts, or nil when
// T is not an interface or mentions a type parameter.
func assertIface(T types2.Type) *types2.Interface {
	if T == nil || mentionsTypeParam(T, nil) {
		return nil
	}
	i, _ := T.Underlying().(*types2.Interface)
	return i
}

// BoxAsserts reports whether the box o can be the operand of an assertion
// to the interface type T that succeeds: o is not an interface box
// (eir.ObjBox), or its dynamic type, the box's Type, implements T, or that
// type is not known exactly (it mentions a type parameter). An assertion to
// an interface leaves out the boxes it cannot yield (rules.go, OpAssert).
func BoxAsserts(o *eir.Object, T types2.Type) bool {
	if o.Kind != eir.ObjBox || o.Type == nil {
		return true
	}
	i := assertIface(T)
	if i == nil || mentionsTypeParam(o.Type, nil) {
		return true
	}
	return types2.Implements(o.Type, i)
}

// mentionsTypeParam reports whether T mentions a type parameter.
func mentionsTypeParam(T types2.Type, seen map[*types2.Named]bool) bool {
	switch t := types2.Unalias(T).(type) {
	case *types2.TypeParam:
		return true
	case *types2.Basic, nil:
		return false
	case *types2.Pointer:
		return mentionsTypeParam(t.Elem(), seen)
	case *types2.Slice:
		return mentionsTypeParam(t.Elem(), seen)
	case *types2.Array:
		return mentionsTypeParam(t.Elem(), seen)
	case *types2.Chan:
		return mentionsTypeParam(t.Elem(), seen)
	case *types2.Map:
		return mentionsTypeParam(t.Key(), seen) || mentionsTypeParam(t.Elem(), seen)
	case *types2.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if mentionsTypeParam(t.Field(i).Type(), seen) {
				return true
			}
		}
		return false
	case *types2.Tuple:
		for i := 0; i < t.Len(); i++ {
			if mentionsTypeParam(t.At(i).Type(), seen) {
				return true
			}
		}
		return false
	case *types2.Signature:
		return mentionsTypeParam(t.Params(), seen) || mentionsTypeParam(t.Results(), seen)
	case *types2.Named:
		if t.TypeParams().Len() > 0 && t.TypeArgs().Len() == 0 {
			return true
		}
		for i := 0; i < t.TypeArgs().Len(); i++ {
			if mentionsTypeParam(t.TypeArgs().At(i), seen) {
				return true
			}
		}
		return false
	case *types2.Interface:
		for i := 0; i < t.NumMethods(); i++ {
			if mentionsTypeParam(t.Method(i).Type(), seen) {
				return true
			}
		}
		return false
	}
	return true // a type this does not know: as if it did
}

// heapOnlyLoad reports whether a load through x reads a heap-only field
// (rules.go, OpLoad): x is the address of a field of a struct, by OpAddr or
// OpField with the type of what it offsets.
func (s *solver) heapOnlyLoad(x eir.Reg) bool {
	d := s.defs.s[x]
	if d == 0 {
		return false
	}
	in := &s.f.Blocks[d>>32].Instrs[uint32(d)-1]
	var T types2.Type
	switch in.Op {
	case eir.OpAddr:
		if int(in.Obj) < len(s.f.Objects) {
			T = s.f.Objects[in.Obj].Type
		}
	case eir.OpField:
		T = in.Type
	}
	if T == nil || len(in.Path) == 0 {
		return false
	}
	S, field, ok := fieldClass(T, in.Path)
	return ok && s.env.HeapOnly(S, field)
}

// fieldClass walks T along path to the struct type and field index of the
// last step.
func fieldClass(T types2.Type, path []int) (types2.Type, int, bool) {
	for i, st := range path {
		u := T.Underlying()
		last := i == len(path)-1
		if st == eir.ElemStep {
			if last {
				return nil, 0, false
			}
			switch a := u.(type) {
			case *types2.Array:
				T = a.Elem()
			case *types2.Slice:
				T = a.Elem()
			default:
				return nil, 0, false
			}
			continue
		}
		str, ok := u.(*types2.Struct)
		if !ok || st < 0 || st >= str.NumFields() {
			return nil, 0, false
		}
		if last {
			return T, st, true
		}
		T = str.Field(st).Type()
	}
	return nil, 0, false
}

// cloneSummary copies sum, sharing no slice with it.
func cloneSummary(sum *summary.Summary) *summary.Summary {
	c := *sum
	c.Esc = slices.Clone(sum.Esc)
	c.Store = slices.Clone(sum.Store)
	c.HeapStore = slices.Clone(sum.HeapStore)
	c.Reasons = slices.Clone(sum.Reasons)
	c.Deferred = slices.Clone(sum.Deferred)
	c.Region.OuterResults = slices.Clone(sum.Region.OuterResults)
	c.Res = make([]summary.Result, len(sum.Res))
	for i, r := range sum.Res {
		c.Res[i] = summary.Result{From: slices.Clone(r.From), Heap: slices.Clone(r.Heap), CountedHeap: slices.Clone(r.CountedHeap)}
	}
	return &c
}
