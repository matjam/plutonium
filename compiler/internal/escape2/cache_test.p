// Ported from internal/escape2/cache/cache_test.go at the go-cutover tag.
// The Go file was the external package cache_test, which imports
// internal/escape2; pluto test has no external test packages, so the test
// is in package escape2 (and the hooks of export_test.go are
// cache/testhooks.p). loadSrc is the one of escape2_test.p. t.TempDir is
// testenv.TempDir, removed when the test is done.

package escape2

import (
	"compiler/compat"
	"pluto/bytes"
	"pluto/path/filepath"
	"pluto/slices"
	"pluto/strings"
	"pluto/testing"

	"compiler/internal/escape2/cache"
	"compiler/internal/escape2/domain"
	"compiler/internal/escape2/eir"
	"compiler/internal/escape2/sched"
	"compiler/internal/load"
	"compiler/internal/testenv"
	"compiler/internal/types2"
	"compiler/pu"
)

// outcome is one run of the analysis.
type outcome struct {
	diags string                        // the diagnostics, as the build prints them
	sums  [][]byte                      // each function's summary, canonically encoded
	sccs  map[string]sched.CacheOutcome // by the names of the SCC's functions
	stats sched.Stats
	c     *cache.Cache
}

// sccLog is what logSCC was told of a run's SCCs. In Go these were
// variables of analyse that the OnSCC function literal used; sched keeps
// its options (the Result's environment holds them), so a function that
// uses a frame's variables cannot be given to it.
var sccLog struct {
	comps [][]eir.FuncID
	outs  []sched.CacheOutcome
}

func logSCC(comp []eir.FuncID, st sched.SCCStats) {
	sccLog.comps = compat.Append(sccLog.comps, compat.Clone(comp))
	sccLog.outs = compat.Append(sccLog.outs, st.Cache)
}

// analyse runs the analysis on prog, with the cache in dir ("" for none),
// and saves the cache.
func analyse(t *testing.T, prog *load.Program, dir string) outcome {
	t.Helper()
	var out outcome
	out.sccs = map[string]sched.CacheOutcome{}
	sccLog.comps, sccLog.outs = nil, nil
	opts := sched.Options{OnSCC: logSCC}
	if dir != "" {
		c, err := cache.Open(dir, cache.Config{Compiler: "test"})
		if err != nil {
			t.Fatal(err.Error())
		}
		opts.Cache = c
		out.c = c
	}
	res, err := CheckWith(prog, opts)
	if err != nil {
		out.diags = err.Error()
	}
	if res == nil {
		t.Fatalf("analysis failed: %v", err)
	}
	out.stats = res.Stats
	ps := res.Env.Paths()
	for _, s := range res.Summaries {
		out.sums = compat.Append(out.sums, cache.EncodeSummary(ps, s))
	}
	p := res.Env.Program()
	comps, outs := sccLog.comps, sccLog.outs
	for i, comp := range comps {
		var names []string
		for _, id := range comp {
			names = compat.Append(names, p.Funcs[id].Name)
		}
		// A redone SCC reports twice: keep what the first pass did.
		if k := compat.Join(names, ","); outs[i] != sched.CacheRedone {
			out.sccs[k] = outs[i]
		}
	}
	if out.c != nil {
		if err := out.c.Save(res); err != nil {
			t.Fatal(err.Error())
		}
	}
	return out
}

