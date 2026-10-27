// Translated by go2pu from internal/build/test.go at the go-cutover tag; edited by hand since.

package build

import (
	"pluto/errors"
	"pluto/fmt"
	"pluto/strconv"
	"pluto/strings"
	"pluto/unicode"
	"pluto/unicode/utf8"
	"compiler/compat"
	"compiler/internal/diag"
	"compiler/internal/load"
	"compiler/internal/regexp"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
)

// ErrNoTestFiles is returned by BuildTest for a package without _test.p
// files.
var ErrNoTestFiles = errors.New("no test files")

// TestOptions choose what a test binary runs and how it reports.
type TestOptions struct {
	// Run selects tests, as go test -run does: a regular expression per
	// level of the test name, separated by slashes. Below the top level,
	// where the test binary does the matching, a pattern may only be
	// literal text with optional ^ and $ anchors.
	Run string
	// Bench selects benchmarks by a regular expression; empty runs none.
	Bench   string
	Verbose bool
	Count   int // times to run each test and benchmark; 0 means 1
}

// A TestBinary describes a test binary built by BuildTest.
type TestBinary struct {
	Path       string   // import path of the package under test
	Tests      []string // the tests it runs, in source order
	Benchmarks []string // the benchmarks it runs
}

// BuildTest builds a test binary into cfg.Output for the package target: a
// directory, or a standard library import path. The binary holds the
// package with its _test.p files and a generated main package that runs
// the selected tests and benchmarks through pluto/testing. When the package
// has no _test.p files it returns ErrNoTestFiles, with the package's path
// in the TestBinary.
func BuildTest(cfg *Config, target string, opts *TestOptions) (*TestBinary, error) {
	runTop, runSub, bench, err := opts.patterns()
	if err != nil {
		return nil, err
	}
	bin := pu.New(TestBinary{})
	lcfg, err := loadConfig(cfg)
	if err != nil {
		return nil, err
	}
	prog, err := load.LoadTest(lcfg, target, func(fset *token.FileSet, pkg *load.Package) ([]byte, error) {
		bin.Path = pkg.Path
		if len(pkg.Tests) == 0 {
			return nil, ErrNoTestFiles
		}
		tests, benchmarks := testFuncs(fset, pkg) or return
		for _, name := range tests {
			if runTop.MatchString(name) {
				bin.Tests = compat.Append(bin.Tests, name)
			}
		}
		if bench != nil {
			for _, name := range benchmarks {
				if bench.MatchString(name) {
					bin.Benchmarks = compat.Append(bin.Benchmarks, name)
				}
			}
		}
		return testMain(pkg.Path, bin, runSub, opts), nil
	})
	if err != nil {
		// bin.Path is set if the package itself loaded.
		return bin, err
	}
	return bin, buildProgram(cfg, prog)
}

// Check reports whether the -run and -bench patterns are valid.
func (o *TestOptions) Check() error {
	_, _, _, err := o.patterns()
	return err
}

// patterns compiles the -run and -bench patterns. bench is nil when no
// benchmarks are to run.
func (o *TestOptions) patterns() (runTop *regexp.Regexp, runSub []string, bench *regexp.Regexp, err error) {
	runTop, runSub, err = splitRunPattern(o.Run)
	if err != nil {
		return nil, nil, nil, err
	}
	if o.Bench != "" {
		if bench, err = regexp.Compile(o.Bench); err != nil {
			return nil, nil, nil, compat.Wrap(compat.Sprintfb("invalid -bench pattern: %w", compat.ArgW(err)), err)
		}
	}
	return runTop, runSub, bench, nil
}

// splitRunPattern splits a -run pattern into the top-level regular
// expression, which the driver applies, and the subtest patterns below it,
// which the test binary applies with a literal matcher.
func splitRunPattern(run string) (*regexp.Regexp, []string, error) {
	levels := splitLevels(run)
	top, err := regexp.Compile(levels[0])
	if err != nil {
		return nil, nil, compat.Wrap(compat.Sprintfb("invalid -run pattern: %w", compat.ArgW(err)), err)
	}
	sub := levels[1:]
	for _, p := range sub {
		lit := strings.TrimSuffix(strings.TrimPrefix(p, "^"), "$")
		if strings.ContainsAny(lit, `\.+*?()|[]{}^$`) {
			return nil, nil, compat.NewError(compat.Sprintfb("invalid -run pattern %q: below the top level, a pattern may only be literal text with optional ^ and $ anchors", compat.ArgS(p)))
		}
	}
	return top, sub, nil
}

