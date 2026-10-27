// Translated by go2pu from internal/build/escapemode.go at the go-cutover tag; edited by hand since.

package build

import (
	"compiler/compat"
	"compiler/internal/diag"
	"compiler/internal/escape2"
	"compiler/internal/load"
	"compiler/internal/moves"
	"compiler/internal/scratch"
	"compiler/pu"
)

// analyzeEscapes runs the escape analysis (internal/escape2): its errors,
// and what it found about calls for the move checker.
func analyzeEscapes(prog *load.Program) (*moves.Calls, error) {
	return escape2.CheckCalls(prog)
}

// escapeStep runs the escape analysis as a scratch step: its program
// representation, summaries and solver state die with it, and only what it
// found about calls and its errors are kept.
type escapeStep struct {
	prog  *load.Program
	calls *moves.Calls
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
func runEscapes(prog *load.Program) (*moves.Calls, error) {
	s := pu.New(escapeStep{prog: prog})
	scratch.Do(s)
	scratch.Trim()
	return s.calls, s.err
}
