package sched

import (
	"fmt"
	"reflect"
	"slices"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/solve"
	"github.com/matjam/plutonium/internal/escape2/summary"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/types2"
)

// TestParallelDeterministic checks that the parallel driver gives the same
// analysis whatever the number of workers: the same findings, summaries
// (path IDs included) and paths, on programs with many SCCs, with D49 on
// and off.
func TestParallelDeterministic(t *testing.T) {
	if testing.Short() {
		t.Skip("loads programs")
	}
	for _, dir := range []string{"../../../examples/15-interfaces", "../../../examples/24-errors", "../../../examples/22-maps"} {
		for _, defer_ := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/defer=%v", dir[len("../../../examples/"):], defer_), func(t *testing.T) {
				savedDefer := deferralOn
				deferralOn = defer_
				defer func() { deferralOn = savedDefer }()
				var want *analysisDump
				for _, n := range []int{1, 3, 8} {
					t.Setenv("PLUTO_ESCAPE2_JOBS", fmt.Sprint(n))
					got := analyse(t, dir)
					if want == nil {
						want = got
						continue
					}
					if !reflect.DeepEqual(got.findings, want.findings) {
						t.Fatalf("%d workers: findings differ from 1 worker's", n)
					}
					if !reflect.DeepEqual(got.sums, want.sums) {
						for i := range got.sums {
							if !reflect.DeepEqual(got.sums[i], want.sums[i]) {
								t.Fatalf("%d workers: the summary of function %d differs: %+v, want %+v", n, i, got.sums[i], want.sums[i])
							}
						}
					}
					if !reflect.DeepEqual(got.paths, want.paths) {
						t.Fatalf("%d workers: the paths differ (%d, want %d)", n, len(got.paths), len(want.paths))
					}
				}
			})
		}
	}
}

// TestParallelDeterministicAtLimits checks that whether a solve is past
// the solver's limits (E3010) does not depend on which worker runs it: with
// the memory limit lowered so that some of the programs' functions are past
// it, the analysis is the same with 1, 3 and 8 workers. A worker's
// workspace keeps memory from its earlier solves, and a solve that measured
// only what it added to it was past the limit or not by the worker's
// history.
func TestParallelDeterministicAtLimits(t *testing.T) {
	if testing.Short() {
		t.Skip("loads programs")
	}
	restore := solve.SetLimits(96<<10, solve.MaxFuncSteps)
	defer restore()
	for _, dir := range []string{"../../../examples/15-interfaces", "../../../examples/22-maps"} {
		t.Run(dir[len("../../../examples/"):], func(t *testing.T) {
			var want *analysisDump
			limited := 0
			for _, n := range []int{1, 3, 8} {
				t.Setenv("PLUTO_ESCAPE2_JOBS", fmt.Sprint(n))
				got := analyse(t, dir)
				if want == nil {
					want = got
					for _, f := range got.findings {
						if f.Code == codeEscapeLimit {
							limited++
						}
					}
					continue
				}
				if !reflect.DeepEqual(got.findings, want.findings) {
					t.Fatalf("%d workers: findings differ from 1 worker's", n)
				}
				if !reflect.DeepEqual(got.sums, want.sums) {
					t.Fatalf("%d workers: summaries differ from 1 worker's", n)
				}
			}
			if limited == 0 {
				t.Fatalf("no function past the lowered limit: the test does not test it")
			}
		})
	}
}

type analysisDump struct {
	findings []solve.Finding
	sums     []*summary.Summary
	paths    [][]int
}

func analyse(t *testing.T, dir string) *analysisDump {
	t.Helper()
	cfg := &load.Config{Tags: load.BuildTags(false, false, nil), Stdlib: plutonium.Stdlib(), Sizes: types2.SizesFor("gc", "amd64")}
	prog, err := load.Load(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	p, err := eir.Build(prog)
	if err != nil {
		t.Fatal(err)
	}
	res := RunWith(p, Options{})
	ps := res.Env.(*env).paths
	d := &analysisDump{findings: res.Findings, sums: slices.Clone(res.Summaries)}
	for i := 0; i < ps.Len(); i++ {
		var st []int
		for _, s := range ps.Steps(domain.PathID(i)) {
			st = append(st, int(s))
		}
		d.paths = append(d.paths, st)
	}
	return d
}

// TestParallelRoundReads checks that a solve's reads count for the whole
// round it ran in: a and b call each other, a's summary is its own key
// alone, b's adds a's. In the first round b reads a's summary before a has
// one, and a, committed first, grows: b must be solved again, though its
// read is committed after a's growth.
func TestParallelRoundReads(t *testing.T) {
	var order []eir.FuncID
	withFakes(t, &order)
	pr := newProg()
	a := pr.fn("a", eir.FuncKey{}, nil)
	b := pr.fn("b", eir.FuncKey{}, nil)
	a.callStatic(b)
	b.callStatic(a)
	inner := solveFunc
	solveFunc = func(f *eir.Func, en solve.Env, ws *solve.Workspace) solve.Result {
		r := inner(f, en, ws)
		if f.ID == a.f.ID {
			r.Summary.Esc = []domain.Key{domain.Key(f.ID)}
		}
		return r
	}
	res := RunWith(pr.p, Options{})
	got := map[domain.Key]bool{}
	for _, k := range res.Summaries[b.f.ID].Esc {
		got[k] = true
	}
	if !got[domain.Key(a.f.ID)] {
		t.Errorf("b's summary %v lacks a's key: b was not solved again after a grew", res.Summaries[b.f.ID].Esc)
	}
}
