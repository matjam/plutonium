package build

// The inputs of the lowering oracle (docs/plans/2026-10-20-escape-redesign.md,
// section 3.1): the escape analysis's record of the calls it applied, and the
// calls the lowered program makes. Not translated by tools/go2pu (its
// untranslated map): it is test machinery of the Go compiler.

import (
	"github.com/matjam/plutonium/internal/escape"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/lower"
	"github.com/matjam/plutonium/internal/moves"
	"github.com/matjam/plutonium/internal/race"
	"github.com/matjam/plutonium/internal/scoped"
)

// OracleInputs are what the lowering oracle compares.
type OracleInputs struct {
	Prog  *load.Program
	Log   *escape.CallLog
	Facts *lower.CallFacts
	// Errors of the checks and of lowering, which do not stop the oracle:
	// the review programs are rejected on purpose.
	EscapeErr, LowerErr error
}

// Oracle loads the program in cfg.Dir and runs the front end as a build
// does, keeping the escape analysis's record of calls and the lowering
// facts.
func Oracle(cfg *Config) (*OracleInputs, error) {
	prog, err := LoadProgram(cfg)
	if err != nil {
		return nil, err
	}
	return OracleProgram(cfg, prog), nil
}

// LoadProgram loads the program in cfg.Dir as a build would.
func LoadProgram(cfg *Config) (*load.Program, error) {
	lcfg, err := loadConfig(cfg)
	if err != nil {
		return nil, err
	}
	return load.Load(lcfg, cfg.Dir)
}

// OracleProgram is Oracle for a loaded program.
func OracleProgram(cfg *Config, prog *load.Program) *OracleInputs {
	calls, log, escErr := escape.AnalyzeLogged(prog)
	info, moveErr := moves.AnalyzeWith(prog, calls)
	var overlaps scoped.Overlaps
	if joinLifetimeErrors(moveErr, escErr) == nil {
		overlaps, _ = race.AnalyzeWith(prog, info)
	}
	facts, lowerErr := lower.LowerFacts(&lower.Config{
		Overlaps:     overlaps,
		Freestanding: cfg.Freestanding || cfg.Kernel,
		Kernel:       cfg.Kernel,
		Sizes:        sizes,
		NoFieldNames: !cfg.fieldNames(),
		GenCheck:     cfg.genCheck(),
		Moves:        info,
	}, prog)
	return &OracleInputs{Prog: prog, Log: log, Facts: facts, EscapeErr: escErr, LowerErr: lowerErr}
}
