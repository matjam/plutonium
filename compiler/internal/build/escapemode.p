// Translated by go2pu from internal/build/escapemode.go at the go-cutover tag; edited by hand since.

package build

import (
	"compiler/compat"
	"compiler/internal/diag"
	"compiler/internal/escape"
	"compiler/internal/escape2"
	"compiler/internal/load"
	"compiler/internal/scratch"
	"compiler/pu"
)

// analyzeEscapes runs the escape analysis that PLUTO_ESCAPE selects
// (docs/plans/2026-10-20-escape-redesign.md, section (g)): the analysis in
// internal/escape2 when it is unset or "new", internal/escape for "old",
// and both for "both", which keeps the old verdict and reports
// every program and error site where the two differ (on standard error,
// and appended to the file PLUTO_ESCAPE_DIFF names). The self-hosted
// compiler links both, from go2pu's translation of this file.
func analyzeEscapes(prog *load.Program) (*escape.Calls, error) {
	mode, err := escape2.ModeFromEnv()
	if err != nil {
		calls, _ := escape.Analyze(prog)
		return calls, err
	}
	return escape2.Analyze(prog, mode)
}

// escapeStep runs the escape analysis as a scratch step: its program
// representation, summaries and solver state die with it, and only what it
// found about calls and its errors are kept.
type escapeStep struct {
	prog  *load.Program
	calls *escape.Calls
	err   error
}

func (s *escapeStep) ScratchWork() { s.calls, s.err = analyzeEscapes(s.prog) }

func (s *escapeStep) ScratchKeep() {
	s.calls = s.calls.Clone()
	s.err = keepError(s.err)
}

// keepError returns a copy of err from the current allocator, keeping a
// list of diagnostics a list.
func keepError(err error) error {
	if l, ok := err.(*diag.List); ok {
		n := pu.Alloc[diag.List]()
		for _, d := range *l {
			(*n) = diag.List(compat.Append((*n), d.Clone()))
		}
		return n
	}
	return scratch.Error(err)
}

// runEscapes is analyzeEscapes in a scratch step of its own.
func runEscapes(prog *load.Program) (*escape.Calls, error) {
	s := pu.New(escapeStep{prog: prog})
	scratch.Do(s)
	scratch.Trim()
	return s.calls, s.err
}
