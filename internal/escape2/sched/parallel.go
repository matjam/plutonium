package sched

import (
	"fmt"
	"os"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/solve"
	"github.com/matjam/plutonium/internal/escape2/summary"
	"github.com/matjam/plutonium/internal/types2"
)

// The parallel driver. The analysis goes in rounds. In a round, every
// function that needs solving is solved, on as many workers as there are
// (PLUTO_ESCAPE2_JOBS), each against the summaries as they were when the
// round began; then the results are applied one after the other in a fixed
// order (commit). A function needs solving when its SCC's turn has come
// (every SCC it calls into is done) and it has not been solved yet, or a
// summary its solves read grew in the last round (Jacobi iteration inside
// an SCC: each round solves an SCC's dirty members against the previous
// round's summaries). An SCC with no dirty member is final: its members are
// solved once more for well-founded reasons (reasons.go), and then it is
// done, and the SCCs that call into it may start.
//
// The results do not depend on the number of workers or their timing:
// nothing a solve reads changes during a round (the summaries, the path
// interner, the facts), the paths a solve makes are its own until the
// commit (overlay.go), and the commit goes in the order of the SCCs and of
// their members. TestParallelDeterministic runs the driver with different
// worker counts and compares everything it gives.
//
// Kept solver states (incr.go) are not used here: a kept state names paths
// by the IDs of its overlay, which the commit renames.

// jobs is the number of workers: PLUTO_ESCAPE2_JOBS, or the number of CPUs,
// at most as many as fit in PLUTO_ESCAPE2_MEM_MB (default 6144) at
// workerMB each.
func jobs() int {
	if n, err := strconv.Atoi(os.Getenv("PLUTO_ESCAPE2_JOBS")); err == nil && n > 0 {
		return n
	}
	n := runtime.NumCPU()
	mem := 6144
	if m, err := strconv.Atoi(os.Getenv("PLUTO_ESCAPE2_MEM_MB")); err == nil && m > 0 {
		mem = m
	}
	if c := mem / workerMB; c < n {
		n = c
	}
	if n < 1 {
		n = 1
	}
	return n
}

// workerMB is what a worker is expected to hold: its workspace between
// solves (a workspace past bigWorkspace is dropped after its solve) and
// what one solve builds.
const workerMB = 256

// bigWorkspace is the size past which a worker's workspace is dropped after
// a solve rather than reset and kept: one large solve should not make a
// worker hold its memory for the rest of the analysis.
const bigWorkspace = 64 << 20

// parFacts are the facts and summaries of other SCCs the workers read, made
// read only for the rounds: what stage B memoizes lazily is computed before
// the first round (warm), and what cannot be is behind mu.
type parFacts struct {
	e      *env
	cands  map[*eir.Call][]eir.FuncID
	defer_ map[*eir.Call]bool
	contr  map[string]*summary.Summary
	mu     sync.Mutex
	// vmu locks the joins of the virtual functions, by virtual function:
	// computing one fills its cache entry.
	vmu  [64]sync.Mutex
	scc  []int32 // by function
	done []bool  // by SCC
}

// warm computes what the workers would otherwise compute lazily: the
// candidates of every dynamic call (and their virtual functions), whether
// each call is deferred, every contract a call names, the definitions of
// every function's registers.
func (pf *parFacts) warm(p *eir.Program) {
	e := pf.e
	pf.cands = map[*eir.Call][]eir.FuncID{}
	pf.defer_ = map[*eir.Call]bool{}
	pf.contr = map[string]*summary.Summary{}
	for _, f := range p.Funcs {
		e.facts.c.defsOf(f)
		for _, b := range f.Blocks {
			for i := range b.Instrs {
				c := b.Instrs[i].Call
				if c == nil {
					continue
				}
				switch c.Kind {
				case eir.CallIface, eir.CallTypeParam, eir.CallFunc:
					pf.cands[c] = e.Candidates(f, c)
					pf.defer_[c] = e.Deferred(f, c)
				case eir.CallContract:
					if _, ok := pf.contr[c.Contract]; !ok {
						if s, ok := e.Contract(c.Contract); ok {
							pf.contr[c.Contract] = s
						}
					}
				}
			}
		}
	}
}

