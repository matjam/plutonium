package tests

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Helpers shared by the tests ported from internal/build at the go-cutover
// tag (the build_*_test.go files). They map the old build.Config fields to
// the flags of pluto build: Freestanding and Kernel are --mode, CompileOnly
// is -c, Listing is -S, NoGenCheck is --gencheck=false, NoTrace is
// --trace=false, Alloc is --alloc, Format is --format, LinkerScript is
// --ldscript and InlineNotes is --inlinenotes.

// single is the files of a program of one main.p.
func single(src string) map[string]string { return map[string]string{"main.p": src} }

// writeTree writes files, named by slash-separated paths relative to the
// directory, to a new directory and returns it.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, src := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// A tryResult is what building a program in a directory gave.
type tryResult struct {
	dir    string // the program's directory
	exe    string // the output file
	out    string // pluto's standard output and standard error
	status int    // pluto's exit status
}

// tryBuild writes files to a new directory and builds them with flags from
// inside it, so that diagnostics name the files as ./main.p does. It does
// not fail the test when the build fails.
func tryBuild(t *testing.T, files map[string]string, flags ...string) tryResult {
	t.Helper()
	return tryBuildDir(t, writeTree(t, files), flags...)
}

// tryBuildDir is tryBuild for a directory that already has the program.
func tryBuildDir(t *testing.T, dir string, flags ...string) tryResult {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "prog")
	args := append([]string{"build"}, flags...)
	args = append(args, "-o", exe, ".")
	out, status := runPluto(t, dir, nil, args...)
	return tryResult{dir: dir, exe: exe, out: out, status: status}
}

// mustBuild builds files with flags and returns the executable's path. A
// build that fails fails the test.
func mustBuild(t *testing.T, files map[string]string, flags ...string) string {
	t.Helper()
	r := tryBuild(t, files, flags...)
	if r.status != 0 {
		t.Fatalf("build %s: exit status %d\n%s", strings.Join(flags, " "), r.status, r.out)
	}
	return r.exe
}

// buildFails builds src as a main package with flags and returns the
// diagnostics pluto printed, or "" if the build succeeded.
func buildFails(t *testing.T, src string, flags ...string) string {
	t.Helper()
	r := tryBuild(t, single(src), flags...)
	if r.status == 0 {
		return ""
	}
	return r.out
}

// runSrc builds src at -O level with flags, runs it and returns its standard
// output followed by its standard error, and its exit status.
func runSrc(t *testing.T, src string, level int, flags ...string) (string, int) {
	t.Helper()
	return runFiles(t, single(src), level, flags...)
}

// runFiles is runSrc for a program of several files.
func runFiles(t *testing.T, files map[string]string, level int, flags ...string) (string, int) {
	t.Helper()
	exe := mustBuild(t, files, append([]string{optFlag(level)}, flags...)...)
	return runExe(t, exe, true)
}

// buildDebugAlloc builds src with --alloc=debug at -O level and returns the
// executable's path.
func buildDebugAlloc(t *testing.T, src string, level int, flags ...string) string {
	t.Helper()
	return mustBuild(t, single(src), append([]string{optFlag(level), "--alloc=debug"}, flags...)...)
}

// runExeEnv runs exe with env added to its environment and returns its
// output (stdout, then stderr) and exit status. A death by signal fails the
// test.
func runExeEnv(t *testing.T, exe string, env ...string) (string, int) {
	t.Helper()
	return runExe(t, exe, true, env...)
}

// runDebugAllocEnv is runDebugAlloc with extra environment variables.
func runDebugAllocEnv(t *testing.T, exe, settings string, env ...string) (string, int) {
	t.Helper()
	var out string
	var status int
	for range joinAttempts {
		out, status = runExe(t, exe, true, append([]string{"PLUTO_DEBUGALLOC=" + settings}, env...)...)
		if !strings.Contains(out, abandonMark) {
			return out, status
		}
	}
	t.Fatalf("%d runs in a row took the abandon path of section 15.4; last output %q, status %d", joinAttempts, out, status)
	return out, status
}

