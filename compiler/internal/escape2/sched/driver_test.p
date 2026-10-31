// Ported from internal/escape2/sched/driver_test.go at the go-cutover tag.

package sched

import (
	"pluto/slices"
	"pluto/testing"

	"compiler/compat"
	"compiler/internal/escape2/domain"
	"compiler/internal/escape2/eir"
	"compiler/internal/escape2/solve"
	"compiler/internal/escape2/summary"
	"compiler/internal/scratch"
	"compiler/internal/syntax/token"
	"compiler/internal/testpanic"
	"compiler/pu"
)

// Stand-ins for the domain and the solver, so that the driver is tested on
// its own.

type fakePaths struct{}

func (fakePaths) Extend(p domain.PathID, s domain.Step) (domain.PathID, bool) { return p, false }
func (fakePaths) Steps(p domain.PathID) []domain.Step                         { return nil }
func (fakePaths) Loads(p domain.PathID) int                                   { return 0 }
func (fakePaths) HasPrefix(p, prefix domain.PathID) bool                      { return p == prefix }
func (fakePaths) Cut(p domain.PathID) domain.PathID                           { return p }
func (fakePaths) Filters() *domain.Filters                                    { return &fakeFilters }

var fakeFilters domain.Filters

func (fakePaths) Len() int { return 1 }

type fakeArena struct {
	bytes, peak, resets int
	fresh               int // new arenas: the fake newArena gives this one, emptied
	releases            int
}

func (a *fakeArena) Alloc(n int) []uint64 {
	a.bytes += 8 * n
	if a.bytes > a.peak {
		a.peak = a.bytes
	}
	return compat.Make[uint64](n)
}
func (a *fakeArena) Bytes() int     { return a.bytes }
func (a *fakeArena) Peak() int      { return a.peak }
func (a *fakeArena) Allocated() int { return a.bytes }
func (a *fakeArena) Reset()         { a.bytes = 0; a.resets++ }
func (a *fakeArena) Release()       { a.bytes = 0; a.releases++ }

type fakeSets struct{ a *fakeArena }

func (s fakeSets) Intern(elems []uint64) domain.SetID   { return 0 }
func (s fakeSets) Elems(domain.SetID) []uint64          { return nil }
func (s fakeSets) Union(a, b domain.SetID) domain.SetID { return 0 }
func (s fakeSets) Diff(a, b domain.SetID) domain.SetID  { return 0 }
func (s fakeSets) Has(domain.SetID, uint64) bool        { return false }
func (s fakeSets) Len(domain.SetID) int                 { return 0 }
func (s fakeSets) Bytes() int                           { return 0 }
func (s fakeSets) Reset()                               {}

// The state of the fakes, which the Go test's closures captured: a function
// stored in solveFunc or newArena cannot capture a test's frame, so the
// order of the solves and the shared arena are package variables.
var (
	fakeOrder []eir.FuncID
	fakeArn   *fakeArena
)

// fakeSolve gives each function the keys of its callees' summaries plus
// one key of its own (its ID), and a finding at its position; it uses the
// arena in proportion to the keys it holds. It records the order of the
// solves in fakeOrder.
//
// The driver runs each solve in scratch memory (scratch.Do), freed when the
// solve ends, so what a fake adds to a package variable comes from the
// caller's memory (scratch.Outer), as the real solver's shared tables do.
func fakeSolve(f *eir.Func, env solve.Env, ws *solve.Workspace) solve.Result {
	r := scratch.Outer()
	fakeOrder = compat.Append(fakeOrder, f.ID)
	r.Restore()
	keys := map[domain.Key]bool{domain.Key(f.ID): true}
	for _, b := range f.Blocks {
		for _, in := range b.Instrs {
			if in.Op != eir.OpCall {
				continue
			}
			callees := funcIDs(in.Call.Callee)
			if in.Call.Kind != eir.CallStatic {
				callees = env.Candidates(f, in.Call)
			}
			for _, id := range callees {
				if s := env.Summary(id); s != nil {
					for _, k := range s.Esc {
						keys[k] = true
					}
				}
			}
		}
	}
	s := pu.New(summary.Summary{})
	for k := range keys {
		s.Esc = compat.Append(s.Esc, k)
	}
	slices.Sort(s.Esc)
	ws.Arena.Alloc(len(s.Esc))
	return solve.Result{Summary: s, Findings: compat.Append[solve.Finding](nil, solve.Finding{Pos: f.Pos, Func: f.ID})}
}

