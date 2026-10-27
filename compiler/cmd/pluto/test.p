// Ported by hand from cmd/pluto/test.go at the go-cutover tag.

package main

// pluto test: build a test binary for each package, run it, and report the
// results in the format of go test. The Go command parses its flags with
// Go's flag package, and so does this one, with pluto/flag; it runs each
// test binary with pluto/os/exec, its standard output and error sharing
// one pipe, and feeds the output to the reporter line by line as it
// arrives.
//
// Beyond the Go command, -c builds the test binary of one package without
// running it, into the file -o names, as go test -c does; the Go tests of
// pluto test drive the self-hosted compiler through it.

import (
	"compiler/compat"
	"compiler/compat/fs"
	"compiler/internal/build"
	"compiler/internal/diag"
	"compiler/pu"
	"pluto/errors"
	"pluto/flag"
	"pluto/io"
	"pluto/os"
	"pluto/os/exec"
	"pluto/path/filepath"
	"pluto/strings"
)

// testLong is the help text of pluto test.
const testLong = `test builds a test binary for each package from the package's files and
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
  -c             build the test binary of one package without running it
  -o file        with -c, write the test binary to file (default pkg.test)

The exit status is 1 if any package fails to build or any test fails.`

// testUsage is cobra's usage text of pluto test.
const testUsage = `Usage:
  pluto test [-run regexp] [-bench regexp] [-v] [-count n] [-O level] [packages] [flags]

Flags:
  -h, --help   help for test
`

// testCommand runs pluto test with the arguments after "test": the help
// text if any of them asks for it, as cobra prints it, and runTests
// otherwise.
func testCommand(args []string, stdout, stderr io.Writer) int {
	for _, a := range args {
		if a == "-h" || a == "-help" || a == "--help" {
			say(stdout, "%s\n\n%s", pu.Box(testLong), pu.Box(testUsage))
			return 0
		}
	}
	return runTests(args, stdout, stderr)
}

// runTests runs pluto test with the given arguments and returns the exit
// status.
func runTests(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("pluto test", flag.ContinueOnError)
	defer flags.Drop()
	flags.SetOutput(stderr)
	// The FlagSet keeps pointers to the variables it sets, so they live in
	// the arena, not in the frame.
	f := pu.New(testFlags{brief: envBrief(), trimpath: true, genCheck: true})
	opts := &f.opts
	flags.StringVar(&opts.Run, "run", "", "run only the tests matching `regexp`")
	flags.StringVar(&opts.Bench, "bench", "", "run the benchmarks matching `regexp`")
	flags.BoolVar(&opts.Verbose, "v", false, "verbose output")
	flags.IntVar(&opts.Count, "count", 1, "run each test and benchmark `n` times")
	flags.IntVar(&f.optLevel, "O", 2, "optimization `level`")
	flags.BoolVar(&f.brief, "brief", f.brief, "print each error on one line, without its explanation (default from $PLUTO_BRIEF)")
	flags.BoolVar(&f.trimpath, "trimpath", true, "record source paths relative to the module root and the standard library")
	flags.BoolVar(&f.genCheck, "gencheck", true, "check heap pointers' generations on every access (false: the unchecked mode)")
	flags.BoolVar(&f.compileOnly, "c", false, "build the test binary without running it")
	flags.StringVar(&f.output, "o", "", "write the test binary built with -c to `file`")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	optLevel, brief, trimpath, genCheck := f.optLevel, f.brief, f.trimpath, f.genCheck
	compileOnly, output := f.compileOnly, f.output
	if opts.Count < 1 {
		say(stderr, "pluto test: -count must be at least 1\n")
		return 2
	}
	if err := opts.Check(); err != nil {
		say(stderr, "pluto test: %s\n", pu.Box(err.Error()))
		return 2
	}
	if output != "" && !compileOnly {
		say(stderr, "pluto test: -o needs -c\n")
		return 2
	}
	targets, err := expandTestTargets(flags.Args())
	if err != nil {
		say(stderr, "pluto test: %s\n", pu.Box(err.Error()))
		return 1
	}
	if compileOnly && len(targets) != 1 {
		say(stderr, "pluto test: -c needs exactly one package\n")
		return 2
	}
	failed := false
	for _, t := range targets {
		cfg := pu.New(build.Config{OptLevel: optLevel, NoTrimPath: !trimpath, NoGenCheck: !genCheck})
		if compileOnly {
			if !compileTest(t, opts, cfg, output, brief, stdout, stderr) {
				failed = true
			}
			continue
		}
		if !testPackage(t, opts, cfg, brief, stdout, stderr) {
			failed = true
		}
	}
	if failed {
		say(stdout, "FAIL\n")
		return 1
	}
	return 0
}

