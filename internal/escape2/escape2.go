package escape2

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/escape"
	"github.com/matjam/plutonium/internal/escape2/eir"
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
// Until stage 4 gives this analysis the facts about calls that the move
// checker reads, they come from internal/escape in every mode; in ModeNew
// its errors are dropped and this analysis's are reported instead.
func Analyze(prog *load.Program, mode Mode) (*escape.Calls, error) {
	calls, oldErr := escape.Analyze(prog)
	switch mode {
	case ModeNew:
		return calls, Check(prog)
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
	defer func() {
		if r := recover(); r != nil {
			res = nil
			err = &diag.Diagnostic{Code: diag.InternalError, Msg: fmt.Sprintf("internal compiler error: escape2: %v", r)}
		}
	}()
	p, res, err := Run(prog, opts)
	if err != nil {
		return nil, err
	}
	return res, Diagnostics(prog, p, res)
}

// Run builds the program's EIR and analyses it.
func Run(prog *load.Program, opts sched.Options) (*eir.Program, *sched.Result, error) {
	p, err := eir.Build(prog)
	if err != nil {
		return nil, nil, err
	}
	return p, sched.RunWith(p, opts), nil
}

// Diagnostics turns findings into errors. The messages are plain until
// stage 5 brings them to the quality of internal/escape's; the position and
// code are what both-mode comparison and the review suites check.
func Diagnostics(prog *load.Program, p *eir.Program, res *sched.Result) error {
	var l diag.List
	seen := map[string]bool{}
	for _, f := range res.Findings {
		pos := prog.Fset.Position(f.Pos)
		key := fmt.Sprintf("%s %s", pos, f.Code)
		if seen[key] {
			continue
		}
		seen[key] = true
		l = append(l, diag.New(pos, f.Code, "%s", message(p, f)))
	}
	return l.Err()
}

func message(p *eir.Program, f solve.Finding) string {
	what := "a frame object"
	if int(f.Func) < len(p.Funcs) {
		fn := p.Funcs[f.Func]
		if int(f.Obj) < len(fn.Objects) && fn.Objects[f.Obj].Name != "" {
			what = fn.Objects[f.Obj].Name
		}
	}
	where := map[eir.SinkKind]string{
		eir.SinkReturn: "is returned", eir.SinkGlobal: "is stored in package memory",
		eir.SinkSend: "is sent on a channel", eir.SinkTask: "is passed to a task",
		eir.SinkExternal: "is passed to code outside Plutonium",
	}[f.Sink]
	if where == "" {
		where = "outlives it"
	}
	return fmt.Sprintf("a pointer to %s %s, past the end of its frame", what, where)
}

// Site is one error of a verdict: its position and code.
type Site struct {
	Pos  string // file:line:col
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
		out[Site{Pos: d.Pos.String(), Code: d.Code}] = true
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
