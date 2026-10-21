package sched

import (
	"sort"
	"testing"

	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/solve"
	"github.com/matjam/plutonium/internal/escape2/summary"
	"github.com/matjam/plutonium/internal/syntax/token"
)

// Stand-ins for the domain and the solver, so that the driver is tested on
// its own.

type fakePaths struct{}

func (fakePaths) Extend(p domain.PathID, s domain.Step) (domain.PathID, bool) { return p, false }
func (fakePaths) Steps(p domain.PathID) []domain.Step                         { return nil }
func (fakePaths) Loads(p domain.PathID) int                                   { return 0 }
func (fakePaths) HasPrefix(p, prefix domain.PathID) bool                      { return p == prefix }
func (fakePaths) Cut(p domain.PathID) domain.PathID                           { return p }
func (fakePaths) Len() int                                                    { return 1 }

type fakeArena struct {
	bytes, peak, resets int
}

func (a *fakeArena) Alloc(n int) []uint64 {
	a.bytes += 8 * n
	if a.bytes > a.peak {
		a.peak = a.bytes
	}
	return make([]uint64, n)
}
func (a *fakeArena) Bytes() int { return a.bytes }
func (a *fakeArena) Peak() int  { return a.peak }
func (a *fakeArena) Reset()     { a.bytes = 0; a.resets++ }

type fakeSets struct{ a *fakeArena }

func (s fakeSets) Intern(elems []uint64) domain.SetID   { return 0 }
func (s fakeSets) Elems(domain.SetID) []uint64          { return nil }
func (s fakeSets) Union(a, b domain.SetID) domain.SetID { return 0 }
func (s fakeSets) Diff(a, b domain.SetID) domain.SetID  { return 0 }
func (s fakeSets) Has(domain.SetID, uint64) bool        { return false }
func (s fakeSets) Len(domain.SetID) int                 { return 0 }
func (s fakeSets) Bytes() int                           { return 0 }
func (s fakeSets) Reset()                               {}

