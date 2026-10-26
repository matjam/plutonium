// Command escapedigest compiles the repository's test corpus and prints, for
// every program and optimisation level, a digest of what the compiler
// produced: the diagnostics when the build fails, the bytes of the object
// file when it succeeds. Two compilers that print the same digests agree on
// every verdict and every byte on the corpus. It is how a change to the
// escape analysis (internal/escape), whose results must not change when it
// is made faster, is checked against the compiler it started from:
//
//	git worktree add /tmp/base origin/main
//	(cd /tmp/base && go run ./tools/escapedigest > /tmp/before.txt)
//	go run ./tools/escapedigest -compare /tmp/before.txt
//
// The corpus is the examples, every program of testdata/run, lib, plutonium
// and trace, the kernel and C ABI programs, and every program of
// tests/diag (most of which are rejected, so their diagnostics
// are digested). Each is built at -O0 and -O2.
package main

import (
	"bytes"
	"crypto/sha256"
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

// digest builds p at the given level and returns a one-line summary.
func digest(root string, p program, level int) string {
	dir := p.dir
	if p.file != "" {
		tmp, err := os.MkdirTemp("", "escapedigest")
		if err != nil {
			return "error " + err.Error()
		}
		defer func() { _ = os.RemoveAll(tmp) }()
		src, err := os.ReadFile(p.file)
		if err != nil {
			return "error " + err.Error()
		}
		if err := os.WriteFile(filepath.Join(tmp, "main.p"), src, 0o644); err != nil {
			return "error " + err.Error()
		}
		dir = tmp
	}
	cfg := p.cfg
	cfg.Dir, cfg.Stdlib, cfg.OptLevel = dir, plutonium.Stdlib(), level
	l, err := build.Frontend(&cfg)
	if err == nil {
		var obj []byte
		if obj, err = l.Object(&cfg); err == nil {
			return fmt.Sprintf("object %d %x", len(obj), sha256.Sum256(obj))
		}
	}
	msg := strings.ReplaceAll(err.Error(), dir, "DIR")
	msg = strings.ReplaceAll(msg, root, "ROOT")
	return fmt.Sprintf("error %x", sha256.Sum256([]byte(msg)))
}

func main() {
	root := flag.String("root", ".", "repository root")
	compare := flag.String("compare", "", "digests file to compare with; exit status 1 if any line differs")
	only := flag.String("only", "", "only programs whose names contain this")
	jobs := flag.Int("j", max(1, runtime.NumCPU()/3), "programs built at once")
	flag.Parse()
	abs, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	var progs []program
	for _, p := range corpus(abs) {
		if strings.Contains(p.name, *only) {
			progs = append(progs, p)
		}
	}
	lines := make([]string, len(progs)*2)
	var wg sync.WaitGroup
	sem := make(chan struct{}, *jobs)
	for i, p := range progs {
		for level := 0; level <= 2; level += 2 {
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				lines[i*2+level/2] = fmt.Sprintf("%s -O%d %s", p.name, level, digest(abs, p, level))
			}()
		}
	}
	wg.Wait()
	sort.Strings(lines)
	var buf bytes.Buffer
	for _, l := range lines {
		fmt.Fprintln(&buf, l)
	}
	if *compare == "" {
		if _, err := os.Stdout.Write(buf.Bytes()); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		return
	}
	want, err := os.ReadFile(*compare)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	wantLines := map[string]bool{}
	for _, l := range strings.Split(strings.TrimSpace(string(want)), "\n") {
		wantLines[l] = true
	}
	differ := 0
	for _, l := range lines {
		if !wantLines[l] {
			fmt.Println("differs:", l)
			differ++
		}
	}
	fmt.Printf("%d programs and levels compared, %d differ\n", len(lines), differ)
	if differ > 0 || len(wantLines) != len(lines) {
		os.Exit(1)
	}
}