// worker is one worker: its workspace, and its view of the program for
// the solve it runs (solve.Env).
type worker struct {
	pf      *parFacts
	ix      *pathIndex
	arena   domain.Arena
	ws      *solve.Workspace
	ov      *overlay
	scc     int32
	solving eir.FuncID
	reads   []eir.FuncID
	mark    []uint32
	stamp   uint32
}

func newWorker(pf *parFacts, ix *pathIndex) *worker {
	w := &worker{pf: pf, ix: ix, mark: make([]uint32, len(pf.e.sums))}
	w.newWorkspace()
	return w
}

func (w *worker) newWorkspace() {
	w.arena = newArena()
	w.ws = &solve.Workspace{Arena: w.arena, Sets: newSets(w.arena)}
}

func (w *worker) note(id eir.FuncID) {
	if int(id) < len(w.mark) && w.pf.scc[id] == w.scc && w.mark[id] != w.stamp {
		w.mark[id] = w.stamp
		w.reads = append(w.reads, id)
	}
}

func (w *worker) Program() *eir.Program { return w.pf.e.p }
func (w *worker) Paths() domain.Paths   { return w.ov }

func (w *worker) Summary(id eir.FuncID) *summary.Summary {
	e := w.pf.e
	if e.virt.isVirtual(id) {
		i := int(id) - len(e.p.Funcs)
		for _, m := range e.virt.sets[i] {
			w.note(m)
		}
		mu := &w.pf.vmu[i%len(w.pf.vmu)]
		mu.Lock()
		s := e.virt.summary(id, func(m eir.FuncID) *summary.Summary { return e.sums[m] })
		mu.Unlock()
		return s
	}
	if int(id) >= len(e.sums) {
		panic(fmt.Sprintf("internal compiler error: escape2: the summary of function %d, which the program does not have, was read", id))
	}
	if sc := w.pf.scc[id]; sc != w.scc && (sc < 0 || !w.pf.done[sc]) {
		panic(fmt.Sprintf("internal compiler error: escape2: the summary of %s was read before it was scheduled", e.p.Funcs[id].Name))
	}
	w.note(id)
	return e.sums[id]
}

func (w *worker) Candidates(f *eir.Func, c *eir.Call) []eir.FuncID {
	ids, ok := w.pf.cands[c]
	if !ok {
		panic(fmt.Sprintf("internal compiler error: escape2: the candidates of a call in %s were not computed before the solves", f.Name))
	}
	return ids
}

func (w *worker) Contract(name string) (*summary.Summary, bool) {
	if s, ok := w.pf.contr[name]; ok {
		return s, true
	}
	w.pf.mu.Lock()
	defer w.pf.mu.Unlock()
	return w.pf.e.Contract(name)
}

func (w *worker) HeapOnly(T types2.Type, field int) bool {
	w.pf.mu.Lock()
	defer w.pf.mu.Unlock()
	return w.pf.e.HeapOnly(T, field)
}

func (w *worker) Instantiated(tp *types2.TypeParam) ([]types2.Type, bool) {
	w.pf.mu.Lock()
	defer w.pf.mu.Unlock()
	return w.pf.e.Instantiated(tp)
}

func (w *worker) Methods(t types2.Type, m *types2.Func) []eir.FuncID {
	w.pf.mu.Lock()
	defer w.pf.mu.Unlock()
	return w.pf.e.Methods(t, m)
}

func (w *worker) Deferred(f *eir.Func, c *eir.Call) bool {
	if v, ok := w.pf.defer_[c]; ok {
		return v
	}
	return false
}

