// Ported by hand from cmd/pluto/fmt.go.

package main

import (
	"pluto/errors"
	"pluto/io"
	"pluto/os"
	"pluto/path/filepath"
	"pluto/strings"
	"compiler/compat"
	"compiler/internal/diff"
	"compiler/internal/syntax/format"
	"compiler/internal/syntax/parser"
	"compiler/internal/syntax/printer"
	"compiler/internal/syntax/scanner"
	"compiler/internal/syntax/token"
	"compiler/pu"
)

const fmtLong = `fmt formats Plutonium source the way gofmt formats Go.

With no paths, or the path "-", it formats standard input, which may be a
whole file or a list of declarations or statements. A directory is walked
for .p files. Without flags, formatted source goes to standard output.

The exit status is 2 if a file could not be read, parsed or written, 1 if
-d found a file whose formatting differs, and 0 otherwise.`

const fmtUsage = `Usage:
  pluto fmt [-l] [-w] [-d] [path ...]

Flags:
  -d, --diff    display diffs instead of rewriting files
  -h, --help    help for fmt
  -l, --list    list files whose formatting differs from pluto fmt's
  -w, --write   write the result to the source file instead of standard output
`

type fmtOptions struct {
	list, write, diff bool

	// harness is the hidden --harness flag's mode, for the tests that run
	// the printer and format goldens through this binary: "format" formats
	// as gofmt does but accepts go statements, as the format tests do, and
	// "printer", "printer-raw" and "printer-norm" print each file with the
	// printer tests' configurations. Each file's result goes to standard
	// output.
	harness string
}

// errFormattingDiffers marks a file that -d found unformatted.
var errFormattingDiffers = errors.New("formatting differs")

// fmtMain runs pluto fmt with the arguments after "fmt" and returns the
// exit status.
func fmtMain(args []string, stdout, stderr io.Writer) int {
	var opts fmtOptions
	var paths []string
	help := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			paths = compat.Append(paths, args[i+1:]...)
			i = len(args)
		case strings.HasPrefix(a, "--"):
			name, value, hasValue := strings.Cut(strings.SliceFrom(a, 2), "=")
			if name == "harness" {
				if !hasValue {
					if i+1 >= len(args) {
						return fmtUsageError(stderr, "flag needs an argument: --harness")
					}
					i++
					value = args[i]
				}
				opts.harness = value
				continue
			}
			p := fmtFlag(&opts, &help, name, "")
			if p == nil {
				return fmtUsageError(stderr, compat.Concat("unknown flag: --", name))
			}
			if !hasValue {
				value = "true"
			}
			b, ok := parseBool(value)
			if !ok {
				return fmtUsageError(stderr, compat.Sprintfb("invalid argument %q for %q flag: strconv.ParseBool: parsing %q: invalid syntax", compat.ArgS(value), compat.ArgS(fmtFlagTitle(name)), compat.ArgS(value)))
			}
			*p = b
		case strings.HasPrefix(a, "-") && len(a) > 1:
			shorts := strings.SliceFrom(a, 1)
			for len(shorts) > 0 {
				s, at := strings.Slice(shorts, 0, 1), shorts
				shorts = strings.SliceFrom(shorts, 1)
				p := fmtFlag(&opts, &help, "", s)
				if p == nil {
					return fmtUsageError(stderr, compat.Sprintfb("unknown shorthand flag: '%s' in -%s", compat.ArgS(s), compat.ArgS(at)))
				}
				value := "true"
				if strings.HasPrefix(shorts, "=") {
					value = strings.SliceFrom(shorts, 1)
					shorts = ""
				}
				b, ok := parseBool(value)
				if !ok {
					return fmtUsageError(stderr, compat.Sprintfb("invalid argument %q for %q flag: strconv.ParseBool: parsing %q: invalid syntax", compat.ArgS(value), compat.ArgS(fmtShortTitle(s)), compat.ArgS(value)))
				}
				*p = b
			}
		default:
			paths = compat.Append(paths, a)
		}
	}
	if help {
		say(stdout, "%s\n\n%s", fmtLong, fmtUsage)
		return 0
	}
	switch opts.harness {
	case "", "format", "printer", "printer-raw", "printer-norm":
	default:
		return fmtUsageError(stderr, compat.Concat("unknown harness mode ", opts.harness))
	}
	if opts.harness == "format" {
		format.AllowGoStmt()
	}
	return runFmt(opts, paths, os.Stdin, stdout, stderr)
}

// fmtFlag returns the variable of the flag with the long name name or the
// one-letter name short, or nil if there is none.
func fmtFlag(opts *fmtOptions, help *bool, name, short string) *bool {
	switch {
	case name == "list" || short == "l":
		return &opts.list
	case name == "write" || short == "w":
		return &opts.write
	case name == "diff" || short == "d":
		return &opts.diff
	case name == "help" || short == "h":
		return help
	}
	return nil
}

// fmtFlagTitle names the flag with the long name name as pflag's errors do.
func fmtFlagTitle(name string) string {
	return compat.Concat("-", strings.Slice(name, 0, 1), ", --", name)
}

// fmtShortTitle names the flag with the one-letter name s as pflag's
// errors do.
func fmtShortTitle(s string) string {
	switch s {
	case "l":
		return "-l, --list"
	case "w":
		return "-w, --write"
	case "d":
		return "-d, --diff"
	}
	return "-h, --help"
}

