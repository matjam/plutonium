package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runMainEnv, when set, makes the test binary run pluto's main instead of
// the tests, so the tests can run the command line end to end.
const runMainEnv = "PLUTO_TEST_RUN_MAIN"

func TestMain(m *testing.M) {
	if os.Getenv(runMainEnv) == "1" {
		main()
	}
	os.Exit(m.Run())
}

// plutoBinary returns the pluto to run: the compiler binary named by
// PLUTO_TEST_COMPILER, such as the self-hosted compiler, or else this test
// binary, which runs pluto's main when runMainEnv is set.
func plutoBinary(t *testing.T) string {
	t.Helper()
	compiler := os.Getenv("PLUTO_TEST_COMPILER")
	if compiler == "" {
		return os.Args[0]
	}
	abs, err := filepath.Abs(compiler)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// runPluto runs pluto with args in dir and returns its combined output and
// exit status.
func runPluto(t *testing.T, dir string, args ...string) (string, int) {
	t.Helper()
	root, err := filepath.Abs("../../lib")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(plutoBinary(t), args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), runMainEnv+"=1", "PLUTOROOT="+root)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err = cmd.Run()
	var ee *exec.ExitError
	switch {
	case errors.As(err, &ee):
		return out.String(), ee.ExitCode()
	case err != nil:
		t.Fatal(err)
	}
	return out.String(), 0
}

// writeProgram writes src as main.p of a new package directory and returns
// the directory.
func writeProgram(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// checkOutput checks that each of want appears exactly once in out, that
// out shows the usage text only if usage is set, and that the exit status
// is code.
func checkOutput(t *testing.T, out string, status, code int, usage bool, want ...string) {
	t.Helper()
	if status != code {
		t.Errorf("exit status %d, want %d", status, code)
	}
	for _, w := range want {
		if n := strings.Count(out, w); n != 1 {
			t.Errorf("%q appears %d times, want once", w, n)
		}
	}
	if n := strings.Count(out, "Usage:"); usage && n != 1 || !usage && n != 0 {
		t.Errorf("usage text appears %d times, want it %v", n, usage)
	}
	if t.Failed() {
		t.Logf("output:\n%s", out)
	}
}

const twoTypeErrors = `package main

func main() {
	var x int = "a"
	var y string = 1
	_, _ = x, y
}
`

// TestCompileErrors checks that build, run and test print each compile
// error once, as file:line:col: message, without the usage text, and exit
// with status 1.
func TestCompileErrors(t *testing.T) {
	dir := writeProgram(t, twoTypeErrors)
	want := []string{
		`main.p:4:14: cannot use "a" (untyped string constant) as int value in variable declaration` + "\n",
		`main.p:5:17: cannot use 1 (untyped int constant) as string value in variable declaration` + "\n",
	}
	for _, args := range [][]string{
		{"build", "-o", filepath.Join(t.TempDir(), "a.out"), "."},
		{"run", "."},
		{"test", "."},
	} {
		t.Run(args[0], func(t *testing.T) {
			out, status := runPluto(t, dir, args...)
			checkOutput(t, out, status, 1, false, want...)
			for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
				if strings.Contains(line, "main.p:") && !strings.HasPrefix(line, filepath.Join(dir, "main.p")+":") {
					t.Errorf("error line %q does not start with the file name", line)
				}
			}
		})
	}
}

// TestSyntaxErrors checks that every syntax error is printed, not only the
// first.
func TestSyntaxErrors(t *testing.T) {
	dir := writeProgram(t, "package main\n\nfunc main() {\n\tx := := 1\n\ty := := 2\n}\n")
	out, status := runPluto(t, dir, "build", "-o", filepath.Join(t.TempDir(), "a.out"), ".")
	checkOutput(t, out, status, 1, false,
		"main.p:4:7: expected operand, found ':='",
		"main.p:6:3: expected ';', found 'EOF'")
	if strings.Contains(out, "more error") {
		t.Errorf("output summarises errors instead of printing them:\n%s", out)
	}
}

