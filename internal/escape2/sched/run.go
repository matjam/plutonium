package sched

import (
	"fmt"
	"sort"

	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/solve"
	"github.com/matjam/plutonium/internal/escape2/summary"
	"github.com/matjam/plutonium/internal/types2"
)

// The parts of other packages the driver calls, as variables so that the
// driver's tests can run it with stand-ins.
var (
	solveFunc = solve.Solve
	leq       = summary.Leq
	joinSums  = summary.Join
	newPaths  = domain.NewPaths
	newArena  = domain.NewArena
	newSets   = domain.NewSets
)

// facts is the stage B implementation of Facts.
type facts struct {
	p    *eir.Program
	tt   *typeTable
	inst *instClosure
	c    *cands
	ho   *heapOnly
	rule dispatchRule
}

// StageB computes the whole-program facts of p.
func StageB(p *eir.Program) Facts {
	return stageB(p)
}

func stageB(p *eir.Program) *facts {
	tt := newTypeTable()
	inst := newInstClosure(tt)
	inst.run(p)
	c := newCands(p, tt, inst)
	fl := newFuncFlow(p, c)
	fl.run()
	c.flow = fl
	f := &facts{p: p, tt: tt, inst: inst, c: c, rule: unionRule{c}}
	f.ho = newHeapOnly(p, tt, inst, c)
	return f
}

// Candidates are the functions a dynamic call in f may reach, by the
// dispatch rule in force (union over the candidate set in stage 2).
func (f *facts) Candidates(fn *eir.Func, c *eir.Call) []eir.FuncID {
	return f.rule.callees(fn, c)
}

// HeapOnly reports whether field index field of struct type T holds only
// heap pointers (plan invariant I6).
func (f *facts) HeapOnly(T types2.Type, field int) bool {
	return f.ho.heapOnly(T, field)
}

// Instantiated reports the type arguments a type parameter is instantiated
// with anywhere in the program, and whether the set is known: false for an
// open type parameter, which may be instantiated with any type.
func (f *facts) Instantiated(tp *types2.TypeParam) ([]types2.Type, bool) {
	return f.inst.instantiated(tp)
}

// Run analyses p: stage B, then the SCCs callees first.
func Run(p *eir.Program) *Result {
	return RunWith(p, Options{})
}

// maxSolvesPerFunc bounds the solves of one function within its SCC. The
// domain is finite and the transfer monotone, so the bound is never reached
// by a correct solver; reaching it is an internal compiler error, never a
// silent stop.
const maxSolvesPerFunc = 10000