// testFlags holds the values of pluto test's flags.
type testFlags struct {
	opts        build.TestOptions
	optLevel    int
	brief       bool
	trimpath    bool
	genCheck    bool
	compileOnly bool
	output      string
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
		args = compat.Append([]string(nil), ".")
	}
	var targets []testTarget
	for _, arg := range args {
		recursive := false
		if strings.HasSuffix(arg, "/...") {
			arg, recursive = strings.TrimSuffix(arg, "/..."), true
			if arg == "" {
				arg = "/"
			}
		} else if arg == "..." {
			arg, recursive = ".", true
		}
		if isStdlibPath(arg) {
			lib := stdlib()
			if !recursive {
				targets = compat.Append(targets, testTarget{arg, lib})
				continue
			}
			var dirs []string
			dirs = walkStdlib(lib, arg, dirs) or return nil, err
			for _, d := range dirs {
				targets = compat.Append(targets, testTarget{d, lib})
			}
			continue
		}
		var dirs []string
		if recursive {
			if _, err := fs.OSStat(arg); err != nil {
				return nil, err
			}
			dirs = walkDirs(arg, arg, dirs)
		} else {
			dirs = compat.Append(dirs, arg)
		}
		for _, dir := range dirs {
			targets = compat.Append(targets, dirTarget(dir))
		}
	}
	return targets, nil
}

// walkStdlib appends to dirs dir and the directories below it in lib, in
// lexical order, that hold .p files, as fs.WalkDir visits them.
func walkStdlib(lib fs.FS, dir string, dirs []string) ([]string, error) {
	entries := fs.ReadDir(lib, dir) or return dirs, err
	if hasSourceEntries(entries) {
		dirs = compat.Append(dirs, dir)
	}
	for _, e := range entries {
		if e.IsDir() {
			dirs = walkStdlib(lib, compat.Concat(dir, "/", e.Name()), dirs) or return dirs, err
		}
	}
	return dirs, nil
}

// walkDirs appends to dirs dir and the directories below it, in lexical
// order, that hold .p files, skipping the directories below root named
// testdata or starting with "." or "_", as the Go command's
// filepath.WalkDir does. A directory it cannot read is left out.
func walkDirs(root, dir string, dirs []string) []string {
	if dir != root {
		name := filepath.Base(dir)
		if name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			return dirs
		}
	}
	entries, err := fs.OSReadDir(dir)
	if err != nil {
		return dirs
	}
	if hasSourceEntries(entries) {
		dirs = compat.Append(dirs, dir)
	}
	for _, e := range entries {
		if e.IsDir() {
			dirs = walkDirs(root, filepath.Join(dir, e.Name()), dirs)
		}
	}
	return dirs
}

func isStdlibPath(p string) bool { return p == "pluto" || strings.HasPrefix(p, "pluto/") }

// hasSourceEntries reports whether a directory with entries holds .p
// files.
func hasSourceEntries(entries []fs.DirEntry) bool {
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
			if st, err := fs.OSStat(filepath.Join(d, "rt")); err == nil && st.IsDir() {
				rel, err := filepath.Rel(root, abs)
				if err == nil {
					return testTarget{filepath.ToSlash(rel), fs.DirFS(root)}
				}
			}
		}
		if filepath.Dir(d) == d {
			return testTarget{dir, stdlib()}
		}
	}
}

// buildTestBinary builds the test binary of t into output and prints why
// it did not: the line of a package without tests, or the build errors.
// It reports whether there is a binary to run, and, when there is none,
// whether the package still passes.
func buildTestBinary(t testTarget, opts *build.TestOptions, cfg *build.Config, output string, brief bool, stdout, stderr io.Writer) (*build.TestBinary, bool, bool) {
	cfg.Output = output
	cfg.Stdlib = t.stdlib
	bin, err := build.BuildTest(cfg, t.pkg, opts)
	if errors.Is(err, build.ErrNoTestFiles) {
		say(stdout, "?   \t%s\t[no test files]\n", pu.Box(bin.Path))
		return nil, false, true
	}
	if err != nil {
		name := t.pkg
		if bin != nil && bin.Path != "" {
			name = bin.Path
		}
		say(stderr, "# %s\n", pu.Box(name))
		diag.Print(stderr, err, brief)
		say(stdout, "FAIL\t%s [build failed]\n", pu.Box(name))
		return nil, false, false
	}
	return bin, true, true
}