func fakeLeq(_ domain.Paths, s, t *summary.Summary) bool {
	have := map[domain.Key]bool{}
	for _, k := range t.Esc {
		have[k] = true
	}
	for _, k := range s.Esc {
		if !have[k] {
			return false
		}
	}
	return true
}

// fakeJoin is the union of two fake summaries' Esc keys.
func fakeJoin(_ domain.Paths, a, b *summary.Summary) *summary.Summary {
	have := map[domain.Key]bool{}
	j := pu.New(summary.Summary{})
	for _, s := range []*summary.Summary{a, b} {
		for _, k := range s.Esc {
			if !have[k] {
				have[k] = true
				j.Esc = compat.Append(j.Esc, k)
			}
		}
	}
	slices.Sort(j.Esc)
	return j
}

func fakeNewPaths() domain.Paths { return fakePaths{} }

func fakeNewArena() domain.Arena {
	fakeArn.bytes = 0
	fakeArn.fresh++
	return fakeArn
}

func fakeNewSets(a domain.Arena) domain.Sets { return fakeSets{a: fakeArn} }

// The final pass of an SCC (reasons.go) solves as the fakes do.
func fakeSolveDerivs(f *eir.Func, env solve.Env, ws *solve.Workspace) solve.Result {
	return solveFunc(f, env, ws)
}

// What withFakes replaced, for restoreFakes.
var (
	savedSolve     func(*eir.Func, solve.Env, *solve.Workspace) solve.Result
	savedLeq       func(domain.Paths, *summary.Summary, *summary.Summary) bool
	savedNewPaths  func() domain.Paths
	savedNewArena  func() domain.Arena
	savedNewSets   func(domain.Arena) domain.Sets
	savedJoinSums  func(domain.Paths, *summary.Summary, *summary.Summary) *summary.Summary
	savedKeep      func(f *eir.Func, env solve.Env, ws *solve.Workspace) (solve.Result, resumable)
	savedDerivs    func(*eir.Func, solve.Env, *solve.Workspace) solve.Result
	savedTestJobs  int
	fakesInstalled bool
)

// withFakes installs the fakes; the test restores what they replaced with
// "defer restoreFakes()" (the Go test's t.Cleanup). The order of the solves
// starts empty in fakeOrder.
func withFakes(t *testing.T) *fakeArena {
	if fakesInstalled {
		t.Fatalf("withFakes: the fakes of an earlier test were not restored")
	}
	fakesInstalled = true
	// The fakes share one arena and record the order of the solves: one
	// worker.
	savedTestJobs = testJobs
	testJobs = 1
	fakeArn = pu.New(fakeArena{})
	fakeOrder = nil
	savedSolve, savedLeq, savedNewPaths, savedNewArena, savedNewSets, savedJoinSums = solveFunc, leq, newPaths, newArena, newSets, joinSums
	solveFunc = fakeSolve
	leq = fakeLeq
	joinSums = fakeJoin
	newPaths = fakeNewPaths
	newArena = fakeNewArena
	newSets = fakeNewSets
	// The fake solves keep no state: the real solver's resumable solve
	// would solve the fake functions for real.
	savedKeep, savedDerivs = solveKeepFunc, solveDerivsFunc
	solveKeepFunc = nil
	solveDerivsFunc = fakeSolveDerivs
	return fakeArn
}

func restoreFakes() {
	solveKeepFunc, solveDerivsFunc = savedKeep, savedDerivs
	solveFunc = savedSolve
	leq = savedLeq
	newPaths = savedNewPaths
	newArena = savedNewArena
	newSets = savedNewSets
	joinSums = savedJoinSums
	testJobs = savedTestJobs
	fakesInstalled = false
}

// The SCCs and their stats Options.OnSCC records, which the Go tests'
// closures appended to their locals: RunWith keeps opts, so OnSCC cannot
// use a test's frame.
var (
	sccStatsSCCs  [][]eir.FuncID
	sccStatsStats []SCCStats
)