// RunWith is Run with options.
func RunWith(p *eir.Program, opts Options) *Result {
	f := stageB(p)
	g := buildGraph(p, f.rule)
	comps := g.sccs()

	env := &env{
		p:       p,
		paths:   newPaths(),
		facts:   f,
		sums:    make([]*summary.Summary, len(p.Funcs)),
		inSCC:   make([]int32, len(p.Funcs)),
		opts:    opts,
		current: -1,
	}
	env.virt = newVirtuals(p, env.paths)
	if env.opts.Contract == nil {
		env.opts.Contract = newContractSummaries(env.paths).lookup
	}
	for i := range env.inSCC {
		env.inSCC[i] = -1
	}
	arena := newArena()
	ws := &solve.Workspace{Arena: arena, Sets: newSets(arena)}

	res := &Result{Summaries: env.sums}
	res.Stats.Funcs = len(p.Funcs)
	res.Stats.SCCs = len(comps)

	// readers[g] are the functions of g's SCC that read g's summary.
	readers := make([][]eir.FuncID, len(p.Funcs))
	findings := make([][]solve.Finding, len(p.Funcs))

	// solveOne solves one function against the current summaries, with its
	// reads recorded for the cache when there is one.
	solveOne := func(id eir.FuncID) (solve.Result, Recorder) {
		if opts.Cache == nil {
			return solveGuarded(p.Funcs[id], env, ws), nil
		}
		rec := opts.Cache.Recorder(id)
		return solveGuarded(p.Funcs[id], recordingEnv{env, rec}, ws), rec
	}
	// measure notes the workspace a solve used and resets it.
	measure := func(st *SCCStats) {
		if b := arena.Bytes(); b > st.ArenaBytes {
			st.ArenaBytes = b
		}
		if b := ws.Sets.Bytes(); b > st.SetBytes {
			st.SetBytes = b
		}
		ws.Sets.Reset()
		arena.Reset()
	}

	// iterate solves an SCC from bottom to a post-fixed point.
	iterate := func(comp []eir.FuncID, st *SCCStats) {
		work := newSCCWork(comp, func(id eir.FuncID) []eir.FuncID { return g.deps[id] })
		solves := map[eir.FuncID]int{}
		for !work.empty() {
			id := work.pop()
			solves[id]++
			if solves[id] > maxSolvesPerFunc {
				panic(fmt.Sprintf("internal compiler error: escape2: %s did not converge in its SCC of %d functions", p.Funcs[id].Name, len(comp)))
			}
			r, rec := solveOne(id)
			if rec != nil {
				opts.Cache.Solved(id, rec)
			}
			st.Solves++
			findings[id] = r.Findings
			prev := env.sums[id]
			// Nothing a solve builds outlives it but its result: copy the
			// summary out and reset the workspace, so that memory is that
			// of the largest solve, not the sum of an SCC's iterations.
			// The new summary is joined with the previous one: solving is
			// not monotone in the summaries it reads where paths are cut
			// (summary.Join), and the join makes the iteration climb, so it
			// stops at a post-fixed point.
			if prev == nil {
				env.sums[id] = clone(r.Summary)
			} else {
				env.sums[id] = joinSums(env.paths, prev, r.Summary)
			}
			env.virt.changedFunc(id)
			measure(st)
			if prev != nil && r.Summary != nil && leq(env.paths, r.Summary, prev) {
				continue
			}
			for _, rd := range readers[id] {
				work.push(rd)
			}
		}
	}

	// check solves each function of an SCC once against summaries loaded
	// from the cache, installed as the SCC's, and reports whether they are
	// a post-fixed point read from unchanged inputs (D44). The findings
	// are those of the checking solves.
	check := func(comp []eir.FuncID, loaded []*summary.Summary, st *SCCStats) bool {
		for i, id := range comp {
			env.sums[id] = loaded[i]
		}
		env.virt.changed()
		ok := true
		for i, id := range comp {
			// A loaded summary the decoder accepted may still be one no
			// solver makes; if solving against it fails, the check fails
			// (solving from bottom then shows whether the solver itself
			// does).
			func() {
				defer func() {
					if recover() != nil {
						ok = false
					}
				}()
				r, rec := solveOne(id)
				findings[id] = r.Findings
				if !leq(env.paths, r.Summary, loaded[i]) || !opts.Cache.Matches(id, rec) {
					ok = false
					return
				}
				opts.Cache.Solved(id, rec)
			}()
			st.Solves++
			measure(st)
			if !ok {
				break
			}
		}
		if !ok {
			for _, id := range comp {
				env.sums[id] = nil
				findings[id] = nil
			}
			env.virt.changed()
		}
		return ok
	}

	view := cacheView{env: env, g: g}
	// fromFile[ci]: SCC ci was accepted from the cache.
	var fromFile []bool
	if opts.Cache != nil {
		fromFile = make([]bool, len(comps))
	}
	finish := func(comp []eir.FuncID, st *SCCStats) {
		res.Stats.Iterations += st.Solves
		if b := st.ArenaBytes + st.SetBytes; b > res.Stats.PeakArena {
			res.Stats.PeakArena = b
		}
		if opts.OnSCC != nil {
			opts.OnSCC(comp, *st)
		}
	}
	for ci, comp := range comps {
		if len(comp) > res.Stats.LargestSCC {
			res.Stats.LargestSCC = len(comp)
		}
		for _, id := range comp {
			env.inSCC[id] = int32(ci)
		}
		for _, id := range comp {
			for _, d := range g.deps[id] {
				if env.inSCC[d] == int32(ci) {
					readers[d] = append(readers[d], id)
				}
			}
		}
		env.current = int32(ci)

		var st SCCStats
		st.Funcs = len(comp)
		if opts.Cache != nil {
			loaded := opts.Cache.Begin(view, comp, true)
			switch {
			case loaded == nil:
				st.Cache = CacheMissed
				res.Stats.CacheMissed++
			case check(comp, loaded, &st):
				st.Cache = CacheAccepted
				res.Stats.CacheAccepted++
				fromFile[ci] = true
			default:
				st.Cache = CacheRejected
				res.Stats.CacheRejected++
			}
		}
		if st.Cache != CacheAccepted {
			iterate(comp, &st)
		}
		finish(comp, &st)
	}
	if opts.Cache != nil {
		for _, ci := range redoSCCs(comps, g, env.inSCC, fromFile, findings) {
			comp := comps[ci]
			env.current = int32(ci)
			for _, id := range comp {
				env.sums[id] = nil
				findings[id] = nil
			}
			env.virt.changed()
			opts.Cache.Begin(view, comp, false)
			st := SCCStats{Funcs: len(comp), Cache: CacheRedone}
			res.Stats.CacheRedone++
			iterate(comp, &st)
			finish(comp, &st)
		}
		env.current = int32(len(comps) - 1)
	}
	for _, comp := range comps {
		for _, id := range comp {
			res.Findings = append(res.Findings, findings[id]...)
			res.Stats.SummaryBytes += summaryBytes(env.sums[id])
		}
	}
	sort.SliceStable(res.Findings, func(i, j int) bool {
		a, b := res.Findings[i], res.Findings[j]
		if a.Pos != b.Pos {
			return a.Pos < b.Pos
		}
		if a.Func != b.Func {
			return a.Func < b.Func
		}
		return a.Code < b.Code
	})
	res.Env = env
	return res
}

