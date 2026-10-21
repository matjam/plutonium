package refsolve

import (
	"fmt"
	"strings"

	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/solve"
	"github.com/matjam/plutonium/internal/escape2/summary"
)

func resultOf(sm *summary.Summary) solve.Result { return solve.Result{Summary: sm} }

// Available reports whether solve.Solve and the domain constructors are built
// (their interface stubs panic), so the differential tests can skip until
// they are.
func Available() (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	ps := domain.NewPaths()
	a := domain.NewArena()
	ws := &solve.Workspace{Arena: a, Sets: domain.NewSets(a)}
	c := Random(1, ps)
	solve.Solve(c.F, c.Env, ws)
	return true
}

// Compare runs the solver and the reference solver on f in env, giving both
// the same path interner (env.Paths()), and returns "" when their canonical
// results are equal, else a diff of the two.
//
// Each solver is bounded so that a case that grows without end fails with
// its seed instead of taking the machine's memory: the solver's arena is
// capped at MaxCaseBytes (its sets live in it), and the reference solver at
// MaxPasses passes and MaxValues tagged tokens (Solve panics past them).
func Compare(c *Case) string {
	a := &cappedArena{Arena: domain.NewArena(), max: MaxCaseBytes}
	ws := &solve.Workspace{Arena: a, Sets: domain.NewSets(a)}
	got := Canon(c.Env.P, solve.Solve(c.F, c.Env, ws))
	ws.Sets.Reset()
	a.Reset()
	want := Canon(c.Env.P, Solve(c.F, c.Env))
	return Diff(got, want)
}

// MaxCaseBytes caps the solver's arena on one case of the differential test.
const MaxCaseBytes = 256 << 20

// cappedArena panics when more than max bytes would be held.
type cappedArena struct {
	domain.Arena
	max int
}

func (a *cappedArena) Alloc(n int) []uint64 {
	if a.Bytes()+8*n > a.max {
		panic(fmt.Sprintf("escape2/refsolve: the solver's arena passed %d bytes on one case", a.max))
	}
	return a.Arena.Alloc(n)
}

// Diff is "" when got and want are equal, else the lines only in one of them.
func Diff(got, want string) string {
	if got == want {
		return ""
	}
	gl, wl := strings.Split(got, "\n"), strings.Split(want, "\n")
	in := func(ls []string, x string) bool {
		for _, l := range ls {
			if l == x {
				return true
			}
		}
		return false
	}
	var b strings.Builder
	for _, l := range gl {
		if !in(wl, l) {
			fmt.Fprintf(&b, "solve only: %s\n", l)
		}
	}
	for _, l := range wl {
		if !in(gl, l) {
			fmt.Fprintf(&b, "refsolve only: %s\n", l)
		}
	}
	return b.String()
}