// TestUsageErrors checks that a mistake in the flags or arguments prints
// the error and the command's usage, with exit status 2.
func TestUsageErrors(t *testing.T) {
	dir := writeProgram(t, "package main\n\nfunc main() {}\n")
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"build", "--bogus", "."}, "pluto build: unknown flag: --bogus\n"},
		{[]string{"build", "-O", "x", "."}, `pluto build: invalid argument "x" for "-O, --opt" flag`},
		{[]string{"build", "a", "b"}, "pluto build: accepts at most 1 arg(s), received 2\n"},
		{[]string{"run", "a", "b"}, "pluto run: accepts at most 1 arg(s), received 2\n"},
		{[]string{"fmt", "--bogus"}, "pluto fmt: unknown flag: --bogus\n"},
		{[]string{"bogus"}, `pluto: unknown command "bogus" for "pluto"`},
	}
	for _, tc := range tests {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			out, status := runPluto(t, dir, tc.args...)
			checkOutput(t, out, status, 2, true, tc.want)
		})
	}
}

// TestFlagValueErrors checks that a flag value no build accepts is
// reported once with the command's name, without the usage text, with exit
// status 2.
func TestFlagValueErrors(t *testing.T) {
	dir := writeProgram(t, "package main\n\nfunc main() {}\n")
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"build", "--mode", "ring0", "."}, `pluto build: unknown mode "ring0"`},
		{[]string{"build", "--target", "arm-linux", "."}, `pluto build: unsupported target "arm-linux"`},
		{[]string{"build", "--alloc=fancy", "."}, `pluto build: unknown allocator "fancy"`},
		{[]string{"build", "--alloc=debug", "--mode", "kernel", "."}, "pluto build: --alloc=debug needs a hosted build"},
		{[]string{"run", "--mode", "kernel", "."}, "pluto run: cannot run a kernel"},
	}
	for _, tc := range tests {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			out, status := runPluto(t, dir, tc.args...)
			checkOutput(t, out, status, 2, false, tc.want)
		})
	}
}

// TestBuildFailures checks errors after the source compiles, or that are
// not about the source: each is printed once, without the usage text, with
// exit status 1.
func TestBuildFailures(t *testing.T) {
	dir := writeProgram(t, "package main\n\nfunc main() {}\n")
	missing := filepath.Join(t.TempDir(), "missing.ld")
	out, status := runPluto(t, dir, "build", "-o", filepath.Join(t.TempDir(), "a.out"), "--ldscript", missing, ".")
	checkOutput(t, out, status, 1, false, "running ld failed: exit status 1\n", missing+":")

	out, status = runPluto(t, dir, "build", "./nope")
	checkOutput(t, out, status, 1, false, "nope: no such file or directory\n")

	kernel := writeProgram(t, "package main\n\ntype V struct{ X, Y float64 }\n\nfunc f(p *V) { p.X = p.X + p.Y }\n\nfunc main() {\n\tvar v V\n\tf(&v)\n}\n")
	out, status = runPluto(t, kernel, "build", "-o", filepath.Join(t.TempDir(), "a.out"), "--mode", "kernel", ".")
	checkOutput(t, out, status, 1, false, "main.p:5:22: float64 arithmetic is not allowed in kernel mode")
}

// TestInlineNotesFlag checks that pluto run --inlinenotes prints a note for
// a //pu:inline call left out of line, and nothing for the standard
// library, and that the program still runs.
func TestInlineNotesFlag(t *testing.T) {
	dir := writeProgram(t, `package main

//pu:inline
func fail(n int) {
	println("failing", n)
	panic("fail")
}

//pu:noinline
func get() int { return 3 }

func main() {
	if n := get(); n > 2 {
		println(n)
		return
	}
	fail(1)
}
`)
	out, status := runPluto(t, dir, "run", "--inlinenotes", ".")
	want := filepath.Join(dir, "main.p") + ":17:2: note: call to main.fail marked //pu:inline was not inlined: it never returns\n3\n"
	if status != 0 || out != want {
		t.Errorf("exit status %d, output:\n%s\nwant 0 and:\n%s", status, out, want)
	}
}

// TestRunExitStatus checks that pluto run exits with the program's status.
func TestRunExitStatus(t *testing.T) {
	dir := writeProgram(t, "package main\n\nfunc main() int64 { return 3 }\n")
	out, status := runPluto(t, dir, "run", ".")
	if status != 3 || out != "" {
		t.Errorf("exit status %d, output %q; want 3 and no output", status, out)
	}
}