func (w *worker) CanRead(f *eir.Func, id eir.FuncID) bool {
	e := w.pf.e
	if e.virt.isVirtual(id) {
		for _, m := range e.virt.sets[int(id)-len(e.p.Funcs)] {
			if !w.CanRead(f, m) {
				return false
			}
		}
		return true
	}
	if int(id) >= len(e.sums) {
		return false
	}
	sc := w.pf.scc[id]
	return sc == w.scc || sc >= 0 && w.pf.done[sc]
}

// A task is one solve of a round.
type task struct {
	id    eir.FuncID
	scc   int32
	final bool // the final solve for reasons (SolveDerivs)
}

// outcome is what a task gives, for the commit.
type outcome struct {
	res      solve.Result
	ov       *overlay
	reads    []eir.FuncID
	derivs   [][]summary.Reason // final: summary.EntryDerivs against the SCC's final summaries
	derivsOK bool
	arenaB   int
	setsB    int
	dur      time.Duration
}

// run solves one task.
func (w *worker) run(t task) outcome {
	e := w.pf.e
	f := e.p.Funcs[t.id]
	w.ov = newOverlay(w.ix)
	w.scc = t.scc
	w.solving = t.id
	w.stamp++
	w.reads = w.reads[:0]
	var o outcome
	t0 := time.Now()
	if t.final {
		r := solveDerivsFunc(f, w, w.ws)
		if r.Limit != nil {
			r = limitResult(f, w)
		} else if r.Summary != nil {
			o.derivs = summary.EntryDerivs(w.ov, e.sums[t.id], r.Summary, r.Derivs)
			o.derivsOK = true
		}
		o.res.Findings = append([]solve.Finding(nil), r.Findings...)
	} else {
		r := solveGuarded(f, w, w.ws)
		o.res.Findings = append([]solve.Finding(nil), r.Findings...)
		o.res.Summary = clone(r.Summary)
		o.ov = w.ov
		o.reads = append([]eir.FuncID(nil), w.reads...)
	}
	o.dur = time.Since(t0)
	o.arenaB = w.arena.Bytes()
	o.setsB = w.ws.Sets.Bytes()
	if o.arenaB > bigWorkspace {
		w.newWorkspace()
	} else {
		w.ws.Sets.Reset()
		w.arena.Reset()
	}
	w.ov = nil
	return o
}

// sccRun is the state of an SCC in the parallel driver.
type sccRun struct {
	ci       int32
	dirty    map[eir.FuncID]bool
	solves   map[eir.FuncID]int
	readBy   map[eir.FuncID][]eir.FuncID
	readSeen map[[2]eir.FuncID]bool
	final    bool // its final solves are the next round's
	derivs   map[eir.FuncID][][]summary.Reason
	st       SCCStats
	// rank is the SCC's postorder of its own calls, callees first
	// (worklist.go); lastReads the members of the SCC each member's last
	// solve read.
	rank      map[eir.FuncID]int
	lastReads map[eir.FuncID][]eir.FuncID
}

// ready reports whether dirty member id is solved in this round: callees
// first, as the sequential driver's worklist does, so that a member is not
// solved against summaries of its callees that are about to change. It
// waits while a member it reads (its last solve's reads; its call graph
// deps before its first solve) comes before it and is dirty. The first
// dirty member in that order never waits, so every round solves some.
func (r *sccRun) ready(id eir.FuncID, g *callGraph, scc []int32) bool {
	deps, ok := r.lastReads[id]
	if !ok {
		deps = g.deps[id]
	}
	for _, d := range deps {
		if d != id && scc[d] == r.ci && r.dirty[d] && r.rank[d] < r.rank[id] {
			return false
		}
	}
	return true
}

