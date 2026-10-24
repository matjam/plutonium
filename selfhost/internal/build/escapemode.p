package build

import (
	"selfhost/internal/escape"
	"selfhost/internal/load"
)

// analyzeEscapes runs the escape analysis. This file is written by hand,
// in place of a translation of internal/build/escapemode.go (tools/go2pu
// keeps it), so PLUTO_ESCAPE is not read here. internal/escape2 is
// translated (selfhost/internal/escape2), but the self-hosted compiler does
// not link it yet: the Go compiler cannot build a program that holds the
// translated analysis. internal/escape runs out of memory on it, and
// internal/escape2 rejects parts of the translated compiler and does not
// yet give internal/moves its facts about calls (stage 4 of
// docs/plans/2026-10-20-escape-redesign.md). Once it can, go2pu translates
// escapemode.go and this file goes.
func analyzeEscapes(prog *load.Program) (*escape.Calls, error) {
	return escape.Analyze(prog)
}