func recordSCC(scc []eir.FuncID, st SCCStats) {
	r := scratch.Outer()
	defer r.Restore()
	sccStatsSCCs = compat.Append(sccStatsSCCs, compat.Clone(scc))
	sccStatsStats = compat.Append(sccStatsStats, st)
}

// TestDriver's expected keys: every function's final summary holds its own
// key and its callees'. (A package variable: a map literal in the test
// would keep the slice literals of its frame.)
var driverWant = map[string][]string{
	"c": {"c"}, "a": {"a", "b", "c"}, "b": {"a", "b", "c"},
	"d": {"d", "e", "f"}, "e": {"d", "e", "f"}, "f": {"d", "e", "f"},
	"main": {"main", "a", "b", "c", "d", "e", "f"},
}

// TestDriver checks the per-SCC driver: callees first, iteration to a fixed
// point inside a recursive SCC, findings sorted, summaries copied out and
// the arena reset after each SCC.
func TestDriver(t *testing.T) {
	arena := withFakes(t)
	defer restoreFakes()
	pr := newProg()
	// main → a ⇄ b → c, a 3-cycle d → e → f → d called from main.
	names := []string{"main", "a", "b", "c", "d", "e", "f"}
	fs := map[string]*fbuild{}
	for i, n := range names {
		fs[n] = pr.fn(n, eir.FuncKey{}, nil)
		fs[n].f.Pos = token.Pos(100 - i)
	}
	fs["main"].callStatic(fs["a"])
	fs["main"].callStatic(fs["d"])
	fs["a"].callStatic(fs["b"])
	fs["b"].callStatic(fs["a"])
	fs["b"].callStatic(fs["c"])
	fs["d"].callStatic(fs["e"])
	fs["e"].callStatic(fs["f"])
	fs["f"].callStatic(fs["d"])

	sccStatsSCCs, sccStatsStats = nil, nil
	res := RunWith(pr.p, Options{OnSCC: recordSCC})
	sccs, stats := sccStatsSCCs, sccStatsStats

	if res.Stats.SCCs != 4 || res.Stats.LargestSCC != 3 || len(sccs) != 4 {
		t.Fatalf("stats %+v, sccs %v", res.Stats, sccs)
	}
	// Every solve starts with an empty workspace: reset (the sequential
	// driver) or new (the parallel one).
	if arena.resets+arena.fresh < res.Stats.Iterations {
		t.Errorf("arena reset %d times and new %d times, want once per solve (%d)", arena.resets, arena.fresh, res.Stats.Iterations)
	}
	// Every function's final summary holds its own key and its callees'
	// (driverWant).
	for n, ks := range driverWant {
		s := res.Summaries[fs[n].f.ID]
		if s == nil || len(s.Esc) != len(ks) {
			t.Errorf("%s: summary %v, want keys of %v", n, s, ks)
		}
	}
	// main is solved last, once.
	order := fakeOrder
	if order[len(order)-1] != fs["main"].f.ID {
		t.Errorf("main not solved last: %v", order)
	}
	// Findings are sorted by position.
	for i := 1; i < len(res.Findings); i++ {
		if res.Findings[i-1].Pos > res.Findings[i].Pos {
			t.Errorf("findings not sorted: %v", res.Findings)
		}
	}
	if len(res.Findings) != len(names) {
		t.Errorf("%d findings, want one per function", len(res.Findings))
	}
	if res.Stats.PeakArena == 0 || res.Stats.Iterations < len(names) {
		t.Errorf("stats %+v", res.Stats)
	}
	for i, st := range stats {
		if st.Funcs != len(sccs[i]) || st.Solves < st.Funcs {
			t.Errorf("SCC %v: stats %+v", sccs[i], st)
		}
	}
}

// fakeKept is a kept solve of the fake solver: resuming solves again. The
// count of resumes is fakeResumes (the Go test's resumes pointer, to a
// local of the test).
type fakeKept struct {
	f     *eir.Func
	ws    *solve.Workspace
	bytes int
}

var fakeResumes, fakeKeeps int

func (k *fakeKept) Resume(env solve.Env) solve.Result {
	fakeResumes++
	return solveFunc(k.f, env, k.ws)
}

