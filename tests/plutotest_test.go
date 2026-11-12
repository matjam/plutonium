package tests

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The tests in this file check pluto test on the packages under
// testdata/testpkg and testdata/badtests.

// times matches the durations in test output, and benchResult the
// iteration count and time of a benchmark, which vary from run to run.
// stdlibLine matches the line numbers of standard library frames in a
// panic's stack trace, which change whenever pluto/testing does.
var (
	times       = regexp.MustCompile(`\d+\.\d+s`)
	benchResult = regexp.MustCompile(`(?m)^(Benchmark\w+)\t *\d+\t *[0-9.]+ ns/op$`)
	stdlibLine  = regexp.MustCompile(`(\((?:\$PLUTOROOT/)?pluto/[^:]+):\d+\)`)
)

// runTestCommand runs "pluto test" with args in this directory and
// returns its standard output, its standard error and its exit status.
func runTestCommand(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	return runPlutoSplit(t, ".", nil, "", append([]string{"test"}, args...)...)
}

// plutoTest runs "pluto test" with args and returns its normalised
// standard output and its exit status.
func plutoTest(t *testing.T, args ...string) (string, int) {
	t.Helper()
	stdout, stderr, code := runTestCommand(t, args...)
	if stderr != "" {
		t.Logf("stderr:\n%s", stderr)
	}
	out := benchResult.ReplaceAllString(stdout, "$1\tN\tT ns/op")
	if wd, err := os.Getwd(); err == nil {
		out = strings.ReplaceAll(out, wd, "$DIR")
	}
	out = stdlibLine.ReplaceAllString(out, "$1:N)")
	return times.ReplaceAllString(out, "T"), code
}

const calc = "testdata/testpkg/calc"

// crashTrace is the stack trace of testdata/testpkg/crash's panic, through
// the test runner. The package's files are named relative to the module
// root (testdata/testpkg), and the standard library's by import path.
const crashTrace = `    example.com/testpkg/crash.Div (./crash/crash.p:5)
    example.com/testpkg/crash.TestPanics.func1 (./crash/crash_test.p:12)
    pluto/testing.(*T).run.func1 (pluto/testing/testing.p:N)
    pluto/rt.CatchExitTask (pluto/rt/goexit.p:N)
    pluto/testing.(*T).run (pluto/testing/testing.p:N)
    pluto/testing.(*T).Run (pluto/testing/testing.p:N)
    example.com/testpkg/crash.TestPanics (./crash/crash_test.p:8)
    pluto/testing.(*T).run.func1 (pluto/testing/testing.p:N)
    pluto/rt.CatchExitTask (pluto/rt/goexit.p:N)
    pluto/testing.(*T).run (pluto/testing/testing.p:N)
    pluto/testing.(*M).Test (pluto/testing/testing.p:N)
    main.main (_testmain.p:15)
`

// crashTraceAbs is crashTrace with -trimpath=false: absolute paths, and
// standard library files under $PLUTOROOT.
const crashTraceAbs = `    example.com/testpkg/crash.Div ($DIR/testdata/testpkg/crash/crash.p:5)
    example.com/testpkg/crash.TestPanics.func1 ($DIR/testdata/testpkg/crash/crash_test.p:12)
    pluto/testing.(*T).run.func1 ($PLUTOROOT/pluto/testing/testing.p:N)
    pluto/rt.CatchExitTask ($PLUTOROOT/pluto/rt/goexit.p:N)
    pluto/testing.(*T).run ($PLUTOROOT/pluto/testing/testing.p:N)
    pluto/testing.(*T).Run ($PLUTOROOT/pluto/testing/testing.p:N)
    example.com/testpkg/crash.TestPanics ($DIR/testdata/testpkg/crash/crash_test.p:8)
    pluto/testing.(*T).run.func1 ($PLUTOROOT/pluto/testing/testing.p:N)
    pluto/rt.CatchExitTask ($PLUTOROOT/pluto/rt/goexit.p:N)
    pluto/testing.(*T).run ($PLUTOROOT/pluto/testing/testing.p:N)
    pluto/testing.(*M).Test ($PLUTOROOT/pluto/testing/testing.p:N)
    main.main (_testmain.p:15)
`

