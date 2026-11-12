package sched

import (
	"compiler/compat"
	"compiler/internal/escape2/eir"
	"compiler/internal/escape2/summary"
	"compiler/internal/scratch"
	"compiler/internal/types2"
	"compiler/pu"
)

// The sequential driver's solves (run.go's iterate and check, and the
// final solves for reasons) run as the round driver's do: on a worker, in
// scratch memory freed once the outcome is copied out (workers.go). The
// compiler's code does not free, so a solve run in the caller's memory
// keeps all it allocated: with the summary cache, which uses this driver,
// building the compiler took the analysis past 24 GB. A solve that keeps
// its state (incr.go) still runs on the env, outside scratch memory.
type seqSolver struct {
	e  *env
	pf *parFacts
	ix *pathIndex
	w  *worker
}

func newSeqSolver(p *eir.Program, e *env, comps [][]eir.FuncID) *seqSolver {
	pf := pu.New(parFacts{e: e, scc: compat.Make[int32](len(p.Funcs)), done: compat.Make[bool](len(comps))})
	for i := range pf.scc {
		pf.scc[i] = -1
	}
	for ci, comp := range comps {
		for _, id := range comp {
			pf.scc[id] = int32(ci)
		}
	}
	pf.warm(p)
	ix := newPathIndex(e.paths)
	return pu.New(seqSolver{e: e, pf: pf, ix: ix, w: newWorker(pf, ix)})
}

// at makes SCC ci the current one: the SCCs before it are final, those
// after it not solved, as env.Summary and env.CanRead have it.
func (s *seqSolver) at(ci int) {
	for j := range s.pf.done {
		s.pf.done[j] = j < ci
	}
}

// next makes SCC ci, the one after the current, the current one.
func (s *seqSolver) next(ci int) {
	if ci > 0 {
		s.pf.done[ci-1] = true
	}
}

// solve runs t in scratch memory, its reads of facts recorded by rec when
// it is not nil, and renames the summary's new paths to those it made in
// the interner.
func (s *seqSolver) solve(t task, rec Recorder) outcome {
	// The interner may have grown outside a worker's solve (a kept
	// state's solve, a join made for the cache or the reasons).
	s.ix.sync()
	s.w.rec = rec
	st := solveStep{w: s.w, t: t}
	scratch.Do(&st)
	s.w.rec = nil
	o := st.out
	if !t.final && o.ov != nil {
		remapSummary(s.e.paths, o.res.Summary, o.ov.commit())
		o.ov = nil
	}
	return o
}

// recordingWorker is a worker's view of the program with its reads of
// facts recorded. Deferred calls and CanRead are decided from the EIR and
// the call graph, which the cache's digest covers, so they are not. What the
// recorder records stays in the solve's scratch memory: the recorder is
// sealed into a digest before that memory is freed (solveStep).
type recordingWorker struct {
	*worker
	rec Recorder
}

func (w recordingWorker) HeapOnly(T types2.Type, field int) bool {
	h := w.worker.HeapOnly(T, field)
	w.rec.HeapOnly(T, field, h)
	return h
}

func (w recordingWorker) Instantiated(tp *types2.TypeParam) ([]types2.Type, bool) {
	types, known := w.worker.Instantiated(tp)
	w.rec.Instantiated(tp, types, known)
	return types, known
}

func (w recordingWorker) Methods(t types2.Type, m *types2.Func) []eir.FuncID {
	ids := w.worker.Methods(t, m)
	w.rec.Methods(t, m, ids)
	return ids
}

func (w recordingWorker) PointerWrappers(u types2.Type) ([]types2.Type, bool) {
	types, known := w.worker.PointerWrappers(u)
	w.rec.PointerWrappers(u, types, known)
	return types, known
}

func (w recordingWorker) Retyped(u, v types2.Type) bool {
	ok := w.worker.Retyped(u, v)
	w.rec.Retyped(u, v, ok)
	return ok
}

func (w recordingWorker) Contract(name string) (*summary.Summary, bool) {
	s, ok := w.worker.Contract(name)
	w.rec.Contract(name, s, ok)
	return s, ok
}
