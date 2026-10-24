// Command escape2diff runs the escape analysis of internal/escape and the one
// being rebuilt in internal/escape2 on every program of the repository's
// test corpus and the escape review suites, and prints each program where
// they differ: accept against reject, and the error sites one reports and
// the other does not (docs/plans/2026-10-20-escape-redesign.md, section
// (g)). It is PLUTO_ESCAPE=both without lowering or linking, so it is cheap
// enough to run over everything.
//
//	go run ./tools/escape2diff [-stats] [extra program dirs or .p files...]
//
// The corpus is that of tools/escapedigest (examples, testdata/run, lib,
// plutonium and trace, the kernel and C ABI programs, cmd/pluto/testdata/diag)
// plus internal/escape/testdata/review*. With -stats it also prints, for
// each program the new analysis runs on, its SCC measurements: functions,
// SCCs, the largest SCC and the peak arena of one SCC.
//
// With -moves it compares the move checker instead (internal/moves, plan
// stage 4): given internal/escape's facts about calls and given
// internal/escape2's (escape2.CheckCalls, what PLUTO_ESCAPE=new gives it),
// and prints each program whose move errors differ.
//
// It exits with status 1 when any program's verdicts differ.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/escape"
	"github.com/matjam/plutonium/internal/escape2"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/sched"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/moves"
	"github.com/matjam/plutonium/internal/types2"
)

type program struct {
	name string
	dir  string // a directory with the program, or
	file string // a single .p file, analysed as main.p
	free bool   // freestanding
	kern bool   // kernel
}

func corpus(root string) []program {
	var out []program
	glob := func(pattern string) []string {
		m, _ := filepath.Glob(filepath.Join(root, pattern))
		sort.Strings(m)
		return m
	}
	for _, f := range glob("examples/*/main.p") {
		out = append(out, program{name: "examples/" + filepath.Base(filepath.Dir(f)), dir: filepath.Dir(f)})
	}
	for _, k := range []program{{name: "sched", kern: true}, {name: "smoke", kern: true}, {name: "gsuser", kern: true}, {name: "irq", free: true}} {
		k.dir = filepath.Join(root, "testdata", "kernel", k.name)
		k.name = "kernel/" + k.name
		out = append(out, k)
	}
	out = append(out, program{name: "cabi", dir: filepath.Join(root, "testdata", "cabi")})
	for _, d := range []string{"run", "lib", "plutonium", "trace"} {
		for _, f := range glob(filepath.Join("testdata", d, "*.p")) {
			out = append(out, program{name: d + "/" + strings.TrimSuffix(filepath.Base(f), ".p"), file: f})
		}
	}
	for _, f := range glob("cmd/pluto/testdata/diag/*/main.p") {
		out = append(out, program{name: "diag/" + filepath.Base(filepath.Dir(f)), dir: filepath.Dir(f)})
	}
	for _, f := range glob("internal/escape/testdata/review*/*.p") {
		rel, _ := filepath.Rel(filepath.Join(root, "internal/escape/testdata"), f)
		out = append(out, program{name: strings.TrimSuffix(rel, ".p"), file: f})
	}
	for _, f := range glob("internal/escape/testdata/review*/*/main.p") {
		rel, _ := filepath.Rel(filepath.Join(root, "internal/escape/testdata"), filepath.Dir(f))
		out = append(out, program{name: rel, dir: filepath.Dir(f)})
	}
	return out
}

// outcome is what the comparison found for one program.
type outcome struct {
	name  string
	err   string // the program does not load
	diff  *escape2.Disagreement
	stats *sched.Stats
	sccs  []sched.SCCStats
	// moves: the move errors only one of the two sets of facts gives
	onlyOld, onlyNew []string
}

func run(p program, wantStats, wantMoves bool) (o outcome) {
	o.name = p.name
	dir := p.dir
	if p.file != "" {
		tmp, err := os.MkdirTemp("", "escape2diff")
		if err != nil {
			o.err = err.Error()
			return o
		}
		defer func() { _ = os.RemoveAll(tmp) }()
		src, err := os.ReadFile(p.file)
		if err != nil {
			o.err = err.Error()
			return o
		}
		if err := os.WriteFile(filepath.Join(tmp, "main.p"), src, 0o644); err != nil {
			o.err = err.Error()
			return o
		}
		dir = tmp
	}
	cfg := &load.Config{Tags: load.BuildTags(p.free, p.kern, nil), Stdlib: plutonium.Stdlib(), Sizes: types2.SizesFor("gc", "amd64")}
	prog, err := load.Load(cfg, dir)
	if err != nil {
		o.err = firstLine(err.Error())
		return o
	}
	if wantMoves {
		oldCalls, _ := escape.Analyze(prog)
		newCalls, _ := escape2.CheckCalls(prog)
		_, oldErr := moves.AnalyzeWith(prog, oldCalls)
		_, newErr := moves.AnalyzeWith(prog, newCalls)
		o.onlyOld, o.onlyNew = lineDiff(errLines(oldErr), errLines(newErr))
		return o
	}
	oldErr := escape.Check(prog)
	opts := sched.Options{}
	if wantStats {
		opts.OnSCC = func(_ []eir.FuncID, st sched.SCCStats) { o.sccs = append(o.sccs, st) }
	}
	res, newErr := escape2.CheckWith(prog, opts)
	if d := escape2.Compare(prog, oldErr, newErr); d != nil {
		d.Program = p.name
		o.diff = d
	}
	if res != nil && wantStats {
		o.stats = &res.Stats
	}
	return o
}

