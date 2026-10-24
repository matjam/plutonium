package build

import (
	"github.com/matjam/plutonium/internal/escape"
	"github.com/matjam/plutonium/internal/escape2"
	"github.com/matjam/plutonium/internal/load"
)

// analyzeEscapes runs the escape analysis that PLUTO_ESCAPE selects
// (docs/plans/2026-10-20-escape-redesign.md, section (g)): internal/escape
// when it is unset or "old", the analysis being rebuilt in internal/escape2
// for "new", and both for "both", which keeps the old verdict and reports
// every program and error site where the two differ (on standard error,
// and appended to the file PLUTO_ESCAPE_DIFF names).
//
// The self-hosted compiler runs internal/escape alone: its translation of
// this file is written by hand (selfhost/internal/build/escapemode.p, which
// says why).
func analyzeEscapes(prog *load.Program) (*escape.Calls, error) {
	mode, err := escape2.ModeFromEnv()
	if err != nil {
		calls, _ := escape.Analyze(prog)
		return calls, err
	}
	return escape2.Analyze(prog, mode)
}