func loadDir(t *testing.T, dir string, tags map[string]bool) *load.Program {
	t.Helper()
	cfg := pu.New(load.Config{Tags: tags, Stdlib: testenv.Stdlib(), Sizes: types2.SizesFor("gc", "amd64")})
	prog, err := load.Load(cfg, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return prog
}

// sameAsCold checks that a run gave the diagnostics and summaries of a run
// without a cache.
func sameAsCold(t *testing.T, what string, cold, got outcome) {
	t.Helper()
	if got.diags != cold.diags {
		t.Errorf("%s: diagnostics differ from a run without the cache:\n%s\nwant:\n%s", what, got.diags, cold.diags)
	}
}

func sameSummaries(t *testing.T, what string, cold, got outcome) {
	t.Helper()
	if len(got.sums) != len(cold.sums) {
		t.Fatalf("%s: %d summaries, want %d", what, len(got.sums), len(cold.sums))
	}
	for i := range got.sums {
		if !bytes.Equal(got.sums[i], cold.sums[i]) {
			t.Errorf("%s: the summary of function %d differs from a run without the cache", what, i)
		}
	}
}

// readDir returns the files of a cache directory by name.
func readDir(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	for _, f := range cache.Files(dir) {
		data, err := compat.ReadFile(f)
		if err != nil {
			t.Fatal(err.Error())
		}
		out[filepath.Base(f)] = data
	}
	return out
}

func sameFiles(t *testing.T, what string, a, b map[string][]byte) {
	t.Helper()
	if len(a) != len(b) {
		t.Errorf("%s: %d files and %d", what, len(a), len(b))
	}
	for name, data := range a {
		if !bytes.Equal(data, b[name]) {
			t.Errorf("%s: %s differs", what, name)
		}
	}
}

const fmtProg = `package main

import "pluto/fmt"

type T struct{ p *int }

func (t *T) String() string { return "T" }

func keep(t *T, p *int) { t.p = p }

func main() {
	x := 1
	t := &T{}
	keep(t, &x)
	fmt.Println(t, *t.p)
	for i := range 3 {
		func() { fmt.Println(i) }()
	}
}
`

const rejectedProg = `package main

import "pluto/fmt"

var g *int

func leak(p *int) { g = p }

func main() {
	x := 1
	leak(&x)
	fmt.Println(x)
}
`

// recursiveProg is rejected through an SCC of two functions, so the reasons
// of its diagnostic come from the SCC's well-founded pass (sched/reasons.p).
const recursiveProg = `package main

import "pluto/fmt"

var g *int

func a(p *int, n int) {
	if n == 0 {
		g = p
		return
	}
	b(p, n-1)
}

func b(p *int, n int) {
	if n > 5 {
		return
	}
	a(p, n-1)
}

func main() {
	x := 1
	a(&x, 3)
	fmt.Println(x)
}
`

// TestWarm checks that a second run accepts every SCC from the file and
// gives the diagnostics and summaries of a run without a cache, for a
// program that is accepted and ones that are rejected, and that saving
// again writes the same bytes.
func TestWarm(t *testing.T) {
	for _, src := range []string{fmtProg, rejectedProg, recursiveProg} {
		prog := loadSrc(t, src)
		cold := analyse(t, prog, "")
		// Removed at the end of the iteration: a defer may not run twice
		// in one call (a test that stops leaves it behind).
		dir := testenv.TempDir(t)
		first := analyse(t, prog, dir)
		sameAsCold(t, "first run", cold, first)
		sameSummaries(t, "first run", cold, first)
		files := readDir(t, dir)
		if len(files) == 0 {
			t.Fatal("no files written")
		}
		warm := analyse(t, prog, dir)
		sameAsCold(t, "second run", cold, warm)
		sameSummaries(t, "second run", cold, warm)
		if warm.stats.CacheMissed != 0 || warm.stats.CacheRejected != 0 {
			// Functions without a stable ID (and so their SCCs) are
			// always solved; there are none in these programs.
			t.Errorf("second run: %d SCCs missed, %d rejected; want all accepted (%+v)", warm.stats.CacheMissed, warm.stats.CacheRejected, warm.stats)
		}
		if cold.diags != "" && warm.stats.CacheRedone == 0 {
			t.Errorf("second run of a rejected program: nothing redone before reporting")
		}
		sameFiles(t, "files after the second run", files, readDir(t, dir))
		testenv.Remove(dir)
	}
}

// TestShared checks that programs sharing a directory keep each other's
// entries: the library's summaries depend on the program (the candidate
// sets of its dynamic calls), so a file holds one variant per program.
func TestShared(t *testing.T) {
	a, b := loadSrc(t, fmtProg), loadSrc(t, chainProg)
	coldA, coldB := analyse(t, a, ""), analyse(t, b, "")
	dir := testenv.TempDir(t)
	defer testenv.Remove(dir)
	analyse(t, a, dir)
	analyse(t, b, dir)
	for _, tc := range []struct {
		name string
		prog *load.Program
		cold outcome
	}{{"first", a, coldA}, {"second", b, coldB}} {
		got := analyse(t, tc.prog, dir)
		sameAsCold(t, tc.name, tc.cold, got)
		sameSummaries(t, tc.name, tc.cold, got)
		if got.stats.CacheMissed != 0 || got.stats.CacheRejected != 0 {
			t.Errorf("%s program: %d SCCs missed, %d rejected; want all accepted", tc.name, got.stats.CacheMissed, got.stats.CacheRejected)
		}
	}
}

// TestDeterministic checks that two runs into empty directories write
// byte-identical files.
func TestDeterministic(t *testing.T) {
	prog := loadSrc(t, fmtProg)
	a, b := testenv.TempDir(t), testenv.TempDir(t)
	defer testenv.Remove(a)
	defer testenv.Remove(b)
	analyse(t, prog, a)
	// A program loaded again: new types, new positions, new EIR.
	analyse(t, loadSrc(t, fmtProg), b)
	fa, fb := readDir(t, a), readDir(t, b)
	if len(fa) == 0 {
		t.Fatal("no files")
	}
	sameFiles(t, "two cold runs", fa, fb)
}

// TestRoundTrip checks that every summary of a program decodes, into an
// interner of its own, to a summary that encodes to the same bytes.
func TestRoundTrip(t *testing.T) {
	prog := loadSrc(t, fmtProg)
	res, err := CheckWith(prog, sched.Options{})
	if res == nil {
		t.Fatalf("%v", err)
	}
	p, ps := res.Env.Program(), res.Env.Paths()
	other := domain.NewPaths()
	n, deep, filtered := 0, 0, 0
	for id, s := range res.Summaries {
		b := cache.EncodeSummary(ps, s)
		f := p.Funcs[id]
		d, err := cache.DecodeSummary(b, other, len(f.Inputs), f.Results)
		if err != nil {
			t.Fatalf("%s: %v", f.Name, err)
		}
		if b2 := cache.EncodeSummary(other, d); !bytes.Equal(b, b2) {
			t.Errorf("%s: round trip changed the encoding", f.Name)
		}
		if s != nil && len(s.Esc)+len(s.Store)+len(s.Res) > 0 {
			n++
		}
		if s != nil {
			for _, k := range compat.Append(compat.Append([]domain.Key(nil), s.Esc...), s.HeapStore...) {
				if k.Deep() {
					deep++
				}
			}
			for _, k := range s.Esc {
				if k.HasFilter() {
					filtered++
				}
			}
		}
	}
	if filtered == 0 {
		t.Fatal("no filtered key (pluto/fmt's walk makes them, D51): the test does not cover them")
	}
	if n == 0 {
		t.Fatal("no summary with entries: the test tests nothing")
	}
	if deep == 0 {
		t.Fatal("no Deep key (pluto/fmt's walk makes them): the test does not cover them")
	}
}

const chainProg = `package main

import "pluto/fmt"

func leaf(p *int) *int {
	return p
}

func mid(p *int) *int { return leaf(p) }

func top() int {
	x := 1
	return *mid(&x)
}

func other(s string) { fmt.Println(s) }

func main() {
	fmt.Println(top())
	other("hi")
}
`

// TestInvalidation checks that after an edit of one function only its SCC
// and the SCCs that depend on it are solved again: everything else, the
// library included, comes from the file. An edit that leaves the
// function's summary as it was stops there (early cutoff).
func TestInvalidation(t *testing.T) {
	for _, tc := range []struct {
		name, edit string
		maySolve   []string // functions whose SCCs may be solved again
	}{
		{"same summary", "if p == nil {\n\t\treturn nil\n\t}\n\treturn p", []string{"main.leaf"}},
		{"new summary", "_ = p\n\treturn nil", []string{"main.leaf", "main.mid", "main.top", "main.main"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := testenv.TempDir(t)
			defer testenv.Remove(dir)
			analyse(t, loadSrc(t, chainProg), dir)
			prog := loadSrc(t, strings.Replace(chainProg, "return p\n}\n\nfunc mid", tc.edit+"\n}\n\nfunc mid", 1))
			cold := analyse(t, prog, "")
			warm := analyse(t, prog, dir)
			sameAsCold(t, "after the edit", cold, warm)
			sameSummaries(t, "after the edit", cold, warm)
			sawLeaf := false
			for names, oc := range warm.sccs {
				if oc == sched.CacheAccepted {
					continue
				}
				if names == "main.leaf" {
					sawLeaf = true
				}
				if !slices.Contains(tc.maySolve, names) {
					t.Errorf("SCC %s: %v, want accepted from the file", names, oc)
				}
			}
			if !sawLeaf {
				t.Errorf("the edited function's SCC was not solved again")
			}
		})
	}
}
