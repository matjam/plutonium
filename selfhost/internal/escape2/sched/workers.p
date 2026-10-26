package sched

import (
	"pluto/alloc"
	"pluto/rt"
	"pluto/slices"
	"pluto/sync"
	"selfhost/compat"
	"selfhost/internal/escape2/domain"
	"selfhost/internal/escape2/eir"
	"selfhost/internal/escape2/solve"
	"selfhost/internal/escape2/summary"
	"selfhost/internal/scratch"
	"selfhost/pu"
)

// runTasks is the Go compiler's runTasks (internal/escape2/sched/
// workers.go): each worker takes the next task until none is left, the
// longest solves (by lastDur) first, on a task of its own. Their outcomes
// do not depend on the order (parallel.go). Hand-written; go2pu keeps it
// in place of a translation of workers.go.
//
// The compiler's code does not free, so each solve runs in scratch memory,
// the worker's own (scratch.Worker), freed once its outcome is copied out:
// the solver's memory is then bounded by the largest solves rather than
// the sum of them. What a solve adds to state that outlives it (the filter
// table, the memo tables of the facts the worker gives, the domain arenas'
// pool) and the outcomes come from the caller's memory, behind a lock
// (scratch.Locked). A solve that keeps its state, or resumes a kept one,
// runs first, on the caller's task, outside scratch memory, since the
// state outlives it.
func runTasks(workers []*worker, tasks []task, lastDur []compat.Duration) []outcome {
	outs := compat.Make[outcome](len(tasks))
	var par []int
	for i, t := range tasks {
		if t.keep || t.kept != nil {
			outs[i] = workers[0].run(t)
			continue
		}
		par = compat.AppendPlain(par, i)
	}
	if len(par) == 0 {
		return outs
	}
	if len(par) == 1 || len(workers) == 1 {
		for _, i := range par {
			s := solveStep{w: workers[0], t: tasks[i]}
			scratch.Do(&s)
			outs[i] = s.out
		}
		return outs
	}
	slices.SortStableFunc(par, func(a, b int) int {
		return cmpDur(lastDur[tasks[b].id], lastDur[tasks[a].id])
	})
	sh := scratch.NewLocked()
	var mu compat.Mutex
	next := 0
	n := min(len(workers), len(par))
	var s sync.Scope
	for k := 0; k < n; k++ {
		// Each task has a worker of its own, which only it changes; the
		// race check cannot tell the workers apart (spec section 19.4,
		// memory reached through views), so the task reaches its worker
		// through rt.Pinned. What the workers share is behind locks
		// (parFacts.mu and vmu, the filter table's, the arena pool's) or
		// read only during the round.
		w := rt.Pinned(workers[k])
		wk := scratch.NewWorker(&sh)
		s.Task(func() {
			for {
				mu.Lock()
				j := next
				next++
				mu.Unlock()
				if j >= len(par) {
					return
				}
				i := par[j]
				st := solveStep{w: w, t: tasks[i]}
				// wk.Do(&st), calling the step directly: the call graph
				// the compiler checks (lower/context.go) then knows this
				// task runs only the solve.
				m := wk.Mark()
				r := alloc.Use(&wk)
				st.ScratchWork()
				r.Restore()
				o := alloc.Use(wk.Shared())
				st.ScratchKeep()
				o.Restore()
				wk.Release(m)
				mu.Lock()
				outs[i] = st.out
				mu.Unlock()
			}
		})
	}
	s.Wait()
	return outs
}

// solveStep is one solve in scratch memory.
type solveStep struct {
	w   *worker
	t   task
	o   outcome // in scratch memory
	out outcome // the copy
}

func (s *solveStep) ScratchWork() { s.o = s.w.run(s.t) }

func (s *solveStep) ScratchKeep() {
	o := &s.o
	out := outcome{
		derivsOK: o.derivsOK,
		arenaB:   o.arenaB,
		setsB:    o.setsB,
		dur:      o.dur,
		resumed:  o.resumed,
		ownWS:    o.ownWS,
	}
	out.res.Findings = compat.Append([]solve.Finding(nil), o.res.Findings...)
	out.res.Summary = clone(o.res.Summary)
	if o.ov != nil {
		out.ov = copyOverlay(o.ov)
	}
	if o.reads != nil {
		out.reads = compat.AppendPlain([]eir.FuncID(nil), o.reads...)
	}
	if o.derivs != nil {
		out.derivs = compat.Make[[]summary.Reason](len(o.derivs))
		for i, d := range o.derivs {
			out.derivs[i] = compat.Append([]summary.Reason(nil), d...)
		}
	}
	s.out = out
	// The worker's workspace and reads were made in scratch memory.
	w := s.w
	w.reads = nil
	w.arena = nil
	w.ws = nil
	w.ov = nil
}

// copyOverlay copies an overlay a solve made out of scratch memory.
func copyOverlay(o *overlay) *overlay {
	c := pu.New(overlay{
		ix:      o.ix,
		baseLen: o.baseLen,
		parent:  compat.AppendPlain([]domain.PathID(nil), o.parent...),
		step:    compat.AppendPlain([]domain.Step(nil), o.step...),
		loads:   compat.AppendPlain([]int(nil), o.loads...),
		cuts:    compat.AppendPlain([]domain.PathID(nil), o.cuts...),
		own:     map[uint64]domain.PathID{},
	})
	if o.steps != nil {
		c.steps = compat.Make[[]domain.Step](len(o.steps))
		for i, st := range o.steps {
			c.steps[i] = compat.AppendPlain([]domain.Step(nil), st...)
		}
	}
	for k, v := range o.own {
		c.own[k] = v
	}
	return c
}