// TestPlutoTest runs pluto test on a fixture package with a passing test,
// a failing test, subtests (passing, failing with Fatalf, and skipped) and
// a benchmark, and checks the output and exit status in each mode.
func TestPlutoTest(t *testing.T) {
	tests := []struct {
		name string
		args []string
		code int
		want string
	}{
		{
			name: "failures",
			args: []string{calc},
			code: 1,
			want: `--- FAIL: TestWrong (T)
    checking Add
    Add(2, 2) = 4, want 5
--- FAIL: TestSub (T)
    --- FAIL: TestSub/fatal (T)
        stop 1
--- FAIL: TestCleanup (T)
    2+2 != 5
    deferred in check(2)
    deferred in the test
--- FAIL: TestScope (T)
    stop while tasks run
FAIL
FAIL	example.com/testpkg/calc	T
FAIL
`,
		},
		{
			name: "verbose",
			args: []string{"-v", calc},
			code: 1,
			want: `=== RUN   TestAdd
    added 2 and 3
--- PASS: TestAdd (T)
=== RUN   TestWrong
    checking Add
    Add(2, 2) = 4, want 5
--- FAIL: TestWrong (T)
=== RUN   TestSub
=== RUN   TestSub/small_numbers
=== RUN   TestSub/fatal
    stop 1
=== RUN   TestSub/skipped
    not today
--- FAIL: TestSub (T)
    --- PASS: TestSub/small_numbers (T)
    --- FAIL: TestSub/fatal (T)
    --- SKIP: TestSub/skipped (T)
=== RUN   TestCleanup
    2+2 != 5
    deferred in check(2)
    deferred in the test
--- FAIL: TestCleanup (T)
=== RUN   TestScope
    stop while tasks run
--- FAIL: TestScope (T)
FAIL
FAIL	example.com/testpkg/calc	T
FAIL
`,
		},
		{
			name: "passing",
			args: []string{"-run", "Add|Sub/small", calc},
			want: "ok  \texample.com/testpkg/calc\tT\n",
		},
		{
			name: "subtest anchors",
			args: []string{"-v", "-run", "Sub/^sk", calc},
			want: `=== RUN   TestSub
=== RUN   TestSub/skipped
    not today
--- PASS: TestSub (T)
    --- SKIP: TestSub/skipped (T)
PASS
ok  	example.com/testpkg/calc	T
`,
		},
		{
			name: "benchmark",
			args: []string{"-run", "^$", "-bench", "Add", calc},
			want: `goos: linux
goarch: amd64
pkg: example.com/testpkg/calc
BenchmarkAdd	N	T ns/op
PASS
ok  	example.com/testpkg/calc	T
`,
		},
		{
			name: "count",
			args: []string{"-v", "-count", "2", "-run", "TestAdd", calc},
			want: `=== RUN   TestAdd
    added 2 and 3
--- PASS: TestAdd (T)
=== RUN   TestAdd
    added 2 and 3
--- PASS: TestAdd (T)
PASS
ok  	example.com/testpkg/calc	T
`,
		},
		{
			name: "no tests to run",
			args: []string{"-run", "Nothing", calc},
			want: "ok  \texample.com/testpkg/calc\tT [no tests to run]\n",
		},
		{
			name: "no test files",
			args: []string{"testdata/testpkg/notests"},
			want: "?   \texample.com/testpkg/notests\t[no test files]\n",
		},
		{
			name: "panic",
			args: []string{"testdata/testpkg/crash"},
			code: 1,
			want: `--- FAIL: TestPanics (T)
    --- FAIL: TestPanics/divide (T)
        dividing by zero
        deferred: runs before the panic is reported
        test binary died (exit status 2) while TestPanics/divide was running; later tests did not run
panic: runtime error: integer divide by zero
` + crashTrace + `FAIL
FAIL	example.com/testpkg/crash	T
FAIL
`,
		},
		{
			name: "panic, absolute paths",
			args: []string{"-trimpath=false", "testdata/testpkg/crash"},
			code: 1,
			want: `--- FAIL: TestPanics (T)
    --- FAIL: TestPanics/divide (T)
        dividing by zero
        deferred: runs before the panic is reported
        test binary died (exit status 2) while TestPanics/divide was running; later tests did not run
panic: runtime error: integer divide by zero
` + crashTraceAbs + `FAIL
FAIL	example.com/testpkg/crash	T
FAIL
`,
		},
		{
			name: "panic verbose",
			args: []string{"-v", "testdata/testpkg/crash"},
			code: 1,
			want: `=== RUN   TestBefore
--- PASS: TestBefore (T)
=== RUN   TestPanics
=== RUN   TestPanics/divide
    dividing by zero
    deferred: runs before the panic is reported
    test binary died (exit status 2) while TestPanics/divide was running; later tests did not run
--- FAIL: TestPanics (T)
    --- FAIL: TestPanics/divide (T)
panic: runtime error: integer divide by zero
` + crashTrace + `FAIL
FAIL	example.com/testpkg/crash	T
FAIL
`,
		},
		{
			name: "recursive",
			args: []string{"-run", "Add$|Before", "testdata/testpkg/..."},
			code: 0,
			want: `ok  	example.com/testpkg/calc	T
ok  	example.com/testpkg/crash	T
?   	example.com/testpkg/notests	[no test files]
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, code := plutoTest(t, tt.args...)
			if code != tt.code {
				t.Errorf("exit status %d, want %d", code, tt.code)
			}
			if got != tt.want {
				t.Errorf("output:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

// TestPlutoTestStdlib runs the standard library's own tests, both by
// directory and by import path.
func TestPlutoTestStdlib(t *testing.T) {
	got, code := plutoTest(t, "../lib/pluto/strings", "pluto/sort")
	want := "ok  \tpluto/strings\tT\nok  \tpluto/sort\tT\n"
	if code != 0 || got != want {
		t.Errorf("exit status %d, output:\n%s\nwant:\n%s", code, got, want)
	}
	got, code = plutoTest(t, "-v", "-O", "0", "-run", "TestSortPatterns", "pluto/sort")
	if code != 0 || !strings.Contains(got, "    --- PASS: TestSortPatterns/descending (T)\n") {
		t.Errorf("exit status %d, output:\n%s", code, got)
	}
}

func TestPlutoTestErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		code int
		out  string // in standard output
		err  string // in standard error
		diag string // the diagnostic's code, in standard error; "" for none
	}{
		{"bad count", []string{"-count", "0", calc}, 2, "", "-count must be at least 1", ""},
		{"bad flag", []string{"-nope"}, 2, "", "flag provided but not defined", ""},
		{"bad run", []string{"-run", "a/b.c", calc}, 2, "", "may only be literal text", ""},
		{"bad regexp", []string{"-run", "(", calc}, 2, "", "invalid -run pattern", ""},
		{"bad bench", []string{"-bench", "(", calc}, 2, "", "invalid -bench pattern", ""},
		{"missing", []string{"testdata/nonexistent"}, 1, "FAIL\ttestdata/nonexistent [build failed]", "not a package directory", "E4002"},
		{"main package", []string{"../examples/01-exit"}, 1, "[build failed]", "testing a main package is not supported", "E4004"},
		{"external", []string{"testdata/badtests/external"}, 1, "FAIL\ttestdata/badtests/external [build failed]", "external test packages are not supported", ""},
		{"test signature", []string{"testdata/badtests/badsig"}, 1, "FAIL\tbadsig [build failed]",
			"badsig/y_test.p:5:6: wrong signature for TestTwo, must be: func TestTwo(t *testing.T)\n", "E4018"},
		{"benchmark signature", []string{"testdata/badtests/badsig"}, 1, "FAIL\tbadsig [build failed]",
			"badsig/y_test.p:7:6: wrong signature for BenchmarkTwo, must be: func BenchmarkTwo(b *testing.B)\n", "E4018"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code := runTestCommand(t, tt.args...)
			if code != tt.code {
				t.Errorf("exit status %d, want %d", code, tt.code)
			}
			if !strings.Contains(stdout, tt.out) {
				t.Errorf("stdout %q does not contain %q", stdout, tt.out)
			}
			if !strings.Contains(stderr, tt.err) {
				t.Errorf("stderr %q does not contain %q", stderr, tt.err)
			}
			if tt.diag != "" && !strings.Contains(stderr, "pluto explain "+tt.diag) {
				t.Errorf("stderr %q does not name code %s", stderr, tt.diag)
			}
		})
	}
}

// TestPlutoTestDirErrors checks the errors of pluto test for a directory
// it cannot test: one named like a standard library package or main
// outside a module (E4003), and one it cannot read (E4017).
func TestPlutoTestDirErrors(t *testing.T) {
	t.Run("reserved name", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "main")
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		src := "package main\n\nfunc F() int { return 1 }\n"
		if err := os.WriteFile(filepath.Join(dir, "f.p"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		_, stderr, code := runTestCommand(t, dir)
		if code != 1 || !strings.Contains(stderr, "cannot be tested from a directory named \"main\"") || !strings.Contains(stderr, "pluto explain E4003") {
			t.Errorf("exit status %d, stderr %q; want 1 and E4003", code, stderr)
		}
	})
	t.Run("unreadable", func(t *testing.T) {
		if os.Getuid() == 0 {
			t.Skip("root reads any directory")
		}
		dir := filepath.Join(t.TempDir(), "calc")
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "pluto.mod"), []byte("module example.com/calc\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		// Search but no read permission: the directory exists, and its
		// entries cannot be listed.
		if err := os.Chmod(dir, 0o311); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(dir, 0o755)
		_, stderr, code := runTestCommand(t, dir)
		if code != 1 || !strings.Contains(stderr, "reading package") || !strings.Contains(stderr, "pluto explain E4017") {
			t.Errorf("exit status %d, stderr %q; want 1 and E4017", code, stderr)
		}
	})
}