// compileTest builds the test binary of t into output, or into pkg.test in
// the current directory, and reports whether it built.
func compileTest(t testTarget, opts *build.TestOptions, cfg *build.Config, output string, brief bool, stdout, stderr io.Writer) bool {
	if output == "" {
		output = compat.Concat(filepath.Base(t.pkg), ".test")
	}
	_, _, ok := buildTestBinary(t, opts, cfg, output, brief, stdout, stderr)
	return ok
}

// testPackage builds and runs one package's tests, printing the results.
// It reports whether the package passed. cfg holds the build settings from
// the command line.
func testPackage(t testTarget, opts *build.TestOptions, cfg *build.Config, brief bool, stdout, stderr io.Writer) bool {
	work, err := os.MkdirTemp("", "pluto-test-")
	if err != nil {
		say(stderr, "pluto test: %s\n", pu.Box(err.Error()))
		return false
	}
	defer removeWork(work)
	bin, built, ok := buildTestBinary(t, opts, cfg, filepath.Join(work, "test"), brief, stdout, stderr)
	if !built {
		return ok
	}

	r := newTestReporter(stdout, opts.Verbose || opts.Bench != "", opts.Verbose)
	start := compat.Now()
	runErr := runTestBinary(cfg.Output, r)
	elapsed := int64(compat.Since(start))
	ok = r.end(runErr)
	if ok {
		suffix := ""
		if r.noTests {
			suffix = " [no tests to run]"
		}
		say(stdout, "ok  \t%s\t%ss%s\n", pu.Box(bin.Path), pu.Box(seconds(elapsed, 3)), pu.Box(suffix))
	} else {
		say(stdout, "FAIL\t%s\t%ss\n", pu.Box(bin.Path), pu.Box(seconds(elapsed, 3)))
	}
	return ok
}

func removeWork(dir string) { _ = os.RemoveAll(dir) }

// seconds formats ns nanoseconds as seconds with digits decimals, as
// %.<digits>f formats them.
func seconds(ns int64, digits int) string {
	if ns < 0 {
		ns = 0
	}
	unit := int64(1000000000)
	for i := 0; i < digits; i++ {
		unit /= 10
	}
	v := (ns + unit/2) / unit
	scale := int64(1)
	for i := 0; i < digits; i++ {
		scale *= 10
	}
	frac := compat.Sprintfb("%d", compat.ArgI(v%scale))
	for len(frac) < digits {
		frac = compat.Concat("0", frac)
	}
	return compat.Concat(compat.Sprintfb("%d", compat.ArgI(v/scale)), ".", frac)
}

// runTestBinary runs a test binary with its standard output and error
// merged, feeding each line to r.
func runTestBinary(binary string, r *testReporter) error {
	lw := pu.New(lineWriter{r: r})
	cmd := exec.Command(binary)
	cmd.Dir = filepath.Dir(binary)
	cmd.Stdout, cmd.Stderr = lw, lw
	err := cmd.Run()
	cmd.Drop()
	lw.flush()
	return err
}

// A lineWriter splits what is written to it into lines, as bufio.Scanner
// does, and passes each to a reporter.
type lineWriter struct {
	r       *testReporter
	partial []byte // the start of a line not yet ended
}

func (w *lineWriter) Write(p []byte) (int, error) {
	start := 0
	for i := 0; i < len(p); i++ {
		if p[i] != '\n' {
			continue
		}
		var line string
		if len(w.partial) > 0 {
			w.partial = compat.AppendPlain(w.partial, p[start:i]...)
			line = scanLine(w.partial)
			w.partial = w.partial[:0]
		} else {
			line = scanLine(p[start:i])
		}
		w.r.line(line)
		start = i + 1
	}
	if start < len(p) {
		w.partial = compat.AppendPlain(w.partial, p[start:]...)
	}
	return len(p), nil
}

// flush passes on the last line, if the output did not end with a newline.
func (w *lineWriter) flush() {
	if len(w.partial) > 0 {
		w.r.line(scanLine(w.partial))
		w.partial = w.partial[:0]
	}
}

// scanLine returns a copy of the line b, without a trailing carriage
// return, as bufio.ScanLines does.
func scanLine(b []byte) string {
	if len(b) > 0 && b[len(b)-1] == '\r' {
		b = b[:len(b)-1]
	}
	return compat.String(b)
}

// testReporter turns the output of a test binary into go test's output.
// The binary announces each test with "=== RUN" and prints its log lines
// and its result line as they happen; the reporter rebuilds the nesting of
// subtests from that, so it can print subtest results under their parent
// and, without -v, only the failed tests with their logs. Because every
// test is announced before it runs, a panic that ends the binary is
// reported as a failure of the test that was running.
type testReporter struct {
	w       io.Writer     // where the report finally goes
	out     io.Writer     // where the report is written: w, or buf until the package's result is known
	buf     compat.Buffer // the report without -v, printed only if the package fails
	stream  bool          // print as the binary runs, and print the final PASS line
	verbose bool          // print every test, not only failures
	stack   []*testNode   // the tests running now, outermost first
	crash   []string      // output after a panic
	summary string        // the binary's final PASS or FAIL line
	noTests bool          // the binary found no tests to run
}

