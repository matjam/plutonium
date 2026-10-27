// Command escapedigest compiles the repository's test corpus with a pluto
// binary and prints, for every program and optimization level, a digest of
// what the compiler produced: the exit status and the diagnostics when the
// build fails, the bytes of the object file (pluto build -c) when it
// succeeds. Two compilers that print the same digests agree on every
// verdict and every byte on the corpus. It is how a change that must not
// change the compiler's output, such as making the escape analysis faster,
// is checked against the compiler it started from:
//
//	go run ./tools/escapedigest -pluto /path/to/old/pluto > /tmp/before.txt
//	go run ./tools/escapedigest -compare /tmp/before.txt
//
// The compiler is -pluto, else $PLUTO_TEST_COMPILER, else ./pluto at the
// repository root. Both runs use the standard library of the repository
// (-lib), so a change to the library shows as a change of output.
//
// The corpus is the examples, every program of testdata/run, lib,
// plutonium and trace, the kernel and C ABI programs, and every case of
// the corpus groups under tests/ (diag, race, types2, escape, moves and
// lower; many are rejected, so their diagnostics are digested) that pluto
// build compiles. Each is built at -O0 and -O2.
package main

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// A program is one program of the corpus and how it is built.
type program struct {
	name  string
	dir   string   // a directory with the program, or
	file  string   // a single .p file, built as main.p
	flags []string // extra build flags
	env   []string // KEY=VALUE pairs added to the environment
}

// groups are the corpus directories under tests/ (tests/corpus_test.go).
var groups = []string{"diag", "race", "types2", "escape", "moves", "lower"}

// corpus lists the programs of the repository at root.
func corpus(root string) []program {
	var out []program
	examples, _ := filepath.Glob(filepath.Join(root, "examples", "*", "main.p"))
	for _, f := range examples {
		out = append(out, program{name: "examples/" + filepath.Base(filepath.Dir(f)), dir: filepath.Dir(f)})
	}
	for _, k := range []struct {
		name  string
		flags []string
	}{
		{"sched", []string{"--mode", "kernel"}}, {"smoke", []string{"--mode", "kernel"}},
		{"gsuser", []string{"--mode", "kernel"}}, {"irq", []string{"--mode", "freestanding", "--ldscript", "kernel.ld"}},
	} {
		out = append(out, program{name: "kernel/" + k.name, dir: filepath.Join(root, "testdata", "kernel", k.name), flags: k.flags})
	}
	out = append(out, program{name: "cabi", dir: filepath.Join(root, "testdata", "cabi")})
	for _, d := range []string{"run", "lib", "plutonium", "trace"} {
		files, _ := filepath.Glob(filepath.Join(root, "testdata", d, "*.p"))
		for _, f := range files {
			out = append(out, program{name: d + "/" + strings.TrimSuffix(filepath.Base(f), ".p"), file: f})
		}
	}
	for _, g := range groups {
		cases, _ := filepath.Glob(filepath.Join(root, "tests", g, "*"))
		for _, d := range cases {
			if st, err := os.Stat(d); err != nil || !st.IsDir() {
				continue
			}
			if _, err := os.Stat(filepath.Join(d, "command")); err == nil {
				continue // another subcommand, such as pluto test
			}
			p := program{name: g + "/" + filepath.Base(d), dir: d}
			if flags, err := os.ReadFile(filepath.Join(d, "flags")); err == nil {
				p.flags = strings.Fields(string(flags))
			}
			if env, err := os.ReadFile(filepath.Join(d, "env")); err == nil {
				p.env = strings.Fields(string(env))
			}
			out = append(out, p)
		}
	}
	return out
}

// workDir matches the name of a build's temporary directory.
var workDir = regexp.MustCompile(`pluto-build-[0-9]+`)

// digest builds p with compiler at the given level and returns a one-line
// summary.
func digest(compiler, lib, root string, p program, level int) string {
	tmp, err := os.MkdirTemp("", "escapedigest")
	if err != nil {
		return "error " + err.Error()
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	dir := p.dir
	if p.file != "" {
		src, err := os.ReadFile(p.file)
		if err != nil {
			return "error " + err.Error()
		}
		dir = filepath.Join(tmp, "src")
		if err := os.Mkdir(dir, 0o755); err != nil {
			return "error " + err.Error()
		}
		if err := os.WriteFile(filepath.Join(dir, "main.p"), src, 0o644); err != nil {
			return "error " + err.Error()
		}
	}
	obj := filepath.Join(tmp, "out.o")
	args := append([]string{"build", "-c", fmt.Sprintf("-O%d", level)}, p.flags...)
	args = append(args, "-o", obj, ".")
	cmd := exec.Command(compiler, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PLUTOROOT="+lib, "PLUTO_BRIEF=")
	cmd.Env = append(cmd.Env, p.env...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	status := 0
	var ee *exec.ExitError
	switch err := cmd.Run(); {
	case errors.As(err, &ee):
		status = ee.ExitCode()
	case err != nil:
		return "error " + err.Error()
	}
	// Messages name the program's directory, the repository and the
	// build's temporary directory, which differ from run to run.
	msg := strings.ReplaceAll(out.String(), dir, "DIR")
	msg = strings.ReplaceAll(msg, root, "ROOT")
	msg = workDir.ReplaceAllString(msg, "pluto-build-N")
	text := fmt.Sprintf("output %x", sha256.Sum256([]byte(msg)))
	if msg == "" {
		text = "no output"
	}
	if status != 0 {
		return fmt.Sprintf("status %d %s", status, text)
	}
	data, err := os.ReadFile(obj)
	if err != nil {
		return "error " + err.Error()
	}
	return fmt.Sprintf("object %d %x %s", len(data), sha256.Sum256(data), text)
}

// defaultCompiler is $PLUTO_TEST_COMPILER, or else pluto at root.
func defaultCompiler(root string) string {
	if c := os.Getenv("PLUTO_TEST_COMPILER"); c != "" {
		return c
	}
	return filepath.Join(root, "pluto")
}

func main() {
	root := flag.String("root", ".", "repository root")
	pluto := flag.String("pluto", "", "the compiler (default $PLUTO_TEST_COMPILER, else pluto at the root)")
	lib := flag.String("lib", "", "the standard library, as PLUTOROOT (default lib at the root)")
	compare := flag.String("compare", "", "digests file to compare with; exit status 1 if any line differs")
	only := flag.String("only", "", "only programs whose names contain this")
	jobs := flag.Int("j", max(1, runtime.NumCPU()/2), "builds at once")
	flag.Parse()
	abs, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if *pluto == "" {
		*pluto = defaultCompiler(abs)
	}
	if *lib == "" {
		*lib = filepath.Join(abs, "lib")
	}
	compiler, err := filepath.Abs(*pluto)
	if err == nil {
		_, err = os.Stat(compiler)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "escapedigest: no compiler:", err)
		os.Exit(2)
	}
	libDir, err := filepath.Abs(*lib)
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
	sem := make(chan struct{}, max(*jobs, 1))
	for i, p := range progs {
		for level := 0; level <= 2; level += 2 {
			sem <- struct{}{}
			wg.Go(func() {
				defer func() { <-sem }()
				lines[i*2+level/2] = fmt.Sprintf("%s -O%d %s", p.name, level, digest(compiler, libDir, abs, p, level))
			})
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
