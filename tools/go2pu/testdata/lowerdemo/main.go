// Command lowerdemo lowers Plutonium programs to SSA with the compiler's own
// front end (internal/load, escape, moves, race and lower) and prints a
// stable text form of the result: the SSA program as internal/ssawrite
// writes it, which carries every value, block, type and symbol of it, after
// lowering (level 0) or after the optimiser's pipeline for the level (2), or
// the errors the checks and lowering report. go2pu translates it with the
// front end, and TestLower (tools/go2pu) runs the Go and the Plutonium build
// over the repository's programs and requires the same text.
//
// Usage: lowerdemo libdir level mode dir...
//
// level is the optimisation level; mode is "full", which prints the text, or
// "digest", which prints its size and a hash. Without arguments it prints
// nothing, which TestDifferential compares like the other demos.
package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/escape"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/lower"
	"github.com/matjam/plutonium/internal/moves"
	"github.com/matjam/plutonium/internal/opt"
	"github.com/matjam/plutonium/internal/race"
	"github.com/matjam/plutonium/internal/scoped"
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/ssawrite"
	"github.com/matjam/plutonium/internal/types2"
)

// The analyses recurse with the nesting of the source and of its types.
//
//pu:stack 1G
func main() {
	if len(os.Args) < 5 {
		return
	}
	level, err := strconv.Atoi(os.Args[2])
	if err != nil {
		fmt.Fprintf(os.Stderr, "lowerdemo: level: %v\n", err)
		os.Exit(2)
	}
	full := os.Args[3] == "full"
	var stdlib fs.FS = os.DirFS(os.Args[1])
	tags := load.BuildTags(false, false, nil)
	tags["gencheck"] = true
	cfg := &load.Config{Tags: tags, Stdlib: stdlib, Sizes: types2.SizesFor("gc", "amd64")}
	for _, dir := range os.Args[4:] {
		var out bytes.Buffer
		lowerDir(&out, cfg, level, full, dir)
		os.Stdout.Write(out.Bytes())
	}
}

// lowerDir loads and lowers the program in dir and dumps it.
func lowerDir(out *bytes.Buffer, cfg *load.Config, level int, full bool, dir string) {
	fmt.Fprintf(out, "dir %s\n", dir)
	prog, err := load.Load(cfg, dir)
	if err != nil {
		fmt.Fprintf(out, "error\n%s\n", strings.TrimRight(err.Error(), "\n"))
		return
	}
	sp, err := lowerProgram(cfg, prog)
	if err != nil {
		fmt.Fprintf(out, "error\n%s\n", strings.TrimRight(err.Error(), "\n"))
		return
	}
	var text bytes.Buffer
	if level > 0 {
		// An optimised program is no longer in the form ssawrite writes:
		// print the functions.
		pipeline := opt.ForLevel(level)
		if err := pipeline.Run(sp, func(pass string, f *ssa.Func) {}); err != nil {
			fmt.Fprintf(out, "error\ninternal compiler error: %s\n", err.Error())
			return
		}
		for _, f := range sp.Funcs {
			text.WriteString(f.String())
		}
	} else if err := ssawrite.Write(&text, sp, prog.Fset); err != nil {
		fmt.Fprintf(out, "error\nwrite: %s\n", err.Error())
		return
	}
	if full {
		out.Write(text.Bytes())
		return
	}
	h := uint64(14695981039346656037)
	for _, b := range text.Bytes() {
		h ^= uint64(b)
		h *= 1099511628211
	}
	fmt.Fprintf(out, "ssa %d bytes %d funcs %016x\n", text.Len(), len(sp.Funcs), h)
}

// lowerProgram runs the checks and lowering as the driver does
// (internal/build, lowerProgram).
func lowerProgram(cfg *load.Config, prog *load.Program) (*ssa.Program, error) {
	calls, escErr := escape.Analyze(prog)
	info, moveErr := moves.AnalyzeWith(prog, calls)
	var overlaps scoped.Overlaps
	var raceErr error
	lifetimeErr := joinLifetimeErrors(moveErr, escErr)
	if lifetimeErr == nil {
		overlaps, raceErr = race.AnalyzeWith(prog, info)
	}
	sp, err := lower.Lower(&lower.Config{
		Overlaps: overlaps,
		Sizes:    cfg.Sizes,
		GenCheck: true,
		Moves:    info,
	}, prog)
	if err != nil {
		return nil, err
	}
	if lifetimeErr != nil {
		return nil, lifetimeErr
	}
	if raceErr != nil {
		return nil, raceErr
	}
	return sp, nil
}

// joinLifetimeErrors returns the errors of the move checker and the escape
// analysis as one list.
func joinLifetimeErrors(errs ...error) error {
	var all diag.List
	for _, err := range errs {
		if l, ok := err.(*diag.List); ok {
			all = append(all, *l...)
		} else if err != nil {
			return err
		}
	}
	return all.Err()
}