// runDebugAllocRaw is runDebugAlloc without the repetition of a run that took
// the abandon path.
func runDebugAllocRaw(t *testing.T, exe, settings string, env ...string) (string, int) {
	t.Helper()
	return runExe(t, exe, true, append([]string{"PLUTO_DEBUGALLOC=" + settings}, env...)...)
}

// runWithTimeout runs exe with env added to its environment and returns its
// combined output and exit status, failing the test if it runs for longer
// than limit.
func runWithTimeout(t *testing.T, limit time.Duration, exe string, env ...string) (string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("%s did not finish in %v; output %q", exe, limit, out)
	}
	var exited *exec.ExitError
	if errors.As(err, &exited) {
		return string(out), exited.ExitCode()
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(out), 0
}

// listingOf builds files with -S and returns the machine code listing.
func listingOf(t *testing.T, files map[string]string, flags ...string) string {
	t.Helper()
	dir := writeTree(t, files)
	asm := filepath.Join(t.TempDir(), "listing.s")
	r := tryBuildDir(t, dir, append([]string{"-S", asm}, flags...)...)
	if r.status != 0 {
		t.Fatalf("build: exit status %d\n%s", r.status, r.out)
	}
	data, err := os.ReadFile(asm)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// funcListing returns the listing of function name from a whole listing.
func funcListing(t *testing.T, text, name string) string {
	t.Helper()
	i := strings.Index(text, "\n"+name+":\n")
	if i < 0 && strings.HasPrefix(text, name+":\n") {
		i = -1
	} else if i < 0 {
		t.Fatalf("%s not in listing:\n%s", name, text)
	}
	text = text[i+1:]
	if j := strings.Index(text, "\n\n"); j >= 0 {
		text = text[:j]
	}
	return text
}

// listFunc compiles src at -O2 and returns the machine listing of function
// name (for example "main.f").
func listFunc(t *testing.T, src, name string) string {
	t.Helper()
	return funcListing(t, listingOf(t, single(src), "-O2"), name)
}

// listFuncUnchecked is listFunc for a build without generation checks (the
// unchecked mode), for tests of code shapes the checks would add to: a
// check before a loop is a call to the panic path, which needs a frame.
func listFuncUnchecked(t *testing.T, src, name string) string {
	t.Helper()
	return funcListing(t, listingOf(t, single(src), "-O2", "--gencheck=false"), name)
}

// panicHandlerSource is the panic handler that freestanding and kernel test
// programs, which must have one, are built with.
const panicHandlerSource = `package main

import "pluto/rt"

//pu:panic
func testPanicHandler(p *rt.Panic) {
	for {
	}
}
`

// kernelCompile compiles src as a kernel-mode main package, with a panic
// handler, to an object file at -O level. It returns the machine code
// listing and, if the build failed, the diagnostics (one line each).
func kernelCompile(t *testing.T, src string, level int) (listing, errs string) {
	t.Helper()
	return modeCompile(t, src, level, "kernel", true)
}

// modeCompile is kernelCompile for --mode mode, with the diagnostics in the
// brief form (one line each) or in full, with their explanations.
func modeCompile(t *testing.T, src string, level int, mode string, brief bool) (listing, errs string) {
	t.Helper()
	dir := writeTree(t, map[string]string{"main.p": src, "panic_handler.p": panicHandlerSource})
	asm := filepath.Join(t.TempDir(), "listing.s")
	flags := []string{optFlag(level), "--mode", mode, "--gencheck=false", "-c", "-S", asm}
	if brief {
		flags = append(flags, "--brief")
	}
	r := tryBuildDir(t, dir, flags...)
	if r.status != 0 {
		return "", r.out
	}
	data, err := os.ReadFile(asm)
	if err != nil {
		t.Fatal(err)
	}
	return string(data), ""
}

// A progCase is a program checked at -O0 and -O2, and with one processor
// and with every CPU.
type progCase struct {
	name, src, want string
	status          int
}

// buildProgCases builds each case at -O0 and -O2 with flags, a few at a
// time, and returns the executables in that order. The tests that use it run
// the programs one at a time afterwards, since their timing should not
// compete with the compiler for the CPU.
func buildProgCases(t *testing.T, cases []progCase, flags ...string) []string {
	t.Helper()
	pluto := compiler(t)
	type job struct {
		name, src string
		level     int
	}
	var jobs []job
	for _, c := range cases {
		for _, level := range []int{0, 2} {
			jobs = append(jobs, job{c.name + "/O" + strconv.Itoa(level), c.src, level})
		}
	}
	progs := make([]string, len(jobs))
	results := make([]tryResult, len(jobs))
	dirs := make([]string, len(jobs))
	for i, j := range jobs {
		dirs[i] = writeTree(t, single(j.src))
	}
	sem := make(chan struct{}, min(runtime.GOMAXPROCS(0), 4))
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			// runPluto only calls t.Fatal on a failure to run pluto at all.
			results[i] = tryBuildNoFatal(pluto, dirs[i], append([]string{optFlag(j.level)}, flags...)...)
		}()
	}
	wg.Wait()
	failed := false
	for i, r := range results {
		if r.status != 0 {
			t.Errorf("%s: build: exit status %d\n%s", jobs[i].name, r.status, r.out)
			failed = true
		}
		progs[i] = r.exe
	}
	if failed {
		t.FailNow()
	}
	return progs
}

