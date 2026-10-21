package solve

import "github.com/matjam/plutonium/internal/escape2/eir"

// Solve solves f against env in ws and returns its summary and findings.
// Findings are complete only when every callee's summary is final; the
// scheduler reports them once f's SCC has converged.
//
// Interface stub; the solver replaces this file.
func Solve(f *eir.Func, env Env, ws *Workspace) Result {
	panic("escape2/solve: Solve is not built yet")
}
