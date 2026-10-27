// Translated by go2pu from internal/escape2/sched/keep.go at the go-cutover tag; edited by hand since.

package sched

import (
	"compiler/internal/escape2/eir"
	"compiler/internal/escape2/solve"
)

// The solver's resumable solve (solve.SolveKeep, solve.State.Resume), for
// the incremental re-solving of incr.go.
func init() {
	solveKeepFunc = func(f *eir.Func, env solve.Env, ws *solve.Workspace) (solve.Result, resumable) {
		r, st := solve.SolveKeep(f, env, ws)
		return r, st
	}
}