// errLines returns the first lines of err's errors, sorted.
func errLines(err error) []string {
	if err == nil {
		return nil
	}
	var out []string
	for _, l := range strings.Split(err.Error(), "\n") {
		if l != "" && !strings.HasPrefix(l, " ") && !strings.HasPrefix(l, "\t") {
			out = append(out, l)
		}
	}
	sort.Strings(out)
	return out
}

// lineDiff returns the lines only a has and those only b has.
func lineDiff(a, b []string) (onlyA, onlyB []string) {
	count := map[string]int{}
	for _, l := range a {
		count[l]++
	}
	for _, l := range b {
		count[l]--
	}
	for _, l := range a {
		if count[l] > 0 {
			onlyA = append(onlyA, l)
			count[l]--
		}
	}
	for l, n := range count {
		for ; n < 0; n++ {
			onlyB = append(onlyB, l)
		}
	}
	sort.Strings(onlyB)
	return onlyA, onlyB
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

func main() {
	stats := flag.Bool("stats", false, "print the new analysis's SCC measurements per program")
	jobs := flag.Int("j", 4, "programs analysed at once (each loads the standard library)")
	runRE := flag.String("run", "", "analyse only the programs whose names match this regular expression")
	movesMode := flag.Bool("moves", false, "compare the move checker given each analysis's facts about calls")
	flag.Parse()
	match, err := regexp.Compile(*runRE)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	progs := corpus(root)
	for _, a := range flag.Args() {
		if strings.HasSuffix(a, ".p") {
			progs = append(progs, program{name: a, file: a})
			continue
		}
		matches, _ := filepath.Glob(filepath.Join(a, "*", "main.p"))
		if len(matches) == 0 {
			progs = append(progs, program{name: a, dir: a})
		}
		for _, m := range matches {
			progs = append(progs, program{name: filepath.Dir(m), dir: filepath.Dir(m)})
		}
	}
	kept := progs[:0]
	for _, p := range progs {
		if match.MatchString(p.name) {
			kept = append(kept, p)
		}
	}
	progs = kept
	var mu sync.Mutex
	var differ, sites, failed, loadErrs, agree int
	print := func(o outcome) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case o.err != "":
			loadErrs++
			fmt.Printf("escape2diff: %s: does not load: %s\n", o.name, o.err)
		case *movesMode && (len(o.onlyOld) > 0 || len(o.onlyNew) > 0):
			differ++
			for _, l := range o.onlyOld {
				fmt.Printf("moves: %s: only with internal/escape's facts: %s\n", o.name, l)
			}
			for _, l := range o.onlyNew {
				fmt.Printf("moves: %s: only with internal/escape2's facts: %s\n", o.name, l)
			}
		case o.diff == nil:
			agree++
		default:
			if o.diff.VerdictsDiffer() {
				differ++
			} else {
				sites++
			}
			if o.diff.NewFailed != "" {
				failed++
			}
			fmt.Print(o.diff.String())
		}
		if o.stats != nil {
			peak := 0
			for _, s := range o.sccs {
				peak = max(peak, s.ArenaBytes+s.SetBytes)
			}
			fmt.Printf("stats: %s: funcs %d, SCCs %d, largest SCC %d, solves %d, peak SCC arena %d B, summaries %d B\n",
				o.name, o.stats.Funcs, o.stats.SCCs, o.stats.LargestSCC, o.stats.Iterations, peak, o.stats.SummaryBytes)
		}
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, max(1, *jobs))
	for _, p := range progs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			print(run(p, *stats, *movesMode))
		}()
	}
	wg.Wait()

	fmt.Printf("escape2diff: %d programs: %d agree, %d verdicts differ (%d with the new analysis failing), %d agree on the verdict with different error sites, %d do not load\n",
		len(progs), agree, differ, failed, sites, loadErrs)
	if differ > 0 {
		os.Exit(1)
	}
}
