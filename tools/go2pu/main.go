// Command go2pu translates Go packages of the Plutonium compiler into
// Plutonium source, as the first step of porting the compiler to its own
// language (docs/plans/2026-10-09-self-hosting-plan.md).
//
// It rewrites the Go constructs Plutonium lacks into calls on the selfhost
// compat package, which gives them Go's semantics on top of an arena:
//
//   - map[K]V becomes *hash.Map[K, V]; indexing, assignment, delete, len,
//     range and literals become method or compat calls;
//   - append becomes compat.Append, which grows from alloc.Default();
//   - s + t on strings, string(b) and []byte(s) become compat calls that copy;
//   - fmt.Sprintf and fmt.Errorf become compat.Sprintf and compat.Errorf;
//   - bytes.Buffer and strings.Builder become compat.Buffer;
//   - sort.Slice with a comparison of x[i] and x[j] becomes sort.SortFunc;
//   - encoding/binary.Write of a fixed-size struct becomes a generated
//     field-by-field encoder.
//
// Constructs it cannot translate (closures and interface values that escape
// their frame, recover, reflection, goroutines, maps modified while ranged
// over) are marked with a "go2pu:" comment and listed on standard error, as
// are string literals that are not valid UTF-8: binary data kept in a
// string, which Plutonium strings cannot hold (language spec section 6.10).
//
// Usage:
//
//	go2pu -out selfhost -mod selfhost [-map from=to]... dir...
//
// Each dir is a Go package directory inside the compiler's module; its
// translation is written under -out at the package's import path with the
// module prefix replaced by -mod.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

// mapFlag collects -map from=to import path replacements.
type mapFlag map[string]string

func (m mapFlag) String() string { return fmt.Sprint(map[string]string(m)) }

func (m mapFlag) Set(s string) error {
	from, to, ok := strings.Cut(s, "=")
	if !ok {
		return fmt.Errorf("-map %q: want from=to", s)
	}
	m[from] = to
	return nil
}

func main() {
	cfg := Config{Imports: mapFlag{}}
	flag.StringVar(&cfg.OutDir, "out", "selfhost", "output root directory")
	flag.StringVar(&cfg.Module, "mod", "selfhost", "Plutonium module path of the output")
	flag.StringVar(&cfg.PlutoRoot, "plutoroot", "lib", "standard library root, to check which names exist")
	flag.Var(mapFlag(cfg.Imports), "map", "replace an import path (from=to); repeatable")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: go2pu [-out dir] [-mod path] [-map from=to] dir...")
		os.Exit(2)
	}
	failed := false
	for _, dir := range flag.Args() {
		res, err := TranslateDir(&cfg, dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			failed = true
			continue
		}
		if err := res.Write(cfg.OutDir); err != nil {
			fmt.Fprintln(os.Stderr, err)
			failed = true
		}
		for _, n := range res.Notes {
			fmt.Fprintln(os.Stderr, n)
		}
	}
	if failed {
		os.Exit(1)
	}
}