// env is what the solver reads of the rest of the program (solve.Env).
type env struct {
	p       *eir.Program
	paths   domain.Paths
	facts   *facts
	sums    []*summary.Summary
	inSCC   []int32
	current int32
	opts    Options
	virt    *virtuals
}

func (e *env) Program() *eir.Program { return e.p }
func (e *env) Paths() domain.Paths   { return e.paths }

// Summary is the current summary of a callee: final for earlier SCCs, the
// latest iterate in the current one, nil (bottom) before the first.
func (e *env) Summary(id eir.FuncID) *summary.Summary {
	if e.virt.isVirtual(id) {
		return e.virt.summary(id, e.Summary)
	}
	if int(id) >= len(e.sums) {
		panic(fmt.Sprintf("internal compiler error: escape2: the summary of function %d, which the program does not have, was read", id))
	}
	if e.inSCC[id] < 0 || e.inSCC[id] > e.current {
		// Not scheduled before the current SCC: the graph has no edge to
		// it, so the solver must not depend on it.
		panic(fmt.Sprintf("internal compiler error: escape2: the summary of %s was read before it was scheduled", e.p.Funcs[id].Name))
	}
	return e.sums[id]
}

// Candidates are the candidates the solver applies at a dynamic call: the
// virtual function whose summary joins the candidate set (join.go).
func (e *env) Candidates(f *eir.Func, c *eir.Call) []eir.FuncID {
	return e.virt.of(e.facts.Candidates(f, c))
}

func (e *env) Contract(name string) (*summary.Summary, bool) {
	if e.opts.Contract == nil {
		return nil, false
	}
	return e.opts.Contract(name)
}

func (e *env) HeapOnly(T types2.Type, field int) bool { return e.facts.HeapOnly(T, field) }

// Instantiated implements solve.Env: stage B's instantiation sets.
func (e *env) Instantiated(tp *types2.TypeParam) ([]types2.Type, bool) {
	return e.facts.Instantiated(tp)
}

// clone copies a summary into memory of its own, sharing nothing with the
// solver's workspace.
func clone(s *summary.Summary) *summary.Summary {
	if s == nil {
		return nil
	}
	c := &summary.Summary{
		Inputs:    s.Inputs,
		Esc:       append([]domain.Key(nil), s.Esc...),
		Store:     append([]summary.StorePair(nil), s.Store...),
		HeapStore: append([]domain.Key(nil), s.HeapStore...),
		Region: summary.Region{
			Builds:       s.Region.Builds,
			OuterResults: append([]int(nil), s.Region.OuterResults...),
		},
		Reasons: append([]summary.Reason(nil), s.Reasons...),
	}
	if s.Res != nil {
		c.Res = make([]summary.Result, len(s.Res))
		for i, r := range s.Res {
			c.Res[i] = summary.Result{
				From:        append([]summary.ResultKey(nil), r.From...),
				Heap:        append([]domain.PathID(nil), r.Heap...),
				CountedHeap: append([]domain.PathID(nil), r.CountedHeap...),
			}
		}
	}
	return c
}

// summaryBytes estimates the memory a summary holds.
func summaryBytes(s *summary.Summary) int {
	if s == nil {
		return 0
	}
	n := 128 + 8*len(s.Esc) + 16*len(s.Store) + 8*len(s.HeapStore) + 8*len(s.Region.OuterResults) + 12*len(s.Reasons)
	for _, r := range s.Res {
		n += 56 + 16*len(r.From) + 4*len(r.Heap) + 4*len(r.CountedHeap)
	}
	return n
}