func (k *fakeKept) Bytes() int { return k.bytes }

func fakeSolveKeep(f *eir.Func, env solve.Env, ws *solve.Workspace) (solve.Result, resumable) {
	fakeKeeps++
	return solveFunc(f, env, ws), pu.New(fakeKept{f: f, ws: ws, bytes: 10})
}

func buildResume() (*prog, map[string]*fbuild) {
	pr := newProg()
	fs := map[string]*fbuild{}
	for _, n := range []string{"main", "a", "b", "c"} {
		fs[n] = pr.fn(n, eir.FuncKey{}, nil)
	}
	fs["main"].callStatic(fs["a"])
	fs["a"].callStatic(fs["b"])
	fs["b"].callStatic(fs["c"])
	fs["c"].callStatic(fs["a"])
	return pr, fs
}

// TestDriverResume checks incremental re-solving in the driver: members of
// an SCC are kept and resumed, the results are those of solving from
// scratch, the states are released when the SCC is final, and past the
// budget the largest are dropped.
func TestDriverResume(t *testing.T) {
	withFakes(t)
	defer restoreFakes()
	// Kept states are the sequential driver's (the parallel one solves from
	// scratch: parallel.go).
	savedSeq := sequential
	sequential = true
	defer func() { sequential = savedSeq }()
	pr, _ := buildResume()
	want := RunWith(pr.p, Options{})

	for _, budget := range []int{1 << 30, 1} {
		fakeResumes, fakeKeeps = 0, 0
		saved, savedBudget := solveKeepFunc, retainBudget
		solveKeepFunc = fakeSolveKeep
		retainBudget = budget
		sccStatsSCCs, sccStatsStats = nil, nil
		pr, fs := buildResume()
		got := RunWith(pr.p, Options{OnSCC: recordSCC})
		stats := sccStatsStats
		solveKeepFunc, retainBudget = saved, savedBudget
		resumes, keeps := fakeResumes, fakeKeeps

		for _, n := range []string{"a", "b", "c", "main"} {
			id := fs[n].f.ID
			if g, w := got.Summaries[id], want.Summaries[id]; len(g.Esc) != len(w.Esc) {
				t.Errorf("budget %d: %s: %d keys, want %d as from scratch", budget, n, len(g.Esc), len(w.Esc))
			}
		}
		var cycle SCCStats
		for _, st := range stats {
			if st.Funcs == 3 {
				cycle = st
			}
		}
		if budget > 1 {
			if keeps != 3 || resumes == 0 || cycle.Resumes != resumes || cycle.RetainedPeak != 30 {
				t.Errorf("keeps %d resumes %d stats %+v: want 3 kept, resumed, 30 bytes at the peak", keeps, resumes, cycle)
			}
		} else if resumes != 0 || cycle.RetainedPeak != 0 {
			// Every state is past a budget of one byte: dropped at once,
			// so every solve is a new kept solve and none resumes.
			t.Errorf("budget 1: resumes %d stats %+v, want none kept", resumes, cycle)
		}
	}
}

// TestSCCWorkOrder checks the SCC worklist: callees first by a postorder of
// the SCC's own calls, each function queued once, others ignored.
func TestSCCWorkOrder(t *testing.T) {
	// a calls b, b calls c, c calls a; c also calls d outside the SCC.
	deps := map[eir.FuncID][]eir.FuncID{0: funcIDs(1), 1: funcIDs(2), 2: funcIDs(0, 9)}
	w := newSCCWork(funcIDs(0, 1, 2), func(id eir.FuncID) []eir.FuncID { return deps[id] })
	var got []eir.FuncID
	for !w.empty() {
		got = compat.Append(got, w.pop())
	}
	if want := funcIDs(2, 1, 0); !sameIDs(got, want) {
		t.Errorf("order %v, want %v (callees first)", got, want)
	}
	w.push(0)
	w.push(2)
	w.push(0)
	w.push(9) // not in the SCC
	got = nil
	for !w.empty() {
		got = compat.Append(got, w.pop())
	}
	if want := funcIDs(2, 0); !sameIDs(got, want) {
		t.Errorf("after pushes: %v, want %v", got, want)
	}
}

