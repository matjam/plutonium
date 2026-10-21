// Package refsolve is the naive reference solver of the escape analysis (plan
// docs/plans/2026-10-20-escape-redesign.md, section (g)). It applies the rules
// of internal/escape2/solve/rules.go, one small function per rule, to plain
// sets until no rule adds anything. It has no difference propagation, no
// cycle collapse, no hash-consing and no caps beyond those the rules state.
// Speed does not matter; each rule can be read against its line of the table.
//
// It exists to check internal/escape2/solve: the differential tests run both
// on the same input and require equal summaries and findings. A disagreement
// is a bug in one of them.
package refsolve

import (
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/solve"
)

// Solve has the signature of solve.Solve, without the workspace: the
// reference solver allocates freely.
func Solve(f *eir.Func, env solve.Env) solve.Result {
	panic("escape2/refsolve: the rules are not written yet")
}
