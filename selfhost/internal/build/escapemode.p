package build

import (
	"selfhost/internal/escape"
	"selfhost/internal/load"
)

// analyzeEscapes runs the escape analysis. This file is written by hand,
// in place of a translation of internal/build/escapemode.go (tools/go2pu
// keeps it): the self-hosted compiler has internal/escape alone, since
// internal/escape2 is translated only when it replaces internal/escape
// (docs/plans/2026-10-20-escape-redesign.md, section (g), stage 7), so
// PLUTO_ESCAPE is not read here.
func analyzeEscapes(prog *load.Program) (*escape.Calls, error) {
	return escape.Analyze(prog)
}
