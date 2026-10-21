package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
	"github.com/matjam/plutonium/internal/escape"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/lower"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/types2"
)

// keepers measures, for decision D47, the functions reachable through a
// dynamic call that keep a pointer parameter, over the programs given, and
// prints them deduplicated, with counts by where they are (the standard
// library or not) and by how they keep it.
func keepers(progs []program, jobs int) {
	type key struct {
		pos, fn, param, how string
		own                 bool
	}
	var mu sync.Mutex
	all := map[key]escape.Keeper{}
	candFuncs := 0
	var wg sync.WaitGroup
	sem := make(chan struct{}, jobs)
	for _, p := range progs {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			ks, n, err := keepersOf(p)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", p.name, firstLine(err.Error()))
			}
			candFuncs = max(candFuncs, n)
			for _, k := range ks {
				all[key{k.Pos.String(), k.Func, k.Param, k.How, k.Own}] = k
			}
		}()
	}
	wg.Wait()
	var lines []string
	funcs := map[string]map[string]bool{} // by place, the functions
	hows := map[string]int{}
	for _, k := range all {
		place := "user"
		if strings.HasPrefix(k.Pos.Filename, load.StdPrefix) {
			place = "lib/pluto"
		}
		if funcs[place] == nil {
			funcs[place] = map[string]bool{}
		}
		funcs[place][k.Pos.String()+" "+k.Func] = true
		own := "loaded through it"
		if k.Own {
			own = "its own pointer"
		}
		hows[fmt.Sprintf("%s: %s, %s", place, k.How, own)]++
		lines = append(lines, fmt.Sprintf("%s: %s (%s) keeps %s (%s): %s", k.Pos, k.Func, k.Via, k.Param, own, k.How))
	}
	sort.Strings(lines)
	for _, l := range lines {
		fmt.Println(l)
	}
	var hs []string
	for h, n := range hows {
		hs = append(hs, fmt.Sprintf("  %4d %s", n, h))
	}
	sort.Strings(hs)
	fmt.Println(strings.Join(hs, "\n"))
	for place, fs := range funcs {
		fmt.Printf("%s: %d functions keep a parameter (largest per-program candidate set: %d functions)\n", place, len(fs), candFuncs)
	}
}

func keepersOf(p program) ([]escape.Keeper, int, error) {
	dir := p.dir
	if p.file != "" {
		tmp, err := os.MkdirTemp("", "escapeoracle")
		if err != nil {
			return nil, 0, err
		}
		defer func() { _ = os.RemoveAll(tmp) }()
		src, err := os.ReadFile(p.file)
		if err != nil {
			return nil, 0, err
		}
		if err := os.WriteFile(filepath.Join(tmp, "main.p"), src, 0o644); err != nil {
			return nil, 0, err
		}
		dir = tmp
	}
	cfg := p.cfg
	cfg.Dir, cfg.Stdlib = dir, plutonium.Stdlib()
	prog, err := build.LoadProgram(&cfg)
	if err != nil {
		return nil, 0, err
	}
	// The functions the generated code may reach through a call through an
	// interface or a func value.
	cf, _ := lower.LowerFacts(&lower.Config{Sizes: types2.SizesFor("gc", "amd64")}, prog)
	if cf == nil {
		return nil, 0, fmt.Errorf("lowering produced nothing")
	}
	objs, lits := map[*types2.Func]bool{}, map[*ast.FuncLit]bool{}
	for _, f := range cf.Funcs() {
		for _, c := range cf.Calls(f) {
			if c.Kind != lower.CallIface && c.Kind != lower.CallFuncVal {
				continue
			}
			for _, t := range c.Targets {
				for _, s := range cf.SourceTargets(t) {
					o := cf.Origin(s)
					if o.Obj != nil {
						objs[o.Obj] = true
					}
					if o.Lit != nil {
						lits[o.Lit] = true
					}
				}
			}
		}
	}
	reached := func(c escape.Callee) bool { return c.Obj != nil && objs[c.Obj] || c.Lit != nil && lits[c.Lit] }
	// The analysis's own errors are the programs' (the review programs
	// are rejected on purpose); the summaries are what is measured.
	ks, cands, _ := escape.DynamicKeepers(prog, reached)
	return ks, cands, nil
}