type testNode struct {
	name    string
	depth   int
	bench   bool
	status  string // PASS, FAIL or SKIP
	dur     string // "(0.01s)"
	items   []testItem
	parent  *testNode
	started compat.Time
}

// A testItem is a log line of a test, or one of its subtests.
type testItem struct {
	line  string
	child *testNode
}

func newTestReporter(w io.Writer, stream, verbose bool) *testReporter {
	r := pu.New(testReporter{w: w, stream: stream, verbose: verbose})
	r.out = w
	if !stream {
		r.out = &r.buf
	}
	return r
}

func (r *testReporter) println(s string) { say(r.out, "%s\n", pu.Box(s)) }

func (r *testReporter) top() *testNode {
	if len(r.stack) == 0 {
		return nil
	}
	return r.stack[len(r.stack)-1]
}

// line handles one line of the binary's output.
func (r *testReporter) line(s string) {
	if r.crash != nil {
		r.crash = compat.Append(r.crash, s)
		return
	}
	top := r.top()
	switch {
	case strings.HasPrefix(s, "=== RUN   "):
		n := pu.New(testNode{name: strings.TrimPrefix(s, "=== RUN   ")})
		n.bench = strings.HasPrefix(n.name, "Benchmark")
		if top != nil {
			n.parent, n.depth = top, top.depth+1
			top.items = compat.Append(top.items, testItem{child: n})
		}
		r.stack = compat.Append(r.stack, n)
		n.started = compat.Now()
		if r.verbose && !n.bench {
			r.println(s)
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
			r.println(s)
		}
	case top != nil && strings.HasPrefix(s, "panic: "):
		r.crash = compat.Append(r.crash, s)
	case top != nil && strings.HasPrefix(s, "    "):
		top.items = compat.Append(top.items, testItem{line: s})
		if r.verbose {
			r.println(s)
		}
	default:
		r.println(s)
	}
}

// result recognises n's result line, "--- PASS: name (0.00s)", and
// records it.
func (r *testReporter) result(n *testNode, s string) bool {
	var status string
	switch {
	case strings.HasPrefix(s, "--- PASS: "):
		status = "PASS"
	case strings.HasPrefix(s, "--- FAIL: "):
		status = "FAIL"
	case strings.HasPrefix(s, "--- SKIP: "):
		status = "SKIP"
	default:
		return false
	}
	rest := strings.SliceFrom(s, len("--- PASS: "))
	name, dur, ok := strings.Cut(rest, " (")
	if !ok || name != n.name {
		return false
	}
	n.status, n.dur = status, compat.Concat("(", dur)
	return true
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
		say(r.out, "%s--- %s: %s %s\n", pu.Box(indent(n.depth)), pu.Box(n.status), pu.Box(n.name), pu.Box(n.dur))
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
	say(r.out, "%s--- FAIL: %s %s\n", pu.Box(indent(n.depth)), pu.Box(n.name), pu.Box(n.dur))
	for _, it := range n.items {
		switch {
		case it.child == nil:
			say(r.out, "%s%s\n", pu.Box(indent(n.depth)), pu.Box(it.line))
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
			status = compat.Concat("died (", runErr.Error(), ")")
		}
		note := compat.Concat("    test binary ", status, " while ", running.name, " was running; later tests did not run")
		running.items = compat.Append(running.items, testItem{line: note})
		if r.verbose {
			r.println(note)
		}
		for i := len(r.stack) - 1; i >= 0; i-- {
			n := r.stack[i]
			n.status = "FAIL"
			n.dur = compat.Concat("(", seconds(int64(compat.Since(n.started)), 2), "s)")
		}
		r.finish(r.stack[0])
		r.stack = nil
		for _, s := range r.crash {
			r.println(s)
		}
		r.summary = "FAIL"
		passed = false
	} else if runErr != nil && r.summary != "FAIL" {
		say(r.out, "test binary failed: %s\n", pu.Box(runErr.Error()))
		r.summary = "FAIL"
	}
	if r.summary == "" {
		r.summary = "FAIL"
	}
	if r.stream || !passed {
		r.println(r.summary)
	}
	if !passed && !r.stream {
		_, _ = r.w.Write(r.buf.Bytes())
	}
	return passed
}