// runParallel analyses the SCCs comps (callees first) in rounds, filling
// env's summaries and findings; finish is called for each SCC when it is
// done.
func runParallel(p *eir.Program, env *env, g *callGraph, comps [][]eir.FuncID, findings [][]solve.Finding, finish func(comp []eir.FuncID, st *SCCStats)) {
	pf := &parFacts{e: env, scc: make([]int32, len(p.Funcs)), done: make([]bool, len(comps))}
	for i := range pf.scc {
		pf.scc[i] = -1
	}
	for ci, comp := range comps {
		for _, id := range comp {
			pf.scc[id] = int32(ci)
			env.inSCC[id] = int32(ci)
		}
	}
	env.current = int32(len(comps) - 1)
	pf.warm(p)
	ix := newPathIndex(env.paths)

	// The SCC DAG: preds[ci] is the number of SCCs ci reads that are not
	// done; succs[ci] the SCCs that read ci.
	preds := make([]int, len(comps))
	succs := make([][]int32, len(comps))
	for ci, comp := range comps {
		seen := map[int32]bool{}
		for _, id := range comp {
			for _, d := range g.deps[id] {
				dc := pf.scc[d]
				if dc < 0 || dc == int32(ci) || seen[dc] {
					continue
				}
				seen[dc] = true
				preds[ci]++
				succs[dc] = append(succs[dc], int32(ci))
			}
		}
	}

	n := jobs()
	// lastDur is how long each function's last solve took: a round starts
	// its longest solves first, so that a long one does not start last and
	// hold the round up (the commit's order does not depend on it).
	lastDur := make([]time.Duration, len(p.Funcs))
	workers := make([]*worker, n)
	for i := range workers {
		workers[i] = newWorker(pf, ix)
	}

	var active []*sccRun
	start := func(ci int32) {
		r := &sccRun{ci: ci, dirty: map[eir.FuncID]bool{}, solves: map[eir.FuncID]int{},
			readBy: map[eir.FuncID][]eir.FuncID{}, readSeen: map[[2]eir.FuncID]bool{},
			lastReads: map[eir.FuncID][]eir.FuncID{}}
		if len(comps[ci]) > 1 {
			r.rank = newSCCWork(comps[ci], func(id eir.FuncID) []eir.FuncID { return g.deps[id] }).rank
		}
		r.st.Funcs = len(comps[ci])
		for _, id := range comps[ci] {
			r.dirty[id] = true
		}
		active = append(active, r)
	}
	for ci := range comps {
		if preds[ci] == 0 {
			start(int32(ci))
		}
	}

	for len(active) > 0 {
		// The round's tasks, in SCC order and member order.
		sortRuns(active)
		var tasks []task
		for _, r := range active {
			for _, id := range comps[r.ci] {
				if r.final {
					tasks = append(tasks, task{id: id, scc: r.ci, final: true})
				} else if r.dirty[id] && r.ready(id, g, pf.scc) {
					tasks = append(tasks, task{id: id, scc: r.ci})
				}
			}
		}
		solveStart := time.Now()
		outs := make([]outcome, len(tasks))
		if len(tasks) == 1 || n == 1 {
			for i, t := range tasks {
				outs[i] = workers[0].run(t)
			}
		} else {
			var wg sync.WaitGroup
			order := make([]int, len(tasks))
			for i := range order {
				order[i] = i
			}
			slices.SortStableFunc(order, func(a, b int) int {
				return cmpDur(lastDur[tasks[b].id], lastDur[tasks[a].id])
			})
			next := make(chan int, len(tasks))
			for _, i := range order {
				next <- i
			}
			close(next)
			for _, w := range workers[:min(n, len(tasks))] {
				wg.Add(1)
				go func(w *worker) {
					defer wg.Done()
					for i := range next {
						outs[i] = w.run(tasks[i])
					}
				}(w)
			}
			wg.Wait()
		}

		commitStart := time.Now()
		// The commit, in task order.
		byCI := map[int32]*sccRun{}
		for _, r := range active {
			byCI[r.ci] = r
		}
		// Every solve of the round read the summaries as they were before
		// it, so its reads count before any of the round's summaries grows:
		// a member whose solve read another solved in the same round is
		// solved again if that one grows, whichever commits first.
		for i, t := range tasks {
			if t.final {
				continue
			}
			r := byCI[t.scc]
			delete(r.dirty, t.id)
			r.lastReads[t.id] = outs[i].reads
			for _, rd := range outs[i].reads {
				if k := [2]eir.FuncID{rd, t.id}; !r.readSeen[k] {
					r.readSeen[k] = true
					r.readBy[rd] = append(r.readBy[rd], t.id)
				}
			}
		}
		for i, t := range tasks {
			r := byCI[t.scc]
			o := &outs[i]
			r.st.Solves++
			lastDur[t.id] = o.dur
			if o.arenaB > r.st.ArenaBytes {
				r.st.ArenaBytes = o.arenaB
			}
			if o.setsB > r.st.SetBytes {
				r.st.SetBytes = o.setsB
			}
			findings[t.id] = o.res.Findings
			if t.final {
				if o.derivsOK {
					r.derivs[t.id] = o.derivs
				}
				continue
			}
			r.solves[t.id]++
			if r.solves[t.id] > maxSolvesPerFunc {
				panic(fmt.Sprintf("internal compiler error: escape2: %s did not converge in its SCC of %d functions", p.Funcs[t.id].Name, len(comps[r.ci])))
			}
			s := o.res.Summary
			remapSummary(env.paths, s, o.ov.commit())
			prev := env.sums[t.id]
			covered := prev != nil && s != nil && leq(env.paths, s, prev)
			if traceSolve != nil {
				traceSolve(env.paths, comps[r.ci], t.id, prev, s, covered)
			}
			if covered {
				continue
			}
			if prev == nil {
				env.sums[t.id] = s
			} else {
				env.sums[t.id] = joinSums(env.paths, prev, s)
			}
			env.virt.changedFunc(t.id)
			for _, rd := range r.readBy[t.id] {
				r.dirty[rd] = true
			}
		}

		if traceRounds {
			fmt.Fprintf(os.Stderr, "escape2: round: %d tasks, solve %v, commit %v, active %d\n", len(tasks), commitStart.Sub(solveStart), time.Since(commitStart), len(active))
		}
		// SCCs that are final, or done.
		var still []*sccRun
		var ready []int32
		for _, r := range active {
			comp := comps[r.ci]
			switch {
			case r.final:
				r.st.Unfounded = chooseReasons(comp, env, r.derivs)
			case len(r.dirty) > 0:
				still = append(still, r)
				continue
			default:
				iterated := len(comp) > 1
				for _, k := range r.solves {
					iterated = iterated || k > 1
				}
				if iterated {
					r.final = true
					r.derivs = map[eir.FuncID][][]summary.Reason{}
					still = append(still, r)
					continue
				}
			}
			pf.done[r.ci] = true
			finish(comp, &r.st)
			for _, sc := range succs[r.ci] {
				preds[sc]--
				if preds[sc] == 0 {
					ready = append(ready, sc)
				}
			}
		}
		active = still
		for _, ci := range ready {
			start(ci)
		}
	}
	for ci, d := range pf.done {
		if !d {
			panic(fmt.Sprintf("internal compiler error: escape2: SCC %d was never solved", ci))
		}
	}
}

// sortRuns sorts the active SCCs by index: the order of a round's tasks and
// of its commit.
func sortRuns(rs []*sccRun) {
	slices.SortFunc(rs, func(a, b *sccRun) int { return int(a.ci) - int(b.ci) })
}

// traceRounds (PLUTO_ESCAPE2_TRACE_ROUNDS=1) prints, for each round, its tasks
// and the time its solves and its commit took, on standard error.
var traceRounds = os.Getenv("PLUTO_ESCAPE2_TRACE_ROUNDS") != ""

func cmpDur(a, b time.Duration) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