// fakeSolve gives each function the keys of its callees' summaries plus
// one key of its own (its ID), and a finding at its position; it uses the
// arena in proportion to the keys it holds.
func fakeSolve(order *[]eir.FuncID) func(f *eir.Func, env solve.Env, ws *solve.Workspace) solve.Result {
	return func(f *eir.Func, env solve.Env, ws *solve.Workspace) solve.Result {
		*order = append(*order, f.ID)
		keys := map[domain.Key]bool{domain.Key(f.ID): true}
		for _, b := range f.Blocks {
			for _, in := range b.Instrs {
				if in.Op != eir.OpCall {
					continue
				}
				callees := []eir.FuncID{in.Call.Callee}
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
		s := &summary.Summary{}
		for k := range keys {
			s.Esc = append(s.Esc, k)
		}
		sort.Slice(s.Esc, func(i, j int) bool { return s.Esc[i] < s.Esc[j] })
		ws.Arena.Alloc(len(s.Esc))
		return solve.Result{Summary: s, Findings: []solve.Finding{{Pos: f.Pos, Func: f.ID}}}
	}
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
	j := &summary.Summary{}
	for _, s := range []*summary.Summary{a, b} {
		for _, k := range s.Esc {
			if !have[k] {
				have[k] = true
				j.Esc = append(j.Esc, k)
			}
		}
	}
	sort.Slice(j.Esc, func(i, k int) bool { return j.Esc[i] < j.Esc[k] })
	return j
}

func withFakes(t *testing.T, order *[]eir.FuncID) *fakeArena {
	arena := &fakeArena{}
	saved := []any{solveFunc, leq, newPaths, newArena, newSets, joinSums}
	solveFunc = fakeSolve(order)
	leq = fakeLeq
	joinSums = fakeJoin
	newPaths = func() domain.Paths { return fakePaths{} }
	newArena = func() domain.Arena { return arena }
	newSets = func(a domain.Arena) domain.Sets { return fakeSets{a: arena} }
	// The fake solves keep no state: the real solver's resumable solve
	// would solve the fake functions for real.
	savedKeep := solveKeepFunc
	solveKeepFunc = nil
	t.Cleanup(func() {
		solveKeepFunc = savedKeep
		solveFunc = saved[0].(func(*eir.Func, solve.Env, *solve.Workspace) solve.Result)
		leq = saved[1].(func(domain.Paths, *summary.Summary, *summary.Summary) bool)
		newPaths = saved[2].(func() domain.Paths)
		newArena = saved[3].(func() domain.Arena)
		newSets = saved[4].(func(domain.Arena) domain.Sets)
		joinSums = saved[5].(func(domain.Paths, *summary.Summary, *summary.Summary) *summary.Summary)
	})
	return arena
}

// TestDriver checks the per-SCC driver: callees first, iteration to a fixed
// point inside a recursive SCC, findings sorted, summaries copied out and
// the arena reset after each SCC.
func TestDriver(t *testing.T) {
	var order []eir.FuncID
	arena := withFakes(t, &order)
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

	var sccs [][]eir.FuncID
	var stats []SCCStats
	res := RunWith(pr.p, Options{OnSCC: func(scc []eir.FuncID, st SCCStats) {
		sccs = append(sccs, append([]eir.FuncID(nil), scc...))
		stats = append(stats, st)
	}})

	if res.Stats.SCCs != 4 || res.Stats.LargestSCC != 3 || len(sccs) != 4 {
		t.Fatalf("stats %+v, sccs %v", res.Stats, sccs)
	}
	if arena.resets != res.Stats.Iterations {
		t.Errorf("arena reset %d times, want once per solve (%d)", arena.resets, res.Stats.Iterations)
	}
	// Every function's final summary holds its own key and its callees'.
	want := map[string][]string{
		"c": {"c"}, "a": {"a", "b", "c"}, "b": {"a", "b", "c"},
		"d": {"d", "e", "f"}, "e": {"d", "e", "f"}, "f": {"d", "e", "f"},
		"main": {"main", "a", "b", "c", "d", "e", "f"},
	}
	for n, ks := range want {
		s := res.Summaries[fs[n].f.ID]
		if s == nil || len(s.Esc) != len(ks) {
			t.Errorf("%s: summary %v, want keys of %v", n, s, ks)
		}
	}
	// main is solved last, once.
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

// fakeKept is a kept solve of the fake solver: resuming solves again.
type fakeKept struct {
	f       *eir.Func
	ws      *solve.Workspace
	bytes   int
	resumes *int
}

func (k *fakeKept) Resume(env solve.Env) solve.Result {
	*k.resumes++
	return solveFunc(k.f, env, k.ws)
}

func (k *fakeKept) Bytes() int { return k.bytes }

// TestDriverResume checks incremental re-solving in the driver: members of
// an SCC are kept and resumed, the results are those of solving from
// scratch, the states are released when the SCC is final, and past the
// budget the largest are dropped.
func TestDriverResume(t *testing.T) {
	build := func() (*prog, map[string]*fbuild) {
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
	var order []eir.FuncID
	withFakes(t, &order)
	pr, _ := build()
	want := RunWith(pr.p, Options{})

	for _, budget := range []int{1 << 30, 1} {
		resumes, keeps := 0, 0
		saved, savedBudget := solveKeepFunc, retainBudget
		solveKeepFunc = func(f *eir.Func, env solve.Env, ws *solve.Workspace) (solve.Result, resumable) {
			keeps++
			return solveFunc(f, env, ws), &fakeKept{f: f, ws: ws, bytes: 10, resumes: &resumes}
		}
		retainBudget = budget
		var stats []SCCStats
		pr, fs := build()
		got := RunWith(pr.p, Options{OnSCC: func(_ []eir.FuncID, st SCCStats) { stats = append(stats, st) }})
		solveKeepFunc, retainBudget = saved, savedBudget

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
	deps := map[eir.FuncID][]eir.FuncID{0: {1}, 1: {2}, 2: {0, 9}}
	w := newSCCWork([]eir.FuncID{0, 1, 2}, func(id eir.FuncID) []eir.FuncID { return deps[id] })
	var got []eir.FuncID
	for !w.empty() {
		got = append(got, w.pop())
	}
	if want := []eir.FuncID{2, 1, 0}; !sameIDs(got, want) {
		t.Errorf("order %v, want %v (callees first)", got, want)
	}
	w.push(0)
	w.push(2)
	w.push(0)
	w.push(9) // not in the SCC
	got = nil
	for !w.empty() {
		got = append(got, w.pop())
	}
	if want := []eir.FuncID{2, 0}; !sameIDs(got, want) {
		t.Errorf("after pushes: %v, want %v", got, want)
	}
}

// TestDriverLimit checks a function past the solver's limits: it gets an
// E3010 finding and the top summary, its callers are solved against that,
// and the analysis goes on. A panic that is not a limit is not caught.
func TestDriverLimit(t *testing.T) {
	var order []eir.FuncID
	withFakes(t, &order)
	pkg := typecheck(t, `
func big(p *int, q *int) *int { return p }
func caller() {}
`)
	pr := newProg()
	big := pr.decl(lookupFunc(t, pkg, "big"))
	caller := pr.decl(lookupFunc(t, pkg, "caller"))
	caller.callStatic(big, caller.heap(), caller.heap())
	inner := fakeSolve(&order)
	var callerSaw *summary.Summary
	solveFunc = func(f *eir.Func, env solve.Env, ws *solve.Workspace) solve.Result {
		if f.ID == big.f.ID {
			panic(&solve.LimitError{Func: f.Name, Bytes: 1 << 30})
		}
		callerSaw = env.Summary(big.f.ID)
		return inner(f, env, ws)
	}
	res := RunWith(pr.p, Options{})
	var lim []solve.Finding
	for _, fd := range res.Findings {
		if fd.Code == codeEscapeLimit {
			lim = append(lim, fd)
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

	// Any other panic goes on up.
	solveFunc = func(f *eir.Func, env solve.Env, ws *solve.Workspace) solve.Result {
		panic("not a limit")
	}
	defer func() {
		if r := recover(); r != "not a limit" {
			t.Errorf("recovered %v, want the panic to go on up", r)
		}
	}()
	RunWith(pr.p, Options{})
}

// TestDriverUnscheduledRead checks that a solver reading the summary of a
// function the graph did not order first is an internal error, not a
// silent bottom summary.
func TestDriverUnscheduledRead(t *testing.T) {
	var order []eir.FuncID
	withFakes(t, &order)
	pr := newProg()
	a := pr.fn("a", eir.FuncKey{}, nil)
	b := pr.fn("b", eir.FuncKey{}, nil)
	solveFunc = func(f *eir.Func, env solve.Env, ws *solve.Workspace) solve.Result {
		if f.ID == a.f.ID {
			env.Summary(b.f.ID) // a does not call b
		}
		return solve.Result{Summary: &summary.Summary{}}
	}
	defer func() {
		if recover() == nil {
			t.Errorf("no internal error")
		}
	}()
	b.callStatic(a)
	RunWith(pr.p, Options{})
}

// TestDriverDynamicJoin checks that a dynamic call is given one virtual
// candidate whose summary joins its candidates', and that functions in a
// cycle through a dynamic call are solved to their fixed point.
func TestDriverDynamicJoin(t *testing.T) {
	var order []eir.FuncID
	withFakes(t, &order)
	pr := newProg()
	main := pr.fn("main", eir.FuncKey{}, nil)
	g1 := pr.fn("g1", eir.FuncKey{}, nil)
	g2 := pr.fn("g2", eir.FuncKey{}, nil)
	h := pr.fn("h", eir.FuncKey{}, nil)
	main.f.Facts.FuncValues = []eir.FuncID{g1.f.ID, g2.f.ID}
	dyn := &eir.Call{Kind: eir.CallFunc, Target: main.heap()}
	main.call(dyn)
	g2.callStatic(h)
	// h calls back through a func value: g1, g2 and h form a cycle through
	// the dispatch node; main is outside it.
	hd := &eir.Call{Kind: eir.CallFunc, Target: h.heap()}
	h.call(hd)

	var gotCands []eir.FuncID
	saved := solveFunc
	solveFunc = func(f *eir.Func, env solve.Env, ws *solve.Workspace) solve.Result {
		if f.ID == main.f.ID {
			gotCands = env.Candidates(f, dyn)
		}
		return saved(f, env, ws)
	}
	res := RunWith(pr.p, Options{})
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