func fmtUsageError(stderr io.Writer, msg string) int {
	say(stderr, "pluto fmt: %s\n%s", pu.Box(msg), fmtUsage)
	return 2
}

// A fmtRun is the state of one run of pluto fmt.
type fmtRun struct {
	opts   fmtOptions
	stdout io.Writer
	stderr io.Writer
	status int
}

// report records err: a difference -d found, or an error to print.
func (r *fmtRun) report(err error) {
	if err == errFormattingDiffers {
		if r.status < 1 {
			r.status = 1
		}
		return
	}
	// scanner.PrintError, but printing strings: fmt.Fprintf of err
	// would keep it, as far as the escape analysis can tell.
	if list, ok := err.(*scanner.ErrorList); ok {
		for _, e := range *list {
			_, _ = compat.Fprintfb(r.stderr, "%s\n", compat.ArgS(e.Error()))
		}
	} else if err != nil {
		_, _ = compat.Fprintfb(r.stderr, "%s\n", compat.ArgS(err.Error()))
	}
	r.status = 2
}

// runFmt formats the given paths and returns the exit status.
func runFmt(opts fmtOptions, paths []string, stdin io.Reader, stdout, stderr io.Writer) int {
	r := pu.New(fmtRun{opts: opts, stdout: stdout, stderr: stderr})
	if len(paths) == 0 {
		paths = []string{"-"}
	}
	for _, path := range paths {
		if path == "-" {
			if opts.write {
				r.report(errors.New("cannot use -w with standard input"))
				continue
			}
			src, err := compat.ReadAll(stdin)
			if err != nil {
				r.report(err)
				continue
			}
			if err := r.source("<standard input>", src, true, 0); err != nil {
				r.report(err)
			}
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			r.report(err)
			continue
		}
		if !info.IsDir() {
			if err := r.file(path, info.Mode().Perm()); err != nil {
				r.report(err)
			}
			continue
		}
		err = filepath.WalkDir(path, func(p string, d *os.DirEntry, err error) error {
			p = compat.Concat(p) // a copy: p is a view the walk reuses
			if err != nil {
				r.report(err)
				return nil
			}
			if d.IsDir() || !isSourceFile(d.NameLossy()) {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				r.report(err)
				return nil
			}
			if err := r.file(p, info.Mode().Perm()); err != nil {
				r.report(err)
			}
			return nil
		})
		if err != nil {
			r.report(err)
		}
	}
	return r.status
}

func isSourceFile(name string) bool {
	return !strings.HasPrefix(name, ".") && strings.HasSuffix(name, ".p")
}

func (r *fmtRun) file(path string, perm os.FileMode) error {
	src := compat.ReadFile(path) or return
	return r.source(path, src, false, perm)
}

// source formats one file's source and acts on the result as gofmt does:
// list, write, diff, or print.
func (r *fmtRun) source(name string, src []byte, fragmentOk bool, perm os.FileMode) error {
	if r.opts.harness != "" {
		res := harnessFormat(r.opts.harness, name, src) or return
		_, err := r.stdout.Write(res)
		return err
	}
	res := format.File(name, src, fragmentOk) or return
	opts := r.opts
	if !bytesEqual(src, res) {
		if opts.list {
			if _, err := r.stdout.Write(compat.Bytes(compat.Concat(name, "\n"))); err != nil {
				return err
			}
		}
		if opts.write {
			if err := writeFormatted(name, res, perm); err != nil {
				return err
			}
		}
		if opts.diff {
			newName := filepath.ToSlash(name)
			if _, err := r.stdout.Write(diff.Diff(compat.Concat(newName, ".orig"), src, newName, res)); err != nil {
				return err
			}
			return errFormattingDiffers
		}
	}
	if !opts.list && !opts.write && !opts.diff {
		_, err := r.stdout.Write(res)
		return err
	}
	return nil
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// harnessFormat formats src in a --harness mode. The printer modes do what
// the printer tests' format function does: parse with go statements
// allowed, print with the test's configuration, and check that the result
// parses again.
func harnessFormat(mode, name string, src []byte) ([]byte, error) {
	if mode == "format" {
		return format.File(name, src, false)
	}
	const parseMode = parser.ParseComments | parser.SkipObjectResolution | parser.AllowGoStmt
	fset := token.NewFileSet()
	f := parser.ParseFile(fset, name, pu.Box(src), parseMode) or return
	cfg := printer.Config{Tabwidth: 8}
	switch mode {
	case "printer-raw":
		cfg.Mode |= printer.RawFormat
	case "printer-norm":
		cfg.Mode |= printer.Mode(1 << 30) // the printer's normalizeNumbers
	}
	buf := pu.Alloc[compat.Buffer]()
	cfg.Fprint(buf, fset, f) or return
	res := buf.Bytes()
	if _, err := parser.ParseFile(fset, name, pu.Box(res), parseMode); err != nil {
		return nil, compat.Wrap(compat.Concat("re-parse: ", err.Error()), err)
	}
	return res, nil
}

// writeFormatted replaces a file's contents through a temporary file in
// the same directory, so a failed write leaves the original intact.
func writeFormatted(name string, data []byte, perm os.FileMode) error {
	tmp, err := compat.CreateTemp(filepath.Dir(name), compat.Concat(filepath.Base(name), ".*.tmp"))
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(tmpName, perm)
	}
	if werr == nil {
		werr = os.Rename(tmpName, name)
	}
	if werr != nil {
		_ = os.Remove(tmpName)
	}
	return werr
}