// splitLevels splits a pattern on slashes that are not inside brackets or
// parentheses, as go test does.
func splitLevels(s string) []string {
	var levels []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '[', '(':
			depth++
		case ']', ')':
			depth--
		case '\\':
			i++
		case '/':
			if depth == 0 {
				levels = compat.Append(levels, strings.Slice(s, start, i))
				start = i + 1
			}
		}
	}
	return compat.Append(levels, strings.SliceFrom(s, start))
}

// testFuncs returns the tests and benchmarks declared in the package's
// _test.p files, in source order, after checking their signatures.
func testFuncs(fset *token.FileSet, pkg *load.Package) (tests, benchmarks []string, err error) {
	errs := pu.Alloc[diag.List]()
	for _, f := range pkg.Tests {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv != nil {
				continue
			}
			name := fd.Name.Name
			var param string
			switch {
			case isTestName(name, "Test"):
				param = "T"
				tests = compat.Append(tests, name)
			case isTestName(name, "Benchmark"):
				param = "B"
				benchmarks = compat.Append(benchmarks, name)
			default:
				continue
			}
			fn, _ := pkg.Info.Defs[fd.Name].(*types2.Func)
			if fn == nil || !isTestSignature(fn.Type().(*types2.Signature), param) {
				(*errs) = diag.List(compat.Append((*errs), diag.New(fset.Position(fd.Name.Pos()), diag.BadTestSignature, "wrong signature for %s, must be: func %s(%s *testing.%s)",
					pu.Box(name), pu.Box(name), pu.Box(strings.ToLower(param)), pu.Box(param))))
			}
		}
	}
	return tests, benchmarks, errs.Err()
}

// isTestName reports whether name is prefix followed by nothing or by a
// character that is not a lower-case letter, as go test requires.
func isTestName(name, prefix string) bool {
	rest, ok := strings.CutPrefix(name, prefix)
	if !ok {
		return false
	}
	if rest == "" {
		return true
	}
	r, _ := utf8.DecodeRuneInString(rest)
	return !unicode.IsLower(r)
}

// isTestSignature reports whether sig is func(*testing.<param>).
func isTestSignature(sig *types2.Signature, param string) bool {
	if sig.TypeParams().Len() != 0 || sig.Results().Len() != 0 || sig.Params().Len() != 1 {
		return false
	}
	ptr, ok := sig.Params().At(0).Type().(*types2.Pointer)
	if !ok {
		return false
	}
	named, ok := ptr.Elem().(*types2.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj.Pkg() != nil && obj.Pkg().Path() == "pluto/testing" && obj.Name() == param
}

// testMain generates the main package of a test binary.
func testMain(path string, bin *TestBinary, runSub []string, opts *TestOptions) []byte {
	count := max(opts.Count, 1)
	nameWidth := 0
	for _, name := range bin.Benchmarks {
		nameWidth = max(nameWidth, len(name))
	}
	b := pu.Alloc[compat.Buffer]()
	fmt.Fprintf(b, "// Code generated by pluto test. DO NOT EDIT.\n\n")
	// With nothing to run, the package under test is still linked in, so
	// that it is checked and its initialisation runs, as go test does.
	alias := "_test"
	if len(bin.Tests)+len(bin.Benchmarks) == 0 {
		alias = "_"
	}
	compat.Fprintfb(b, "package main\n\nimport (\n\t_testing \"pluto/testing\"\n\t%s %s\n)\n\n", compat.ArgS(alias), compat.ArgS(strconv.Quote(path)))
	fmt.Fprintf(b, "func main() int64 {\n")
	fmt.Fprintf(b, "\tvar m _testing.M\n")
	compat.Fprintfb(b, "\tm.Init(%s, %t, %d)\n", compat.ArgS(strconv.Quote(path)), compat.ArgB(opts.Verbose), compat.ArgI(int64(nameWidth)))
	for _, p := range runSub {
		compat.Fprintfb(b, "\tm.Filter(%s)\n", compat.ArgS(strconv.Quote(p)))
	}
	if len(bin.Tests) > 0 {
		compat.Fprintfb(b, "\tfor i := 0; i < %d; i++ {\n", compat.ArgI(int64(count)))
		for _, name := range bin.Tests {
			compat.Fprintfb(b, "\t\tm.Test(%s, _test.%s)\n", compat.ArgS(strconv.Quote(name)), compat.ArgS(name))
		}
		fmt.Fprintf(b, "\t}\n")
	}
	for _, name := range bin.Benchmarks {
		compat.Fprintfb(b, "\tfor i := 0; i < %d; i++ {\n", compat.ArgI(int64(count)))
		compat.Fprintfb(b, "\t\tm.Benchmark(%s, _test.%s)\n", compat.ArgS(strconv.Quote(name)), compat.ArgS(name))
		fmt.Fprintf(b, "\t}\n")
	}
	fmt.Fprintf(b, "\treturn m.Exit()\n}\n")
	return b.Bytes()
}
