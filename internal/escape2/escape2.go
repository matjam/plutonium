package escape2

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/escape"
	"github.com/matjam/plutonium/internal/escape2/cache"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/report"
	"github.com/matjam/plutonium/internal/escape2/sched"
	"github.com/matjam/plutonium/internal/escape2/solve"
	"github.com/matjam/plutonium/internal/load"
)

// Mode says which escape analysis a build runs (plan section (g)).
type Mode uint8

const (
	ModeOld  Mode = iota // internal/escape alone: the default
	ModeNew              // this analysis decides the verdict
	ModeBoth             // both run; the old one decides, and every difference is reported
)

// ModeFromEnv reads PLUTO_ESCAPE: "" or "old", "new", "both".
func ModeFromEnv() (Mode, error) {
	switch v := os.Getenv("PLUTO_ESCAPE"); v {
	case "", "old":
		return ModeOld, nil
	case "new":
		return ModeNew, nil
	case "both":
		return ModeBoth, nil
	default:
		return ModeOld, fmt.Errorf("PLUTO_ESCAPE=%q: want old, new or both", v)
	}
}

// Analyze runs the escape analysis that mode selects, as internal/build
// calls it: what calls do with their arguments (for internal/moves) and
// the escape errors.
//
// In ModeNew internal/escape does not run: the facts about calls are this
// analysis's (calls.go). In ModeBoth they are internal/escape's, whose
// verdict stands.
func Analyze(prog *load.Program, mode Mode) (*escape.Calls, error) {
	if mode == ModeNew {
		return CheckCalls(prog)
	}
	calls, oldErr := escape.Analyze(prog)
	switch mode {
	case ModeBoth:
		newErr := Check(prog)
		if d := Compare(prog, oldErr, newErr); d != nil {
			d.report()
		}
	}
	return calls, oldErr
}

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
func CheckCalls(prog *load.Program) (calls *escape.Calls, err error) {
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
	if dir := os.Getenv(cache.EnvVar); dir != "" && opts.Cache == nil {
		if c, err = cache.Open(dir, cache.Config{}); err != nil {
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
		var lim *solve.LimitError
		if errors.As(rerr, &lim) {
			return &diag.Diagnostic{Code: diag.EscapeLimit, Msg: lim.Error()}
		}
	}
	return &diag.Diagnostic{Code: diag.InternalError, Msg: fmt.Sprintf("internal compiler error: escape2: %v", r)}
}

// Run builds the program's EIR and analyses it.
func Run(prog *load.Program, opts sched.Options) (*eir.Program, *sched.Result, error) {
	p, err := eir.Build(prog)
	if err != nil {
		return nil, nil, err
	}
	return p, sched.RunWith(p, opts), nil
}

// Diagnostics turns findings into errors, with the messages of
// internal/escape2/report.
func Diagnostics(prog *load.Program, p *eir.Program, res *sched.Result) error {
	l := diag.List(report.Diagnostics(prog, p, res))
	return l.Err()
}

// Site is one error of a verdict: its position and code.
type Site struct {
	Pos  string // file:line: the two analyses point at different columns of one line (the address taken, the statement that keeps it)
	Code diag.Code
}

func (s Site) String() string { return s.Pos + " " + string(s.Code) }

// Disagreement is a program on which the two analyses differ.
type Disagreement struct {
	Program    string
	OldRejects bool
	NewRejects bool
	OnlyOld    []Site // errors the old analysis reports and the new one does not
	OnlyNew    []Site // and the reverse
	NewFailed  string // the new analysis failed with an internal error
}

// VerdictsDiffer reports whether one accepts and the other rejects.
func (d *Disagreement) VerdictsDiffer() bool { return d.OldRejects != d.NewRejects }

// Compare compares the errors of the two analyses on prog and returns nil
// when they agree on the verdict and on every error site.
func Compare(prog *load.Program, oldErr, newErr error) *Disagreement {
	d := &Disagreement{Program: programName(prog), OldRejects: oldErr != nil, NewRejects: newErr != nil}
	if newErr != nil && isInternal(newErr) {
		d.NewFailed = newErr.Error()
	}
	oldS, newS := sites(oldErr), sites(newErr)
	for s := range oldS {
		if !newS[s] {
			d.OnlyOld = append(d.OnlyOld, s)
		}
	}
	for s := range newS {
		if !oldS[s] {
			d.OnlyNew = append(d.OnlyNew, s)
		}
	}
	sortSites(d.OnlyOld)
	sortSites(d.OnlyNew)
	if !d.VerdictsDiffer() && len(d.OnlyOld) == 0 && len(d.OnlyNew) == 0 && d.NewFailed == "" {
		return nil
	}
	return d
}

func isInternal(err error) bool {
	if l, ok := err.(*diag.List); ok {
		for _, d := range *l {
			if d.Code == diag.InternalError {
				return true
			}
		}
		return false
	}
	if d, ok := err.(*diag.Diagnostic); ok {
		return d.Code == diag.InternalError
	}
	return true
}

func sites(err error) map[Site]bool {
	out := map[Site]bool{}
	add := func(d *diag.Diagnostic) {
		if d.Code == diag.InternalError {
			return
		}
		out[Site{Pos: fmt.Sprintf("%s:%d", d.Pos.Filename, d.Pos.Line), Code: d.Code}] = true
	}
	switch e := err.(type) {
	case nil:
	case *diag.List:
		for _, d := range *e {
			add(d)
		}
	case *diag.Diagnostic:
		add(e)
	}
	return out
}

func sortSites(s []Site) {
	sort.Slice(s, func(i, j int) bool {
		if s[i].Pos != s[j].Pos {
			return s[i].Pos < s[j].Pos
		}
		return s[i].Code < s[j].Code
	})
}

func programName(prog *load.Program) string {
	if prog.Main != nil {
		if prog.Main.Dir != "" {
			return prog.Main.Dir
		}
		return prog.Main.Path
	}
	return "(program)"
}

// String formats the disagreement as both mode reports it.
func (d *Disagreement) String() string {
	var b strings.Builder
	verdict := func(rejects bool) string {
		if rejects {
			return "rejects"
		}
		return "accepts"
	}
	if d.VerdictsDiffer() {
		fmt.Fprintf(&b, "escape2: %s: verdicts differ: old %s, new %s\n", d.Program, verdict(d.OldRejects), verdict(d.NewRejects))
	} else {
		fmt.Fprintf(&b, "escape2: %s: verdicts agree (%s), error sites differ\n", d.Program, verdict(d.OldRejects))
	}
	if d.NewFailed != "" {
		fmt.Fprintf(&b, "\tnew analysis failed: %s\n", firstLine(d.NewFailed))
	}
	for _, s := range d.OnlyOld {
		fmt.Fprintf(&b, "\told only: %s\n", s)
	}
	for _, s := range d.OnlyNew {
		fmt.Fprintf(&b, "\tnew only: %s\n", s)
	}
	return b.String()
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

var reportMu sync.Mutex

// report writes the disagreement to standard error and, when
// PLUTO_ESCAPE_DIFF names a file, appends it there (builds run in
// parallel, so the file collects the report of a whole test run).
func (d *Disagreement) report() {
	reportMu.Lock()
	defer reportMu.Unlock()
	text := d.String()
	_, _ = io.WriteString(os.Stderr, text)
	if name := os.Getenv("PLUTO_ESCAPE_DIFF"); name != "" {
		f, err := os.OpenFile(name, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "escape2: PLUTO_ESCAPE_DIFF: %v\n", err)
			return
		}
		_, _ = f.WriteString(text)
		_ = f.Close()
	}
}