// TestDriverLimit's solver state (the Go test's closure captured it).
var (
	limitBig       eir.FuncID
	limitCallerSaw *summary.Summary
)

func limitSolve(f *eir.Func, env solve.Env, ws *solve.Workspace) solve.Result {
	if f.ID == limitBig {
		return solve.Result{Limit: pu.New(solve.LimitError{Func: f.Name, Bytes: 1 << 30})}
	}
	limitCallerSaw = env.Summary(limitBig)
	return fakeSolve(f, env, ws)
}

func notALimitSolve(f *eir.Func, env solve.Env, ws *solve.Workspace) solve.Result {
	panic("not a limit")
}

// TestDriverLimit checks a function past the solver's limits: it gets an
// E3010 finding and the top summary, its callers are solved against that,
// and the analysis goes on. A panic that is not a limit is not caught.
func TestDriverLimit(t *testing.T) {
	withFakes(t)
	defer restoreFakes()
	pkg := typecheck(t, `
func big(p *int, q *int) *int { return p }
func caller() {}
`)
	pr := newProg()
	big := pr.decl(lookupFunc(t, pkg, "big"))
	caller := pr.decl(lookupFunc(t, pkg, "caller"))
	caller.callStatic(big, caller.heap(), caller.heap())
	limitBig, limitCallerSaw = big.f.ID, nil
	solveFunc = limitSolve
	res := RunWith(pr.p, Options{})
	callerSaw := limitCallerSaw
	var lim []solve.Finding
	for _, fd := range res.Findings {
		if fd.Code == codeEscapeLimit {
			lim = compat.Append(lim, fd)
		}
	}
	if len(lim) != 1 || lim[0].Func != big.f.ID {
		t.Fatalf("findings %+v, want one E3010 at big", res.Findings)
	}
	s := res.Summaries[big.f.ID]
	if s == nil || s.Inputs != 2 || len(s.Esc) != 2 || !s.Esc[0].GE() || len(s.Res) != 1 || len(s.Res[0].From) != 2 || !s.Region.Builds {
		t.Errorf("big's summary %+v, want the top summary", s)
	}
	if callerSaw == nil || len(callerSaw.Esc) != 2 {
		t.Errorf("caller solved against %+v, want big's top summary", callerSaw)
	}

	// Any other panic goes on up. (Plutonium has no recover: the run is in
	// a child process, and the test checks that it panics, not the value.)
	solveFunc = notALimitSolve
	if !testpanic.Panics(func() { RunWith(pr.p, Options{}) }) {
		t.Errorf("the run returned, want the panic to go on up")
	}
}

// TestDriverLimitOnce's solver state: whether big has been past the limit.
var limitOnceHit bool

func limitOnceSolve(f *eir.Func, env solve.Env, ws *solve.Workspace) solve.Result {
	if f.ID == limitBig && !limitOnceHit {
		limitOnceHit = true
		return solve.Result{Limit: pu.New(solve.LimitError{Func: f.Name, Bytes: 1 << 30})}
	}
	return fakeSolve(f, env, ws)
}

func limitOnceDerivs(f *eir.Func, env solve.Env, ws *solve.Workspace) solve.Result {
	r := solveFunc(f, env, ws)
	r.Summary = nil
	return r
}

// TestDriverLimitOnce checks a member of a recursive SCC past the solver's
// limits in one solve of the iteration only: the join keeps the top summary
// it got, so its E3010 stays, and its later solves' findings (the frame
// errors the top summary makes) do not take its place. Both drivers.
func TestDriverLimitOnce(t *testing.T) {
	withFakes(t)
	defer restoreFakes()
	savedSeq := sequential
	defer func() { sequential = savedSeq }()
	pkg := typecheck(t, `
func big(p *int) { other(p) }
func other(p *int) { big(p) }
`)
	for _, seq := range []bool{true, false} {
		sequential = seq
		pr := newProg()
		big := pr.decl(lookupFunc(t, pkg, "big"))
		other := pr.decl(lookupFunc(t, pkg, "other"))
		big.callStatic(other, big.heap())
		other.callStatic(big, other.heap())
		limitBig, limitOnceHit = big.f.ID, false
		solveFunc = limitOnceSolve
		// The final pass gives no summary: big's is the top one, and
		// other's holds it, whose paths are real ones, which the fake
		// paths cannot hold for the derivations (summary.EntryDerivs).
		solveDerivsFunc = limitOnceDerivs
		res := RunWith(pr.p, Options{})
		if !limitOnceHit {
			t.Fatalf("sequential %v: big was never past the limit", seq)
		}
		var lim, bigOther int
		for _, fd := range res.Findings {
			switch {
			case fd.Code == codeEscapeLimit && fd.Func == big.f.ID:
				lim++
			case fd.Func == big.f.ID:
				bigOther++
			}
		}
		if lim != 1 || bigOther != 0 {
			t.Errorf("sequential %v: findings %+v, want only one E3010 at big", seq, res.Findings)
		}
		if res.Stats.Iterations < 3 {
			t.Errorf("sequential %v: %d solves, want big solved again after its limit", seq, res.Stats.Iterations)
		}
	}
}

