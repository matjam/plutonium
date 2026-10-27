// Ported from internal/escape2/sched/parallel_test.go at the go-cutover tag.

package sched

import (
	"pluto/fmt"
	"pluto/strings"
	"pluto/testing"

	"compiler/compat"
	"compiler/compat/fs"
	"compiler/internal/escape2/domain"
	"compiler/internal/escape2/eir"
	"compiler/internal/escape2/solve"
	"compiler/internal/escape2/summary"
	"compiler/internal/load"
	"compiler/internal/types2"
	"compiler/pu"
)

// The tests that load programs ran in Go unless -short was given. Plutonium's
// tests have no -short: they run only with PLUTO_SLOW_TESTS set, and find the
// examples from PLUTOROOT (the repository's lib directory).
func loadsPrograms(t *testing.T) {
	if compat.Getenv("PLUTO_SLOW_TESTS") == "" {
		t.Skip("loads programs (set PLUTO_SLOW_TESTS)")
	}
	if compat.Getenv("PLUTOROOT") == "" {
		t.Skip("loads programs: PLUTOROOT is not set")
	}
}

// repoDir is the directory rel of the repository, PLUTOROOT's parent.
func repoDir(rel string) string {
	return compat.Concat(compat.Getenv("PLUTOROOT"), "/../", rel)
}

func baseName(dir string) string {
	if i := strings.LastIndex(dir, "/"); i >= 0 {
		return strings.SliceFrom(dir, i+1)
	}
	return dir
}

