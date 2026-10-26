// Command escapeoracle is the lowering oracle of the escape analysis (plan
// docs/plans/2026-10-20-escape-redesign.md, section 3.1, stage 0): it compiles
// programs as far as lowering, before any optimisation, and checks that every
// call the generated code makes to a source function, at a source position,
// is a call whose summary the escape analysis (internal/escape) applied at that
// position. A call the analysis did not apply is a likely hole: the analysis
// judged the program without what that call does with its pointers.
//
//	go run ./tools/escapeoracle                  # the escapedigest corpus
//	go run ./tools/escapeoracle -dirs d1,d2      # every program under d1 and d2
//	go run ./tools/escapeoracle -translated      # selfhost/demo/diagdemo
//
// A program is a directory with a main.p, or a single .p file. The output
// lists each disagreement, by kind (see Kind), then counts.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
)

type program struct {
	name string
	dir  string // a directory with the program, or
	file string // a single .p file, built as main.p
	cfg  build.Config
}

func main() {
	root := flag.String("root", ".", "repository root")
	dirs := flag.String("dirs", "", "comma-separated directories of programs to check instead of the corpus")
	translated := flag.Bool("translated", false, "check the translated compiler (selfhost/demo/diagdemo) instead of the corpus")
	only := flag.String("only", "", "only programs whose names contain this")
	jobs := flag.Int("j", max(1, runtime.NumCPU()/4), "programs checked at once")
	verbose := flag.Bool("v", false, "print the notes (check and lowering errors) of each program")
	keep := flag.Bool("keepers", false, "instead of the oracle, list the functions reachable through dynamic calls that keep a pointer parameter (decision D47)")
	flag.Parse()
	abs, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	var progs []program
	switch {
	case *translated:
		progs = []program{{name: "translated/diagdemo", dir: filepath.Join(abs, "selfhost", "demo", "diagdemo")}}
	case *dirs != "":
		for _, d := range strings.Split(*dirs, ",") {
			progs = append(progs, programsIn(d)...)
		}
	default:
		progs = corpus(abs)
	}
	var sel []program
	for _, p := range progs {
		if strings.Contains(p.name, *only) {
			sel = append(sel, p)
		}
	}
	if *keep {
		keepers(sel, *jobs)
		return
	}
	reports := make([]*Report, len(sel))
	var wg sync.WaitGroup
	sem := make(chan struct{}, *jobs)
	for i, p := range sel {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			reports[i] = check(p)
		}()
	}
	wg.Wait()
	counts := map[Kind]int{}
	calls, targets := 0, 0
	for _, r := range reports {
		calls += r.Calls
		targets += r.Targets
		if *verbose {
			for _, n := range r.Notes {
				fmt.Printf("note: %s: %s\n", r.Program, n)
			}
		}
		for _, f := range r.Findings {
			counts[f.Kind]++
			fmt.Printf("%s: %s\n", r.Program, f)
		}
	}
	var kinds []string
	for k, n := range counts {
		kinds = append(kinds, fmt.Sprintf("%s %d", k, n))
	}
	sort.Strings(kinds)
	fmt.Printf("%d programs, %d calls, %d call targets compared; %s\n", len(reports), calls, targets, strings.Join(kinds, ", "))
}

// check runs the oracle on one program.
func check(p program) *Report {
	dir := p.dir
	if p.file != "" {
		tmp, err := os.MkdirTemp("", "escapeoracle")
		if err != nil {
			return &Report{Program: p.name, Notes: []string{err.Error()}}
		}
		defer func() { _ = os.RemoveAll(tmp) }()
		src, err := os.ReadFile(p.file)
		if err != nil {
			return &Report{Program: p.name, Notes: []string{err.Error()}}
		}
		if err := os.WriteFile(filepath.Join(tmp, "main.p"), src, 0o644); err != nil {
			return &Report{Program: p.name, Notes: []string{err.Error()}}
		}
		dir = tmp
	}
	cfg := p.cfg
	cfg.Dir, cfg.Stdlib = dir, plutonium.Stdlib()
	in, err := build.Oracle(&cfg)
	if err != nil {
		return &Report{Program: p.name, Notes: []string{"load: " + firstLine(err.Error())}}
	}
	return compare(p.name, in)
}

// programsIn lists the programs under dir: each subdirectory with a main.p,
// and each .p file directly in it.
func programsIn(dir string) []program {
	var out []program
	mains, _ := filepath.Glob(filepath.Join(dir, "*", "main.p"))
	for _, m := range mains {
		out = append(out, program{name: filepath.Base(dir) + "/" + filepath.Base(filepath.Dir(m)), dir: filepath.Dir(m)})
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.p"))
	for _, f := range files {
		if filepath.Base(f) == "main.p" {
			out = append(out, program{name: filepath.Base(dir), dir: dir})
			continue
		}
		out = append(out, program{name: filepath.Base(dir) + "/" + strings.TrimSuffix(filepath.Base(f), ".p"), file: f})
	}
	return out
}

// corpus is the corpus of tools/escapedigest.
func corpus(root string) []program {
	var out []program
	examples, _ := filepath.Glob(filepath.Join(root, "examples", "*", "main.p"))
	for _, f := range examples {
		out = append(out, program{name: "examples/" + filepath.Base(filepath.Dir(f)), dir: filepath.Dir(f)})
	}
	for _, k := range []struct {
		name string
		cfg  build.Config
	}{
		{"sched", build.Config{Kernel: true}}, {"smoke", build.Config{Kernel: true}},
		{"gsuser", build.Config{Kernel: true}}, {"irq", build.Config{Freestanding: true}},
	} {
		out = append(out, program{name: "kernel/" + k.name, dir: filepath.Join(root, "testdata", "kernel", k.name), cfg: k.cfg})
	}
	out = append(out, program{name: "cabi", dir: filepath.Join(root, "testdata", "cabi")})
	for _, d := range []string{"run", "lib", "plutonium", "trace"} {
		files, _ := filepath.Glob(filepath.Join(root, "testdata", d, "*.p"))
		for _, f := range files {
			out = append(out, program{name: d + "/" + strings.TrimSuffix(filepath.Base(f), ".p"), file: f})
		}
	}
	diags, _ := filepath.Glob(filepath.Join(root, "tests", "diag", "*", "main.p"))
	for _, f := range diags {
		out = append(out, program{name: "diag/" + filepath.Base(filepath.Dir(f)), dir: filepath.Dir(f)})
	}
	return out
}
