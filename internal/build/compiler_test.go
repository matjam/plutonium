package build_test

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/syntax/token"
)

// testCompiler is the compiler binary named by PLUTO_TEST_COMPILER, such as
// the self-hosted compiler, as an absolute path, or "" to build with this
// package's Build in process. Every build of these tests goes through
// buildCfg, which runs that binary with the command-line flags that stand
// for the build.Config fields.
var testCompiler = func() string {
	c := os.Getenv("PLUTO_TEST_COMPILER")
	if c == "" {
		return ""
	}
	if abs, err := filepath.Abs(c); err == nil {
		return abs
	}
	return c
}()

// skipUnderTestCompiler skips a test of a Go API of this package that has
// no command-line form, when the tests run against PLUTO_TEST_COMPILER.
func skipUnderTestCompiler(t *testing.T, why string) {
	t.Helper()
	if testCompiler != "" {
		t.Skipf("Go API only (PLUTO_TEST_COMPILER is set): %s", why)
	}
}

// libRoot returns the absolute path of the repository's lib directory, the
// standard library the external compiler reads by default.
func libRoot(t testing.TB) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "lib"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// buildCfg builds cfg: with build.Build, or with the PLUTO_TEST_COMPILER
// binary if it is set.
func buildCfg(cfg *build.Config) error {
	if testCompiler == "" {
		return build.Build(cfg)
	}
	return buildExternal(cfg)
}

// defaultStdlibType is the type of plutonium.Stdlib(): a Config whose
// Stdlib has another type has a library of its own, such as an overlay,
// which the external compiler reads from a copy on disk.
var defaultStdlibType = reflect.TypeOf(plutonium.Stdlib())

// A cliError is a build the external compiler failed. Its Error is the
// first line of each diagnostic, as --brief prints them and as a
// diag.List's Error reads; full is everything it printed, explanations
// included.
type cliError struct {
	msg  string
	full string
}

func (e *cliError) Error() string { return e.msg }

// As makes the diagnostics available to errors.As as a *diag.List or the
// first *diag.Diagnostic, read back from the compiler's output: position,
// code and message.
func (e *cliError) As(target any) bool {
	switch p := target.(type) {
	case **diag.List:
		l := parseDiagnostics(e.full)
		if len(l) == 0 {
			return false
		}
		*p = &l
		return true
	case **diag.Diagnostic:
		l := parseDiagnostics(e.full)
		if len(l) == 0 {
			return false
		}
		*p = l[0]
		return true
	}
	return false
}

var (
	diagLine = regexp.MustCompile(`^(.+?):(\d+):(\d+): (.*)$`)
	diagCode = regexp.MustCompile(`run "pluto explain (E\d+)"`)
)

// parseDiagnostics reads diagnostics from pluto's output: an unindented
// line "file:line:col: message" starts one, and its indented explanation
// ends in a line naming its code.
func parseDiagnostics(out string) diag.List {
	var l diag.List
	var cur *diag.Diagnostic
	for _, line := range strings.Split(out, "\n") {
		switch {
		case line == "":
		case strings.HasPrefix(line, " "):
			if m := diagCode.FindStringSubmatch(line); m != nil && cur != nil && cur.Code == "" {
				cur.Code = diag.Code(m[1])
			}
		default:
			cur = &diag.Diagnostic{Msg: line}
			if m := diagLine.FindStringSubmatch(line); m != nil {
				ln, _ := strconv.Atoi(m[2])
				col, _ := strconv.Atoi(m[3])
				cur.Pos = token.Position{Filename: m[1], Line: ln, Column: col}
				cur.Msg = m[4]
			}
			l = append(l, cur)
		}
	}
	return l
}

// fullError returns the diagnostics in err as pluto prints them, with
// their explanations.
func fullError(err error) string {
	var ce *cliError
	if errors.As(err, &ce) {
		return ce.full
	}
	var b strings.Builder
	diag.Print(&b, err, false)
	return b.String()
}