// TestParallelDeterministic checks that the parallel driver gives the same
// analysis whatever the number of workers: the same findings, summaries
// (path IDs and the entries their reasons name included) and paths, on
// programs with many SCCs, with D49 on and off. iface_receiver_pointers
// calls through an interface whose candidates' entries are filtered, so
// the reasons of its callers name entries of a join whose order the
// filters' IDs decide.
func TestParallelDeterministic(t *testing.T) {
	loadsPrograms(t)
	for _, dir := range []string{"examples/15-interfaces", "examples/24-errors", "examples/22-maps", "tests/diag/iface_receiver_pointers"} {
		for _, defer_ := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/defer=%v", baseName(dir), defer_), func(t *testing.T) {
				savedDefer := deferralOn
				deferralOn = defer_
				defer func() { deferralOn = savedDefer }()
				savedJobs := testJobs
				defer func() { testJobs = savedJobs }()
				var want *analysisDump
				for _, n := range []int{1, 3, 8} {
					testJobs = n
					got := analyse(t, repoDir(dir))
					if want == nil {
						want = got
						continue
					}
					if !findingsEqual(got.findings, want.findings) {
						t.Fatalf("%d workers: findings differ from 1 worker's", n)
					}
					if !sumsEqual(got.sums, want.sums) {
						for i := range got.sums {
							if i >= len(want.sums) || !sumEqual(got.sums[i], want.sums[i]) {
								t.Fatalf("%d workers: the summary of function %d differs: %+v, want %+v", n, i, got.sums[i], want.sums[i])
							}
						}
						t.Fatalf("%d workers: %d summaries, want %d", n, len(got.sums), len(want.sums))
					}
					if !pathsEqual(got.paths, want.paths) {
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
	loadsPrograms(t)
	restore := solve.SetLimits(96<<10, solve.MaxFuncSteps)
	defer restore()
	for _, dir := range []string{"examples/15-interfaces", "examples/22-maps"} {
		t.Run(strings.SliceFrom(dir, len("examples/")), func(t *testing.T) {
			savedJobs := testJobs
			defer func() { testJobs = savedJobs }()
			var want *analysisDump
			limited := 0
			for _, n := range []int{1, 3, 8} {
				testJobs = n
				got := analyse(t, repoDir(dir))
				if want == nil {
					want = got
					for _, f := range got.findings {
						if f.Code == codeEscapeLimit {
							limited++
						}
					}
					continue
				}
				if !findingsEqual(got.findings, want.findings) {
					t.Fatalf("%d workers: findings differ from 1 worker's", n)
				}
				if !sumsEqual(got.sums, want.sums) {
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

// findingsEqual, sumsEqual and pathsEqual stand for the Go test's
// reflect.DeepEqual. A summary is compared by its %+v form, which shows
// every field (it does not tell a nil slice from an empty one).
func findingsEqual(a, b []solve.Finding) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sumEqual(a, b *summary.Summary) bool {
	if a == nil || b == nil {
		return a == b
	}
	return fmt.Sprintf("%+v", *a) == fmt.Sprintf("%+v", *b)
}

func sumsEqual(a, b []*summary.Summary) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !sumEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

func pathsEqual(a, b [][]int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if len(a[i]) != len(b[i]) {
			return false
		}
		for j := range a[i] {
			if a[i][j] != b[i][j] {
				return false
			}
		}
	}
	return true
}

func loadConfig() *load.Config {
	return pu.New(load.Config{Tags: load.BuildTags(false, false, nil), Stdlib: fs.DirFS(compat.Getenv("PLUTOROOT")), Sizes: types2.SizesFor("gc", "amd64")})
}

func analyse(t *testing.T, dir string) *analysisDump {
	t.Helper()
	prog, err := load.Load(loadConfig(), dir)
	if err != nil {
		t.Fatal(err)
	}
	p, err := eir.Build(prog)
	if err != nil {
		t.Fatal(err)
	}
	res := RunWith(p, Options{})
	ps := res.Env.(*env).paths
	d := pu.New(analysisDump{findings: res.Findings, sums: compat.Clone(res.Summaries)})
	for i := 0; i < ps.Len(); i++ {
		var st []int
		for _, s := range ps.Steps(domain.PathID(i)) {
			st = compat.Append(st, int(s))
		}
		d.paths = compat.Append(d.paths, st)
	}
	return d
}

// TestParallelRoundReads's solver: a's summary is its own key alone.
var roundReadsA eir.FuncID

func roundReadsSolve(f *eir.Func, en solve.Env, ws *solve.Workspace) solve.Result {
	r := fakeSolve(f, en, ws)
	if f.ID == roundReadsA {
		r.Summary.Esc = compat.Append[domain.Key](nil, domain.Key(f.ID))
	}
	return r
}

// TestParallelRoundReads checks that a solve's reads count for the whole
// round it ran in: a and b call each other, a's summary is its own key
// alone, b's adds a's. In the first round b reads a's summary before a has
// one, and a, committed first, grows: b must be solved again, though its
// read is committed after a's growth.
func TestParallelRoundReads(t *testing.T) {
	withFakes(t)
	defer restoreFakes()
	pr := newProg()
	a := pr.fn("a", eir.FuncKey{}, nil)
	b := pr.fn("b", eir.FuncKey{}, nil)
	a.callStatic(b)
	b.callStatic(a)
	roundReadsA = a.f.ID
	solveFunc = roundReadsSolve
	res := RunWith(pr.p, Options{})
	got := map[domain.Key]bool{}
	for _, k := range res.Summaries[b.f.ID].Esc {
		got[k] = true
	}
	if !got[domain.Key(a.f.ID)] {
		t.Errorf("b's summary %v lacks a's key: b was not solved again after a grew", res.Summaries[b.f.ID].Esc)
	}
}

// TestReleasedArenasUnused checks that no state is read from an arena after
// the driver released it: with checked arenas, which poison their memory on
// Release, the analysis is the same as with plain ones. It runs the
// parallel and the sequential driver, with the memory limit as it is and
// lowered so that some solves are past it (their workspaces are spares
// released at once), and with D49 off, as the parallel driver keeps
// solver states (and releases the dropped ones) only then.
func TestReleasedArenasUnused(t *testing.T) {
	loadsPrograms(t)
	for _, low := range []bool{false, true} {
		for _, dir := range []string{"examples/15-interfaces", "examples/22-maps"} {
			for _, seq := range []bool{false, true} {
				for _, defer_ := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/low=%v/seq=%v/defer=%v", strings.SliceFrom(dir, len("examples/")), low, seq, defer_), func(t *testing.T) {
						savedDefer := deferralOn
						deferralOn = defer_
						defer func() { deferralOn = savedDefer }()
						if low {
							restore := solve.SetLimits(96<<10, solve.MaxFuncSteps)
							defer restore()
						}
						savedSeq := sequential
						sequential = seq
						defer func() { sequential = savedSeq }()
						savedJobs := testJobs
						testJobs = 3
						defer func() { testJobs = savedJobs }()
						want := analyse(t, repoDir(dir))
						saved := newArena
						newArena = domain.NewCheckedArena
						defer func() { newArena = saved }()
						got := analyse(t, repoDir(dir))
						if !findingsEqual(got.findings, want.findings) {
							t.Fatalf("findings differ with checked arenas")
						}
						if !sumsEqual(got.sums, want.sums) {
							t.Fatalf("summaries differ with checked arenas")
						}
						if !pathsEqual(got.paths, want.paths) {
							t.Fatalf("paths differ with checked arenas")
						}
					})
				}
			}
		}
	}
}

// TestReasonsNameRenamedEntries checks that a reason names the callee's
// entry it came from after canonicalFilters renumbers the filters and so
// reorders the entries of filtered summaries. In iface_receiver_pointers,
// call passes its parameter to an interface method whose candidates keep
// their receivers under two filters: each entry of call's summary is the
// entry of the join its reason names, key for key. Many workers intern the
// filters in an order their timing decides, so the test runs the analysis
// a few times.
func TestReasonsNameRenamedEntries(t *testing.T) {
	loadsPrograms(t)
	savedJobs := testJobs
	testJobs = 24
	defer func() { testJobs = savedJobs }()
	prog, err := load.Load(loadConfig(), repoDir("tests/diag/iface_receiver_pointers"))
	if err != nil {
		t.Fatal(err)
	}
	for run := 0; run < 8; run++ {
		p, err := eir.Build(prog)
		if err != nil {
			t.Fatal(err)
		}
		res := RunWith(p, Options{})
		en := res.Env.(*env)
		checked := 0
		for _, f := range p.Funcs {
			if f.Name != "call" && !strings.HasSuffix(f.Name, ".call") {
				continue
			}
			s := res.Summaries[f.ID]
			for i, k := range s.Esc {
				r := s.Reasons[i]
				v := int(r.Callee) - len(p.Funcs)
				if v < 0 || v >= len(en.virt.cache) {
					continue
				}
				vs := en.virt.cache[v]
				if int(r.Entry) >= len(vs.Esc) || vs.Esc[r.Entry] != k {
					t.Fatalf("run %d: call's entry %d (%v) names entry %d of the join, whose entries are %v", run, i, k, r.Entry, vs.Esc)
				}
				checked++
			}
		}
		if checked < 2 {
			t.Fatalf("run %d: %d entries of call name the join: the test does not test it", run, checked)
		}
	}
}
