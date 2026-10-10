package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/matjam/plutonium/internal/build"
)

var testCmd = &cobra.Command{
	Use:   "test [-run regexp] [-bench regexp] [-v] [-count n] [-O level] [packages]",
	Short: "Run the tests of Plutonium packages",
	Long: `test builds a test binary for each package from the package's files and
its _test.p files, runs it, and reports the results in the format of go test.

Tests are functions func TestXxx(t *testing.T) and benchmarks are functions
func BenchmarkXxx(b *testing.B) in package pluto/testing. Benchmarks run
only when -bench selects them.

A package is a directory, a standard library import path such as
pluto/strings, or either followed by /... for every package below it. A
directory inside a standard library tree (lib/pluto/strings) is tested with
that tree as the standard library. With no packages, test runs the package
in the current directory.

Flags:
  -run regexp    run only the tests matching regexp; slashes separate the
                 patterns for each level of subtests, and below the top level
                 a pattern is literal text with optional ^ and $ anchors
  -bench regexp  run the benchmarks matching regexp
  -v             print every test's name, result and log
  -count n       run each test and benchmark n times
  -O level       optimisation level: 0, 1 or 2 (default 2)
  -brief         print each build error on one line, without its
                 explanation (default from $PLUTO_BRIEF)

The exit status is 1 if any package fails to build or any test fails.`,
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		for _, a := range args {
			if a == "-h" || a == "-help" || a == "--help" {
				return cmd.Help()
			}
		}
		if code := runTests(args, os.Stdout, os.Stderr); code != 0 {
			os.Exit(code)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(testCmd)
}

// runTests runs pluto test with the given arguments and returns the exit
// status.
func runTests(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("pluto test", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var opts build.TestOptions
	flags.StringVar(&opts.Run, "run", "", "run only the tests matching `regexp`")
	flags.StringVar(&opts.Bench, "bench", "", "run the benchmarks matching `regexp`")
	flags.BoolVar(&opts.Verbose, "v", false, "verbose output")
	flags.IntVar(&opts.Count, "count", 1, "run each test and benchmark `n` times")
	optLevel := flags.Int("O", 2, "optimisation `level`")
	flags.BoolVar(&briefErrors, "brief", briefErrors, briefUsage)
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if opts.Count < 1 {
		say(stderr, "pluto test: -count must be at least 1\n")
		return 2
	}
	if err := opts.Check(); err != nil {
		say(stderr, "pluto test: %v\n", err)
		return 2
	}
	targets, err := expandTestTargets(flags.Args())
	if err != nil {
		say(stderr, "pluto test: %v\n", err)
		return 1
	}
	failed := false
	for _, t := range targets {
		if !testPackage(t, &opts, *optLevel, stdout, stderr) {
			failed = true
		}
	}
	if failed {
		say(stdout, "FAIL\n")
		return 1
	}
	return 0
}

// say writes to the terminal. Like go test, pluto test does not stop for a
// failed write of its report.
func say(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

// A testTarget is one package to test, with the standard library to build
// it against.
type testTarget struct {
	pkg    string // directory or standard library import path
	stdlib fs.FS
}

// expandTestTargets turns the command-line packages into targets.
func expandTestTargets(args []string) ([]testTarget, error) {
	if len(args) == 0 {
		args = []string{"."}
	}
	var targets []testTarget
	for _, arg := range args {
		recursive := false
		if rest, ok := strings.CutSuffix(arg, "/..."); ok {
			arg, recursive = rest, true
			if arg == "" {
				arg = "/"
			}
		} else if arg == "..." {
			arg, recursive = ".", true
		}
		if isStdlibPath(arg) {
			lib := stdlib()
			if !recursive {
				targets = append(targets, testTarget{arg, lib})
				continue
			}
			err := fs.WalkDir(lib, arg, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() && hasSource(lib, p) {
					targets = append(targets, testTarget{p, lib})
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
			continue
		}
		dirs := []string{arg}
		if recursive {
			dirs = dirs[:0]
			err := filepath.WalkDir(arg, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !d.IsDir() {
					return nil
				}
				name := d.Name()
				if p != arg && (name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
					return filepath.SkipDir
				}
				if hasSource(os.DirFS(p), ".") {
					dirs = append(dirs, p)
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
		}
		for _, dir := range dirs {
			targets = append(targets, dirTarget(dir))
		}
	}
	return targets, nil
}

func isStdlibPath(p string) bool { return p == "pluto" || strings.HasPrefix(p, "pluto/") }

// hasSource reports whether directory dir of fsys holds .p files.
func hasSource(fsys fs.FS, dir string) bool {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".p") {
			return true
		}
	}
	return false
}

// dirTarget makes the target for a package directory. A directory inside a
// standard library tree, one whose ancestor holds pluto/rt, is tested as
// that standard library package, built against that tree.
func dirTarget(dir string) testTarget {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return testTarget{dir, stdlib()}
	}
	for d := filepath.Dir(abs); ; d = filepath.Dir(d) {
		if filepath.Base(d) == "pluto" {
			root := filepath.Dir(d)
			if st, err := os.Stat(filepath.Join(d, "rt")); err == nil && st.IsDir() {
				rel, err := filepath.Rel(root, abs)
				if err == nil {
					return testTarget{filepath.ToSlash(rel), os.DirFS(root)}
				}
			}
		}
		if filepath.Dir(d) == d {
			return testTarget{dir, stdlib()}
		}
	}
}

// testPackage builds and runs one package's tests, printing the results.
// It reports whether the package passed.
func testPackage(t testTarget, opts *build.TestOptions, optLevel int, stdout, stderr io.Writer) bool {
	work, err := os.MkdirTemp("", "pluto-test-")
	if err != nil {
		say(stderr, "pluto test: %v\n", err)
		return false
	}
	defer func() { _ = os.RemoveAll(work) }()
	cfg := &build.Config{
		Output:   filepath.Join(work, "test"),
		OptLevel: optLevel,
		Stdlib:   t.stdlib,
	}
	bin, err := build.BuildTest(cfg, t.pkg, opts)
	if errors.Is(err, build.ErrNoTestFiles) {
		say(stdout, "?   \t%s\t[no test files]\n", bin.Path)
		return true
	}
	if err != nil {
		name := t.pkg
		if bin != nil && bin.Path != "" {
			name = bin.Path
		}
		say(stderr, "# %s\n", name)
		printError(stderr, err)
		say(stdout, "FAIL\t%s [build failed]\n", name)
		return false
	}

	r := newTestReporter(stdout, opts.Verbose || opts.Bench != "", opts.Verbose)
	start := time.Now()
	runErr := runTestBinary(cfg.Output, r)
	elapsed := time.Since(start)
	ok := r.end(runErr)
	if ok {
		suffix := ""
		if r.noTests {
			suffix = " [no tests to run]"
		}
		say(stdout, "ok  \t%s\t%.3fs%s\n", bin.Path, elapsed.Seconds(), suffix)
	} else {
		say(stdout, "FAIL\t%s\t%.3fs\n", bin.Path, elapsed.Seconds())
	}
	return ok
}

// runTestBinary runs a test binary with its standard output and error
// merged, feeding each line to r.
func runTestBinary(binary string, r *testReporter) error {
	pr, pw, err := os.Pipe()
	if err != nil {
		return err
	}
	cmd := exec.Command(binary)
	cmd.Dir = filepath.Dir(binary)
	cmd.Stdout, cmd.Stderr = pw, pw
	err = cmd.Start()
	_ = pw.Close()
	if err != nil {
		_ = pr.Close()
		return err
	}
	sc := bufio.NewScanner(pr)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		r.line(sc.Text())
	}
	_ = pr.Close()
	return cmd.Wait()
}

// testReporter turns the output of a test binary into go test's output.
// The binary announces each test with "=== RUN" and prints its log lines
// and its result line as they happen; the reporter rebuilds the nesting of
// subtests from that, so it can print subtest results under their parent
// and, without -v, only the failed tests with their logs. Because every
// test is announced before it runs, a panic that ends the binary is
// reported as a failure of the test that was running.
type testReporter struct {
	w       io.Writer    // where the report finally goes
	out     io.Writer    // where the report is written: w, or buf until the package's result is known
	buf     bytes.Buffer // the report without -v, printed only if the package fails
	stream  bool         // print as the binary runs, and print the final PASS line
	verbose bool         // print every test, not only failures
	stack   []*testNode  // the tests running now, outermost first
	crash   []string     // output after a panic
	summary string       // the binary's final PASS or FAIL line
	noTests bool         // the binary found no tests to run
	started map[*testNode]time.Time
}

type testNode struct {
	name   string
	depth  int
	bench  bool
	status string // PASS, FAIL or SKIP
	dur    string // "(0.01s)"
	items  []testItem
	parent *testNode
}

// A testItem is a log line of a test, or one of its subtests.
type testItem struct {
	line  string
	child *testNode
}

func newTestReporter(w io.Writer, stream, verbose bool) *testReporter {
	r := &testReporter{w: w, stream: stream, verbose: verbose, started: map[*testNode]time.Time{}}
	r.out = w
	if !stream {
		r.out = &r.buf
	}
	return r
}

func (r *testReporter) printf(format string, args ...any) { say(r.out, format, args...) }

func (r *testReporter) top() *testNode {
	if len(r.stack) == 0 {
		return nil
	}
	return r.stack[len(r.stack)-1]
}

// line handles one line of the binary's output.
func (r *testReporter) line(s string) {
	if r.crash != nil {
		r.crash = append(r.crash, s)
		return
	}
	top := r.top()
	switch {
	case strings.HasPrefix(s, "=== RUN   "):
		n := &testNode{name: strings.TrimPrefix(s, "=== RUN   ")}
		n.bench = strings.HasPrefix(n.name, "Benchmark")
		if top != nil {
			n.parent, n.depth = top, top.depth+1
			top.items = append(top.items, testItem{child: n})
		}
		r.stack = append(r.stack, n)
		r.started[n] = time.Now()
		if r.verbose && !n.bench {
			r.printf("%s\n", s)
		}
	case top != nil && r.result(top, s):
		r.stack = r.stack[:len(r.stack)-1]
		if top.parent == nil {
			r.finish(top)
		}
	case top == nil && (s == "PASS" || s == "FAIL"):
		r.summary = s
	case s == "testing: warning: no tests to run":
		r.noTests = true
		if r.verbose {
			r.printf("%s\n", s)
		}
	case top != nil && strings.HasPrefix(s, "panic: "):
		r.crash = append(r.crash, s)
	case top != nil && strings.HasPrefix(s, "    "):
		top.items = append(top.items, testItem{line: s})
		if r.verbose {
			r.printf("%s\n", s)
		}
	default:
		r.printf("%s\n", s)
	}
}

// result recognises n's result line, "--- PASS: name (0.00s)", and
// records it.
func (r *testReporter) result(n *testNode, s string) bool {
	for _, status := range []string{"PASS", "FAIL", "SKIP"} {
		rest, ok := strings.CutPrefix(s, "--- "+status+": ")
		if !ok {
			continue
		}
		name, dur, ok := strings.Cut(rest, " (")
		if !ok || name != n.name {
			return false
		}
		n.status, n.dur = status, "("+dur
		return true
	}
	return false
}

// finish prints a finished top-level test.
func (r *testReporter) finish(n *testNode) {
	if r.verbose {
		r.printResults(n)
		return
	}
	if n.status == "FAIL" {
		r.printFailure(n)
	}
}

// printResults prints the result lines of n and its subtests, as go test
// -v does once a top-level test ends. The log lines are out already.
func (r *testReporter) printResults(n *testNode) {
	if !n.bench || n.status != "PASS" {
		r.printf("%s--- %s: %s %s\n", indent(n.depth), n.status, n.name, n.dur)
	}
	for _, it := range n.items {
		if it.child != nil {
			r.printResults(it.child)
		}
	}
}

// printFailure prints a failed test with its log and its failed subtests,
// indented by depth, as go test does without -v.
func (r *testReporter) printFailure(n *testNode) {
	r.printf("%s--- FAIL: %s %s\n", indent(n.depth), n.name, n.dur)
	for _, it := range n.items {
		switch {
		case it.child == nil:
			r.printf("%s%s\n", indent(n.depth), it.line)
		case it.child.status == "FAIL":
			r.printFailure(it.child)
		}
	}
}

func indent(depth int) string { return strings.Repeat("    ", depth) }

// end handles the binary's exit and reports whether the package passed.
func (r *testReporter) end(runErr error) bool {
	passed := runErr == nil && r.summary == "PASS"
	if running := r.top(); running != nil {
		// The binary died inside a test, most likely by panicking.
		status := "exited"
		if runErr != nil {
			status = "died (" + runErr.Error() + ")"
		}
		note := fmt.Sprintf("    test binary %s while %s was running; later tests did not run", status, running.name)
		running.items = append(running.items, testItem{line: note})
		if r.verbose {
			r.printf("%s\n", note)
		}
		for i := len(r.stack) - 1; i >= 0; i-- {
			n := r.stack[i]
			n.status = "FAIL"
			n.dur = fmt.Sprintf("(%.2fs)", time.Since(r.started[n]).Seconds())
		}
		r.finish(r.stack[0])
		r.stack = nil
		for _, s := range r.crash {
			r.printf("%s\n", s)
		}
		r.summary = "FAIL"
		passed = false
	} else if runErr != nil && r.summary != "FAIL" {
		r.printf("test binary failed: %v\n", runErr)
		r.summary = "FAIL"
	}
	if r.summary == "" {
		r.summary = "FAIL"
	}
	if r.stream || !passed {
		r.printf("%s\n", r.summary)
	}
	if !passed && !r.stream {
		_, _ = r.w.Write(r.buf.Bytes())
	}
	return passed
}
