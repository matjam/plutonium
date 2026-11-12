// Translated by go2pu from internal/escape2/escape2.go at the go-cutover tag; edited by hand since.

package escape2

import (
	"compiler/compat"
	"compiler/internal/diag"
	"compiler/internal/escape2/cache"
	"compiler/internal/escape2/domain"
	"compiler/internal/escape2/eir"
	"compiler/internal/escape2/report"
	"compiler/internal/escape2/sched"
	"compiler/internal/escape2/solve"
	"compiler/internal/load"
	"compiler/internal/moves"
	"compiler/pu"
	"pluto/errors"
	"pluto/fmt"
	"pluto/os"
)

// Check runs this analysis on prog and returns its errors, sorted, as a
// diag.List. An internal error of the analysis (a construct the desugarer
// does not know, a solver that does not converge) is reported as an
// internal compiler error, never as acceptance.
func Check(prog *load.Program) error {
	_, err := CheckWith(prog, sched.Options{})
	return err
}

// CheckWith is Check with options for the scheduler, and returns the
// analysis's result as well (nil when it failed).
func CheckWith(prog *load.Program, opts sched.Options) (res *sched.Result, err error) {
	defer recoverInternal(&res, &err)
	return checkWith(prog, opts)
}

// CheckCalls is Check, and returns this analysis's facts about the calls
// of the program for internal/moves (CallFacts), or nil when the analysis
// failed (moves then takes every call at its worst).
func CheckCalls(prog *load.Program) (calls *moves.Calls, err error) {
	var res *sched.Result
	defer recoverInternal(&res, &err)
	p, res, err := checkProgram(prog, sched.Options{})
	if res == nil {
		return nil, err
	}
	return CallFacts(p, res), err
}

func checkWith(prog *load.Program, opts sched.Options) (*sched.Result, error) {
	_, res, err := checkProgram(prog, opts)
	return res, err
}

// checkProgram is checkWith, and returns the program's EIR as well.
func checkProgram(prog *load.Program, opts sched.Options) (*eir.Program, *sched.Result, error) {
	var c *cache.Cache
	var err error
	if dir := compat.Getenv(cache.EnvVar); dir != "" && opts.Cache == nil {
		if c, err = cache.Open(dir, cache.Config{Flags: sched.CacheFlags()}); err != nil {
			fmt.Fprintf(os.Stderr, "escape2: %v\n", err) // no cache: costs time only
		} else {
			opts.Cache = c
		}
	}
	p, res, err := Run(prog, opts)
	if err != nil {
		return nil, nil, err
	}
	if c != nil {
		if err := c.Save(res); err != nil {
			fmt.Fprintf(os.Stderr, "escape2: %v\n", err)
		}
	}
	return p, res, Diagnostics(prog, p, res)
}

// recoveredError is the diagnostic for a panic of the analysis: a function too
// large for the solver's limits is the program's error (E3010, section 18.3),
// anything else an internal compiler error.
func recoveredError(r any) error {
	if rerr, ok := r.(error); ok {
		lim := pu.Alloc[*solve.LimitError]()
		if errors.As(rerr, lim) {
			return pu.New(diag.Diagnostic{Code: diag.EscapeLimit, Msg: (*lim).Error()})
		}
	}
	return pu.New(diag.Diagnostic{Code: diag.InternalError, Msg: compat.Sprintf("internal compiler error: escape2: %v", r)})
}

// Run builds the program's EIR and analyses it.
func Run(prog *load.Program, opts sched.Options) (*eir.Program, *sched.Result, error) {
	defer domain.ForgetPool()
	p := eir.Build(prog) or return
	return p, sched.RunWith(p, opts), nil
}

// Diagnostics turns findings into errors, with the messages of
// internal/escape2/report, after the errors of the directives it reads.
func Diagnostics(prog *load.Program, p *eir.Program, res *sched.Result) error {
	l := pu.New(diag.List(compat.Append(compat.Append(directiveErrors(prog), dropKeepErrors(prog, p, res)...), report.Diagnostics(prog, p, res)...)))
	return l.Err()
}