// TestDriverUnscheduledRead's solver state.
var unschedA, unschedB eir.FuncID

func unschedSolve(f *eir.Func, env solve.Env, ws *solve.Workspace) solve.Result {
	if f.ID == unschedA {
		env.Summary(unschedB) // a does not call b
	}
	return solve.Result{Summary: pu.New(summary.Summary{})}
}

// TestDriverUnscheduledRead checks that a solver reading the summary of a
// function the graph did not order first is an internal error, not a
// silent bottom summary.
func TestDriverUnscheduledRead(t *testing.T) {
	withFakes(t)
	defer restoreFakes()
	pr := newProg()
	a := pr.fn("a", eir.FuncKey{}, nil)
	b := pr.fn("b", eir.FuncKey{}, nil)
	unschedA, unschedB = a.f.ID, b.f.ID
	solveFunc = unschedSolve
	b.callStatic(a)
	if !testpanic.Panics(func() { RunWith(pr.p, Options{}) }) {
		t.Errorf("no internal error")
	}
}

// TestDriverDynamicJoin's solver state.
var (
	dynMain     eir.FuncID
	dynCall     *eir.Call
	dynGotCands []eir.FuncID
)

func dynSolve(f *eir.Func, env solve.Env, ws *solve.Workspace) solve.Result {
	if f.ID == dynMain {
		r := scratch.Outer()
		dynGotCands = compat.Clone(env.Candidates(f, dynCall))
		r.Restore()
	}
	return fakeSolve(f, env, ws)
}

// TestDriverDynamicJoin checks that a dynamic call is given one virtual
// candidate whose summary joins its candidates', and that functions in a
// cycle through a dynamic call are solved to their fixed point.
func TestDriverDynamicJoin(t *testing.T) {
	withFakes(t)
	defer restoreFakes()
	pr := newProg()
	main := pr.fn("main", eir.FuncKey{}, nil)
	g1 := pr.fn("g1", eir.FuncKey{}, nil)
	g2 := pr.fn("g2", eir.FuncKey{}, nil)
	h := pr.fn("h", eir.FuncKey{}, nil)
	main.f.Facts.FuncValues = funcIDs(g1.f.ID, g2.f.ID)
	dyn := pu.New(eir.Call{Kind: eir.CallFunc, Target: main.heap()})
	main.call(dyn)
	g2.callStatic(h)
	// h calls back through a func value: g1, g2 and h form a cycle through
	// the dispatch node; main is outside it.
	hd := pu.New(eir.Call{Kind: eir.CallFunc, Target: h.heap()})
	h.call(hd)

	dynMain, dynCall, dynGotCands = main.f.ID, dyn, nil
	solveFunc = dynSolve
	res := RunWith(pr.p, Options{})
	gotCands := dynGotCands
	if len(gotCands) != 1 || int(gotCands[0]) < len(pr.p.Funcs) {
		t.Fatalf("candidates of the dynamic call: %v, want one virtual function", gotCands)
	}
	want := map[*fbuild]int{main: 4, g1: 1, g2: 3, h: 3}
	for fb, n := range want {
		if s := res.Summaries[fb.f.ID]; s == nil || len(s.Esc) != n {
			t.Errorf("%s: summary %+v, want %d keys", fb.f.Name, s, n)
		}
	}
}
