package solve

import (
	"fmt"

	"github.com/matjam/plutonium/internal/escape2/eir"
)

// MaxFuncArena is the memory one solve may use: an implementation limit,
// not a precision limit. The analysis never widens inside the limits of
// section 18.3 to stay under it; a function that needs more makes Solve
// return a LimitError (Result.Limit), which the driver reports as an error naming the
// function (never as acceptance). Measured (TestMeasure, 2026-10-21, over
// the corpus and the three review suites, 813,235 solves): the largest is
// main.main of testdata/lib/chunked.p at 67 MB and 17 million steps (26,589
// nodes from its many calls, 221 tokens); the most tokens of one input are
// 1,089 (pluto/hash.NewSet).
const MaxFuncArena = 256 << 20

// MaxFuncSteps bounds the work of one solve (constraint firings and edge
// transforms), an implementation limit like MaxFuncArena: a function that
// grows slowly would otherwise run for minutes before its memory says so.
const MaxFuncSteps = 1_000_000_000

// The limits checkLimit applies; the tests lower them.
var (
	maxFuncArena = MaxFuncArena
	maxFuncSteps = MaxFuncSteps
)

// LimitError is the error of a solve past MaxFuncArena or MaxFuncSteps
// (Result.Limit).
type LimitError struct {
	Func  string
	Bytes int
	Steps int
}

func (e *LimitError) Error() string {
	what := fmt.Sprintf("memory limit (%d MB)", e.Bytes>>20)
	if e.Bytes <= maxFuncArena {
		what = fmt.Sprintf("work limit (%d steps)", e.Steps)
	}
	return fmt.Sprintf("the escape analysis of %s reached its %s: the function holds more distinct pointers than the analysis can follow; split it into smaller functions", e.Func, what)
}

// checkLimit stops the solve with a LimitError (s.limit) when it has used
// more than MaxFuncArena or MaxFuncSteps, and reports whether it stopped.
// It is an error value, not a panic, so that the analysis translated to
// Plutonium, whose panics do not unwind, stops the same way.
func (s *solver) checkLimit() bool {
	if s.limit != nil {
		return true
	}
	if b := s.a.Allocated() - s.base; b > maxFuncArena || s.nsteps > maxFuncSteps {
		s.dump(nil)
		s.measureLimited(true)
		s.limit = &LimitError{Func: s.f.Name, Bytes: b, Steps: s.nsteps}
		return true
	}
	return false
}

// Measurement is what one solve used, for the measurements of the plan
// (sections (e), (h)).
type Measurement struct {
	Tokens         int // distinct tagged tokens
	MaxInputTokens int // the most tokens of one input (In tokens)
	Nodes          int
	ArenaBytes     int  // what the solve allocated in the arena
	Steps          int  // constraint firings and edge transforms
	Limited        bool // the solve stopped at a limit (LimitError)
}

// Measure, when set, is called at the end of every solve with what it used,
// and before the panic of a solve stopped at a limit (Limited set). It is for
// measurements (TestMeasure), not for the compiler.
var Measure func(f *eir.Func, m Measurement)

func (s *solver) measure() { s.measureLimited(false) }

func (s *solver) measureLimited(limited bool) {
	if Measure == nil {
		return
	}
	m := Measurement{Tokens: len(s.toks.s) / 2, Nodes: len(s.nodeRep.s), ArenaBytes: s.a.Allocated() - s.base, Steps: s.nsteps, Limited: limited}
	per := map[uint32]int{}
	for i := 0; i < m.Tokens; i++ {
		if t := s.tokAt(uint64(i)); t.kind == tIn {
			per[t.base]++
		}
	}
	for _, n := range per {
		m.MaxInputTokens = max(m.MaxInputTokens, n)
	}
	Measure(s.f, m)
}
