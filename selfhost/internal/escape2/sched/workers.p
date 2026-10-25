package sched

import (
	"selfhost/compat"
	"selfhost/internal/escape2/domain"
	"selfhost/internal/escape2/eir"
	"selfhost/internal/escape2/solve"
	"selfhost/internal/escape2/summary"
	"selfhost/internal/scratch"
	"selfhost/pu"
)

// runTasks is the Go compiler's runTasks (internal/escape2/sched/
// workers.go) for a compiler that runs one task: the tasks run in order on
// the first worker. Their outcomes do not depend on the order (parallel.go),
// so this gives what the Go version gives with any number of workers.
// Hand-written; go2pu keeps it in place of a translation of workers.go,
// whose channel and task statement the panic handler would reach through
// func values (lower/context.go).
//
// The compiler's code does not free, so each solve runs in scratch memory
// (scratch.Do), freed once its outcome is copied out: the solver's memory
// is then bounded by the largest solve rather than the sum of them. What a
// solve adds to state that outlives it (the filter table, the memo tables
// of the facts the worker gives, the domain arenas' pool) comes from the
// caller's memory (scratch.Outer). A solve that keeps its state, or
// resumes a kept one, runs outside scratch memory, since the state
// outlives it.
func runTasks(workers []*worker, tasks []task, lastDur []compat.Duration) []outcome {
	_ = lastDur
	outs := compat.Make[outcome](len(tasks))
	w := workers[0]
	for i, t := range tasks {
		if t.keep || t.kept != nil {
			outs[i] = w.run(t)
			continue
		}
		s := solveStep{w: w, t: t}
		scratch.Do(&s)
		outs[i] = s.out
	}
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
