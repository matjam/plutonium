package solve_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/escape2"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/sched"
	"github.com/matjam/plutonium/internal/escape2/solve"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/types2"
)

// TestMeasure measures what the solver uses per function over the corpus
// (examples, testdata/run, lib, plutonium and trace), the review suites and,
// with ESCAPE2_MEASURE=all, the translated compiler (selfhost/demo/diagdemo):
// the most tokens of one input, the arena and the work of the largest solves,
// against solve.MaxFuncArena and solve.MaxFuncSteps (ESCAPE2_MEASURE=diagdemo
// measures the translated compiler alone). It prints the largest
// and fails if any function comes within 10x of a limit. It loads every
// program of the corpus, so it runs only with ESCAPE2_MEASURE set (through
// bigjob.sh for "all").
func TestMeasure(t *testing.T) {
	mode := os.Getenv("ESCAPE2_MEASURE")
	if mode == "" {
		t.Skip("set ESCAPE2_MEASURE=corpus or all")
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	type rec struct {
		prog, fn string
		m        solve.Measurement
	}
	var mu sync.Mutex
	var recs []rec
	cur := ""
	solve.Measure = func(f *eir.Func, m solve.Measurement) {
		mu.Lock()
		recs = append(recs, rec{cur, f.Name, m})
		mu.Unlock()
	}
	defer func() { solve.Measure = nil }()

	var dirs []string
	ex, _ := filepath.Glob(filepath.Join(root, "examples", "*", "main.p"))
	for _, f := range ex {
		dirs = append(dirs, filepath.Dir(f))
	}
	var files []string
	for _, d := range []string{"testdata/run", "testdata/lib", "testdata/plutonium", "testdata/trace", "tests/testdata/escape/review2", "tests/testdata/escape/review3", "tests/testdata/escape/review4"} {
		fs, _ := filepath.Glob(filepath.Join(root, d, "*.p"))
		files = append(files, fs...)
	}
	if mode == "all" {
		dirs = append(dirs, filepath.Join(root, "selfhost", "demo", "diagdemo"))
	}
	if mode == "diagdemo" {
		dirs, files = []string{filepath.Join(root, "selfhost", "demo", "diagdemo")}, nil
	}
	cfg := &load.Config{Tags: load.BuildTags(false, false, nil), Stdlib: plutonium.Stdlib(), Sizes: types2.SizesFor("gc", "amd64")}
	run := func(name string, prog *load.Program) {
		cur = name
		defer func() {
			if r := recover(); r != nil {
				t.Logf("%s: %v", name, r)
			}
		}()
		_, _, err := escape2.Run(prog, sched.Options{})
		if err != nil && strings.Contains(err.Error(), "internal compiler error") {
			t.Logf("%s: %v", name, err)
		}
	}
	for _, d := range dirs {
		dcfg := *cfg
		if strings.HasSuffix(d, "diagdemo") {
			dcfg.Tags = map[string]bool{"amd64": true, "linux": true, "hosted": true}
		}
		prog, err := load.Load(&dcfg, d)
		if err != nil {
			t.Logf("load %s: %v", d, err)
			continue
		}
		run(strings.TrimPrefix(d, root+"/"), prog)
	}
	for _, f := range files {
		dir := t.TempDir()
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "main.p"), src, 0o644); err != nil {
			t.Fatal(err)
		}
		prog, err := load.Load(cfg, dir)
		if err != nil {
			continue // rejected by the type checker
		}
		run(strings.TrimPrefix(f, root+"/"), prog)
	}
	top := func(what string, key func(solve.Measurement) int, limit int) {
		sort.Slice(recs, func(i, j int) bool { return key(recs[i].m) > key(recs[j].m) })
		var b strings.Builder
		fmt.Fprintf(&b, "largest %s (limit %d):\n", what, limit)
		for i := 0; i < 8 && i < len(recs); i++ {
			fmt.Fprintf(&b, "  %d  %s  %s  (tokens %d, max per input %d, arena %d, steps %d)\n", key(recs[i].m), recs[i].prog, recs[i].fn, recs[i].m.Tokens, recs[i].m.MaxInputTokens, recs[i].m.ArenaBytes, recs[i].m.Steps)
		}
		t.Log(b.String())
		if limit > 0 && len(recs) > 0 && key(recs[0].m)*10 > limit {
			t.Errorf("%s: %s %s is within 10x of the limit", what, recs[0].prog, recs[0].fn)
		}
	}
	t.Logf("%d solves", len(recs))
	// No solve may stop at a limit, on any pass of its SCC. The functions
	// of watched once did on passes in the middle of their SCC's iteration
	// (types2.(*Checker).builtin reached 318 MB on its fifth of fifteen
	// passes, with 64 MB at the fixed point), so they must be measured.
	watched := map[string]int{}
	if mode == "all" || mode == "diagdemo" {
		watched["selfhost/internal/types2.(*Checker).builtin"] = 0
		watched["selfhost/internal/types2.(*unifier).nify"] = 0
	}
	for _, r := range recs {
		if r.m.Limited {
			t.Errorf("%s: %s stopped at a limit (arena %d, steps %d)", r.prog, r.fn, r.m.ArenaBytes, r.m.Steps)
		}
		if n, ok := watched[r.fn]; ok {
			watched[r.fn] = n + 1
			t.Logf("%s pass %d: arena %d, steps %d, tokens %d", r.fn, n+1, r.m.ArenaBytes, r.m.Steps, r.m.Tokens)
		}
	}
	for fn, n := range watched {
		if n == 0 {
			t.Errorf("%s was not solved", fn)
		}
	}
	top("arena bytes", func(m solve.Measurement) int { return m.ArenaBytes }, solve.MaxFuncArena)
	top("steps", func(m solve.Measurement) int { return m.Steps }, solve.MaxFuncSteps)
	top("tokens of one input", func(m solve.Measurement) int { return m.MaxInputTokens }, 0)
}