// tryBuildNoFatal is tryBuildDir for use outside the test's goroutine: it
// reports a failure to run pluto as status -1.
func tryBuildNoFatal(pluto, dir string, flags ...string) tryResult {
	exe := filepath.Join(dir, "prog")
	args := append([]string{"build"}, flags...)
	args = append(args, "-o", exe, ".")
	root, err := filepath.Abs(filepath.Join("..", "lib"))
	if err != nil {
		return tryResult{out: err.Error(), status: -1}
	}
	cmd := exec.Command(pluto, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PLUTOROOT="+root, "PLUTO_BRIEF=")
	out, err := cmd.CombinedOutput()
	status := 0
	var ee *exec.ExitError
	switch {
	case errors.As(err, &ee):
		status = ee.ExitCode()
	case err != nil:
		return tryResult{dir: dir, exe: exe, out: err.Error(), status: -1}
	}
	return tryResult{dir: dir, exe: exe, out: string(out), status: status}
}

// runProgCases builds the cases with flags and runs each at -O0 and -O2,
// with one processor and with every CPU.
func runProgCases(t *testing.T, cases []progCase, flags ...string) {
	t.Helper()
	progs := buildProgCases(t, cases, flags...)
	for _, c := range cases {
		for _, level := range []int{0, 2} {
			prog := progs[0]
			progs = progs[1:]
			for _, procs := range []string{"1", ""} {
				name := c.name + "/O" + strconv.Itoa(level)
				if procs != "" {
					name += "/procs" + procs
				}
				t.Run(name, func(t *testing.T) {
					var env []string
					if procs != "" {
						env = append(env, "PLUTO_MAXPROCS="+procs)
					}
					got, status := runExeEnv(t, prog, env...)
					if status != c.status || got != c.want {
						t.Errorf("status %d, output:\n%s\nwant status %d, output:\n%s", status, got, c.status, c.want)
					}
				})
			}
		}
	}
}

// squash collapses the white space of s, as the diagnostics of a test that
// checks a sentence of an explanation need.
func squash(s string) string { return strings.Join(strings.Fields(s), " ") }

// matchTrace reports whether out matches the regular expression re, in which
// FILE stands for the path of main.p in a trace.
func matchTrace(re string) *regexp.Regexp {
	return regexp.MustCompile(`\A` + strings.ReplaceAll(re, "FILE", `\./main\.p`) + `\z`)
}
