package sched

import (
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/solve"
)

// The solver's resumable solve (solve.SolveKeep, solve.State.Resume), for
// the incremental re-solving of incr.go.
func init() {
	solveKeepFunc = func(f *eir.Func, env solve.Env, ws *solve.Workspace) (solve.Result, resumable) {
		r, st := solve.SolveKeep(f, env, ws)
		return r, st
	}
}