// compilerArgs returns the arguments of pluto build for cfg, after the
// command's name and before the package directory.
func compilerArgs(cfg *build.Config, output string) ([]string, error) {
	args := []string{"-O" + strconv.Itoa(cfg.OptLevel), "-o", output}
	switch {
	case cfg.Kernel:
		args = append(args, "--mode", "kernel")
	case cfg.Freestanding:
		args = append(args, "--mode", "freestanding")
	}
	if len(cfg.Tags) > 0 {
		args = append(args, "--tags", strings.Join(cfg.Tags, ","))
	}
	if cfg.LinkerScript != "" {
		args = append(args, "--ldscript", cfg.LinkerScript)
	}
	if cfg.Format != "" {
		args = append(args, "--format", cfg.Format)
	}
	if cfg.SSADump != "" {
		args = append(args, "--ssa", cfg.SSADump)
	}
	if cfg.InlineNotes != nil {
		args = append(args, "--inlinenotes")
	}
	if cfg.Verbose {
		args = append(args, "-v")
	}
	if cfg.KeepWork {
		args = append(args, "--work")
	}
	if cfg.CompileOnly {
		args = append(args, "-c")
	}
	if cfg.NoDWARF {
		args = append(args, "-g=false")
	}
	if cfg.Strip {
		args = append(args, "-s")
	}
	if cfg.NoTrace {
		args = append(args, "--trace=false")
	}
	if cfg.NoTrimPath {
		args = append(args, "--trimpath=false")
	}
	if cfg.Alloc != "" {
		args = append(args, "--alloc", cfg.Alloc)
	}
	if cfg.GenCheck {
		args = append(args, "--gencheck=true")
	}
	if cfg.NoGenCheck {
		args = append(args, "--gencheck=false")
	}
	switch cfg.FieldNames {
	case "":
	case "on":
		args = append(args, "--fieldnames=true")
	case "off":
		args = append(args, "--fieldnames=false")
	default:
		return nil, fmt.Errorf("FieldNames %q has no command-line form", cfg.FieldNames)
	}
	return args, nil
}

// buildExternal builds cfg with the PLUTO_TEST_COMPILER binary: pluto
// build with the flags that stand for cfg's fields, in the test's working
// directory (so a relative Dir names the same directory), with PLUTOROOT
// naming cfg's standard library.
func buildExternal(cfg *build.Config) error {
	work, err := os.MkdirTemp("", "pluto-test-build-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(work) }()

	output := cfg.Output
	if output == "" {
		output = filepath.Join(work, "a.out")
	}
	args, err := compilerArgs(cfg, output)
	if err != nil {
		return err
	}
	listing := filepath.Join(work, "listing.s")
	if cfg.Listing != nil {
		args = append(args, "-S", listing)
	}
	dir := cfg.Dir
	if dir == "" {
		dir = "."
	}
	args = append([]string{"build"}, args...)
	args = append(args, dir)

	root := cfg.StdlibDir
	switch {
	case root != "":
	case cfg.Stdlib == nil || reflect.TypeOf(cfg.Stdlib) == defaultStdlibType:
		root, err = filepath.Abs(filepath.Join("..", "..", "lib"))
		if err != nil {
			return err
		}
	default:
		root = filepath.Join(work, "lib")
		if err := writeFS(cfg.Stdlib, root); err != nil {
			return fmt.Errorf("writing the build's standard library: %v", err)
		}
	}

	cmd := exec.Command(testCompiler, args...)
	cmd.Env = append(os.Environ(), "PLUTOROOT="+root, "PLUTO_BRIEF=0")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	if runErr == nil {
		if cfg.Listing != nil {
			data, err := os.ReadFile(listing)
			if err != nil {
				return err
			}
			if _, err := cfg.Listing.Write(data); err != nil {
				return err
			}
		}
		if cfg.InlineNotes != nil {
			if _, err := cfg.InlineNotes.Write(stderr.Bytes()); err != nil {
				return err
			}
		} else if stderr.Len() > 0 {
			_, _ = os.Stderr.Write(stderr.Bytes())
		}
		if stdout.Len() > 0 {
			_, _ = os.Stdout.Write(stdout.Bytes())
		}
		return nil
	}
	var exitErr *exec.ExitError
	if !errors.As(runErr, &exitErr) {
		return runErr
	}
	text := stderr.String()
	switch exitErr.ExitCode() {
	case 1:
		// A compile error, printed by diag.Print.
		return &cliError{msg: briefLines(text), full: text}
	case 2:
		// A flag the build rejects: "pluto build: message", followed by
		// the usage text if the flag parser rejected it. A runtime panic
		// of the compiler also exits with 2, and is reported whole.
		first, _, _ := strings.Cut(text, "\n")
		if msg, ok := strings.CutPrefix(first, "pluto build: "); ok {
			return &cliError{msg: msg, full: text}
		}
	}
	return fmt.Errorf("%s: %v\n%s%s", testCompiler, runErr, stdout.String(), text)
}

// briefLines returns what --brief prints of diag.Print's output: the
// lines that are not explanation (indented) or blank, without the last
// newline.
func briefLines(out string) string {
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		if line == "" || strings.HasPrefix(line, " ") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// writeFS writes the regular files of fsys under dir.
func writeFS(fsys fs.FS, dir string) error {
	return fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dir, filepath.FromSlash(path))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// runTestCompiler runs the PLUTO_TEST_COMPILER binary with args and the
// repository's standard library and returns what it printed and its exit
// status.
func runTestCompiler(t *testing.T, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(testCompiler, args...)
	cmd.Env = append(os.Environ(), "PLUTOROOT="+libRoot(t))
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	switch {
	case errors.As(err, &exitErr):
		return string(out), exitErr.ExitCode()
	case err != nil:
		t.Fatal(err)
	}
	return string(out), 0
}
