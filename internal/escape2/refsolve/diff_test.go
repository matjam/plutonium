package refsolve

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/matjam/plutonium/internal/escape2/domain"
)

// cases is the number of random cases of each test: REFSOLVE_CASES, default
// 3000.
func cases() int {
	if s := os.Getenv("REFSOLVE_CASES"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
	}
	return 3000
}

// TestRandomReferenceOnly runs the reference solver on random inputs with the
// naive paths: it must terminate, be deterministic, and produce summaries
// that are antichains with entries in range.
func TestRandomReferenceOnly(t *testing.T) {
	for seed := int64(0); seed < int64(cases()); seed++ {
		ps := NewNaivePaths()
		c := Random(seed, ps)
		r1 := Canon(ps, Solve(c.F, c.Env))
		r2 := Canon(ps, Solve(c.F, c.Env))
		if r1 != r2 {
			t.Fatalf("not deterministic:\n%s\n%s\n%s", c.Dump(), r1, r2)
		}
		sm := Solve(c.F, c.Env).Summary
		for _, k := range sm.Esc {
			for _, o := range sm.Esc {
				if k != o && domain.Covers(ps, o, k) {
					t.Fatalf("Esc is not an antichain: %s covers %s\n%s", CanonKey(ps, o), CanonKey(ps, k), c.Dump())
				}
			}
			if k.Input() >= len(c.F.Inputs) {
				t.Fatalf("key of input %d, function has %d", k.Input(), len(c.F.Inputs))
			}
		}
	}
}

// TestDifferential runs solve.Solve and the reference solver on random
// inputs and on hand-built ones, and requires equal summaries and findings. A
// disagreement is a bug in one of them.
func TestDifferential(t *testing.T) {
	if !Available() {
		t.Skip("solve.Solve or domain is not built yet")
	}
	from := int64(0)
	if v, err := strconv.Atoi(os.Getenv("REFSOLVE_FROM")); err == nil {
		from = int64(v)
	}
	n := cases()
	if ran := runRange(t, from, n, os.Getenv("REFSOLVE_TRACE") != ""); ran != n {
		t.Fatalf("ran %d cases of %d", ran, n)
	}
	t.Logf("seeds %d to %d: %d cases, both solvers on each", from, from+int64(n)-1, n)
}

// runRange compares the two solvers on seeds from to from+n-1 and returns how
// many cases ran both solvers (Compare counts them).
func runRange(t *testing.T, from int64, n int, trace bool) int {
	t.Helper()
	before := compared.Load()
	for seed := from; seed < from+int64(n); seed++ {
		if trace {
			fmt.Fprintln(os.Stderr, "seed", seed)
		}
		c := Random(seed, domain.NewPaths())
		if d := compareBounded(c); d != "" {
			t.Fatalf("solve and refsolve differ on seed %d:\n%s\n%s", seed, d, c.Dump())
		}
	}
	return int(compared.Load() - before)
}

// A run over a range of seeds runs both solvers on every seed of it, as a
// run from seed 0 does, and a range far from 0 makes cases of the same size
// as one near it (the generator does not degenerate with large seeds).
func TestRangeRunsEveryCase(t *testing.T) {
	if got := runRange(t, 0, 50, false); got != 50 {
		t.Fatalf("from 0: %d cases of 50", got)
	}
	if got := runRange(t, 20000, 50, false); got != 50 {
		t.Fatalf("from 20000: %d cases of 50", got)
	}
	size := func(from int64) int {
		n := 0
		for seed := from; seed < from+200; seed++ {
			for _, b := range Random(seed, domain.NewPaths()).F.Blocks {
				n += len(b.Instrs)
			}
		}
		return n
	}
	if a, b := size(0), size(20000); b*2 < a || a*2 < b {
		t.Errorf("instructions in 200 cases: %d from seed 0, %d from seed 20000", a, b)
	}
}

// caseTime bounds one case of the differential test.
const caseTime = 20 * time.Second

// compareBounded is Compare, with a bound passed (a panic of either solver
// or of the capped arena, or caseTime) reported as a difference, so the seed
// is named. A case past caseTime is left running; the test fails at once.
func compareBounded(c *Case) string {
	done := make(chan string, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Sprintf("bound or panic: %v", r)
			}
		}()
		done <- Compare(c)
	}()
	select {
	case d := <-done:
		return d
	case <-time.After(caseTime):
		return fmt.Sprintf("time bound: the case ran past %v", caseTime)
	}
}

// Seed 5401 is a random function whose one input reached about 260,000
// distinct tokens within the path limits of section 18.3 before the solvers
// dropped tokens a GE token covers (rules.go, "GE subsumption"); now it is
// an ordinary case, and both solvers must agree on it quickly.
func TestSeed5401(t *testing.T) {
	if got := runRange(t, 5401, 1, false); got != 1 {
		t.Fatalf("ran %d cases", got)
	}
}

// TestRandomMakesDeep: the random cases make Deep and FieldsGE keys of their
// own (an offset anywhere in an object, eir.AnyStep, then a load), so the
// differential test compares the two solvers on them, not only on callee
// summaries that hold them.
func TestRandomMakesDeep(t *testing.T) {
	deep, fge := 0, 0
	for seed := int64(0); seed < 3000; seed++ {
		ps := domain.NewPaths()
		c := Random(seed, ps)
		sm := Solve(c.F, c.Env).Summary
		keys := append(append([]domain.Key(nil), sm.Esc...), sm.HeapStore...)
		for _, p := range sm.Store {
			keys = append(keys, p.Src, p.Dst)
		}
		for _, r := range sm.Res {
			for _, rk := range r.From {
				keys = append(keys, rk.Key)
			}
		}
		for _, k := range keys {
			if k.Deep() {
				deep++
			}
			if k.FieldsGE() {
				fge++
			}
		}
	}
	if deep == 0 || fge == 0 {
		t.Fatalf("3000 random cases made %d Deep and %d FieldsGE keys", deep, fge)
	}
	t.Logf("%d Deep, %d FieldsGE keys", deep, fge)
}
