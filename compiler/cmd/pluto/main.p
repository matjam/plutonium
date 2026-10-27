// Translated by go2pu from tools/go2pu/testdata/pluto/main.go at the go-cutover tag; edited by hand since.

// Command pluto is the compiler's driver: pluto build, run, version,
// explain, vet, test, fmt, doc and lsp, with the flags the Go-hosted
// cmd/pluto had (tag go-cutover), parsed by a small parser of its own
// instead of cobra.
//
// stdlib.go (where the standard library is) and sys.go (running the built
// program, the listing file) are replaced by hand-written stdlib.p and sys.p
// in the translation.
package main

import (
	"compiler/compat"
	"compiler/internal/build"
	"compiler/internal/diag"
	"compiler/pu"
	"pluto/fmt"
	"pluto/io"
	"pluto/os"
	"pluto/path/filepath"
	"pluto/strconv"
	"pluto/strings"
)

const version = "0.1.0-dev"

// The analyses recurse with the nesting of the source and of its types.
//
//pu:stack 1G
func main() {
	stop := startProfiles()
	code := execute(compat.Args()[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// A flagKind is the type of a flag's value.
type flagKind int

const (
	boolFlag flagKind = iota
	stringFlag
	intFlag
	sliceFlag // a comma-separated list, repeatable
)

// A flagDef describes a flag of build and run.
type flagDef struct {
	name  string
	short string // one letter, or ""
	kind  flagKind
	def   string // the default value
}

// buildFlags are the flags of pluto build and pluto run (cmd/pluto,
// addBuildFlags), in the order of their usage text.
var buildFlags = []flagDef{
	{"alloc", "", stringFlag, ""},
	{"asm", "S", stringFlag, ""},
	{"brief", "", boolFlag, "false"},
	{"compile", "c", boolFlag, "false"},
	{"debug", "g", boolFlag, "true"},
	{"fieldnames", "", boolFlag, "true"},
	{"format", "", stringFlag, "elf"},
	{"gencheck", "", boolFlag, "true"},
	{"help", "h", boolFlag, "false"},
	{"inlinenotes", "", boolFlag, "false"},
	{"ldscript", "", stringFlag, ""},
	{"mode", "", stringFlag, ""},
	{"opt", "O", intFlag, "2"},
	{"output", "o", stringFlag, "a.out"},
	{"ssa", "", stringFlag, ""},
	{"strip", "s", boolFlag, "false"},
	{"tags", "", sliceFlag, ""},
	{"target", "", stringFlag, "x86_64-linux"},
	{"trace", "", boolFlag, "true"},
	{"trimpath", "", boolFlag, "true"},
	{"verbose", "v", boolFlag, "false"},
	{"work", "", boolFlag, "false"},
}

// buildFlagUsage is the flag list of cobra's usage text for build and run;
// %s is the command's name.
const buildFlagUsage = `Flags:
      --alloc string      default allocator: debug for the checking allocator (hosted builds; tune with $PLUTO_DEBUGALLOC)
  -S, --asm string        write a machine code listing to this file (- for stdout)
      --brief             print each error on one line, without its explanation (default from $PLUTO_BRIEF)
  -c, --compile           write the object file instead of linking
  -g, --debug             emit DWARF debugging information (-g=false to omit it) (default true)
      --fieldnames        record struct field names in type descriptors, for %%+v and %%#v (default true, false in kernel mode) (default true)
      --format string     output format: elf or binary (default "elf")
      --gencheck          check heap pointers' generations on every access, so a use of freed memory panics (hosted builds; --gencheck=false is the unchecked mode) (default true)
  -h, --help              help for %s
      --inlinenotes       print a note for each call to a //pu:inline function that was not inlined, and why
      --ldscript string   linker script (kernel mode has a default, see abi.md section 10.3)
      --mode string       build mode: hosted, freestanding or kernel (default from the target)
  -O, --opt int           optimization level: 0, 1 or 2 (default 2)
  -o, --output string     output file (default "a.out")
      --ssa string        dump the SSA of this function (or all) after each pass
  -s, --strip             omit DWARF and the symbol table (panic traces still work)
      --tags strings      additional build tags
      --target string     target triple: x86_64-linux or x86_64-none (default "x86_64-linux")
      --trace             emit the table panics use to print stack traces (--trace=false to omit it) (default true)
      --trimpath          record source paths relative to the module root and the standard library (--trimpath=false for absolute paths) (default true)
  -v, --verbose           print the assembler and linker commands
      --work              keep the temporary work directory
`

// rootUsage is the usage text of pluto itself.
const rootUsage = `Usage:
  pluto [command]

Available Commands:
  build       Compile and link a Plutonium program
  doc         Show package documentation and type ownership kinds
  explain     Explain a compile error code
  fmt         Format Plutonium source files
  help        Help about any command
  lsp         Run the language server on standard input and output
  run         Build a hosted program and run it
  test        Run the tests of Plutonium packages
  version     Print the pluto compiler version
  vet         List unsafe operations and their safe callers

Flags:
  -h, --help   help for pluto

Use "pluto [command] --help" for more information about a command.
`

const rootLong = "pluto is the compiler for the Plutonium kernel-safe systems language."

// A command is a parsed command line of build or run.
type command struct {
	name    string            // "build" or "run"
	values  map[string]string // flag values, by name
	changed map[string]bool   // the flags given on the command line
	tags    []string          // --tags
	args    []string          // positional arguments
	dashAt  int               // the number of positional arguments before "--", or -1
}

// usage returns cobra's usage text of the command.
func (c *command) usage() string {
	use := "pluto build [dir]"
	if c.name == "run" {
		use = "pluto run [dir] [-- args...]"
	}
	return compat.Concat("Usage:\n  ", use, " [flags]\n\n", compat.Sprintfb(buildFlagUsage, compat.ArgS(c.name)))
}

// short returns the command's one-line description.
func (c *command) short() string {
	if c.name == "run" {
		return "Build a hosted program and run it"
	}
	return "Compile and link a Plutonium program"
}

// A usageError is a command line the flag parser rejects: pluto prints it
// with the command's usage text.
type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

// A flagError is a flag value no build accepts, or flags that do not go
// together. pluto reports it with the command's name and without the usage
// text, as cmd/pluto does.
type flagError struct{ msg string }

func (e *flagError) Error() string { return e.msg }

func flagErrorf(format string, args ...any) error {
	return pu.New(flagError{msg: compat.Sprintf(format, args...)})
}

// lookup returns the flag named name, or with the one-letter name short.
func lookup(name, short string) (flagDef, bool) {
	for _, f := range buildFlags {
		if (name != "" && f.name == name) || (short != "" && f.short == short) {
			return f, true
		}
	}
	return flagDef{}, false
}

// flagTitle names a flag as pflag's errors do: "-O, --opt" or "--mode".
func flagTitle(f flagDef) string {
	if f.short != "" {
		return compat.Concat("-", f.short, ", --", f.name)
	}
	return compat.Concat("--", f.name)
}

// set records the value of flag f, checking that it parses as the flag's
// type.
func (c *command) set(f flagDef, value string) error {
	switch f.kind {
	case boolFlag:
		if _, ok := parseBool(value); !ok {
			return pu.New(usageError{msg: compat.Sprintfb("invalid argument %q for %q flag: strconv.ParseBool: parsing %q: invalid syntax", compat.ArgS(value), compat.ArgS(flagTitle(f)), compat.ArgS(value))})
		}
	case intFlag:
		if _, err := strconv.ParseInt(value, 0, 64); err != nil {
			return pu.New(usageError{msg: compat.Sprintfb("invalid argument %q for %q flag: %s", compat.ArgS(value), compat.ArgS(flagTitle(f)), compat.ArgS(err.Error()))})
		}
	case sliceFlag:
		if !c.changed[f.name] {
			c.tags = nil
		}
		if value != "" {
			c.tags = compat.Append(c.tags, compat.Split(value, ",")...)
		}
	}
	c.values[f.name] = value
	c.changed[f.name] = true
	return nil
}

// parse parses the arguments of build or run as pflag does: flags and
// positional arguments in any order, "--" ending the flags.
func parse(name string, args []string) (*command, error) {
	c := pu.New(command{name: name, values: map[string]string{}, changed: map[string]bool{}, dashAt: -1})
	for _, f := range buildFlags {
		c.values[f.name] = f.def
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			c.dashAt = len(c.args)
			c.args = compat.Append(c.args, args[i+1:]...)
			return c, nil
		case strings.HasPrefix(a, "--"):
			name, value, hasValue := strings.Cut(strings.SliceFrom(a, 2), "=")
			f, ok := lookup(name, "")
			if !ok {
				return c, pu.New(usageError{msg: compat.Concat("unknown flag: --", name)})
			}
			if !hasValue {
				if f.kind == boolFlag {
					value = "true"
				} else if i+1 < len(args) {
					i++
					value = args[i]
				} else {
					return c, pu.New(usageError{msg: compat.Concat("flag needs an argument: --", name)})
				}
			}
			if err := c.set(f, value); err != nil {
				return c, err
			}
		case strings.HasPrefix(a, "-") && len(a) > 1:
			shorts := strings.SliceFrom(a, 1)
			for len(shorts) > 0 {
				s, at := strings.Slice(shorts, 0, 1), shorts
				shorts = strings.SliceFrom(shorts, 1)
				f, ok := lookup("", s)
				if !ok {
					return c, pu.New(usageError{msg: compat.Sprintfb("unknown shorthand flag: '%s' in -%s", compat.ArgS(s), compat.ArgS(at))})
				}
				var value string
				switch {
				case strings.HasPrefix(shorts, "="):
					value = strings.SliceFrom(shorts, 1)
					shorts = ""
				case f.kind == boolFlag:
					value = "true"
				case shorts != "":
					value = shorts
					shorts = ""
				case i+1 < len(args):
					i++
					value = args[i]
				default:
					return c, pu.New(usageError{msg: compat.Sprintfb("flag needs an argument: '%s' in -%s", compat.ArgS(s), compat.ArgS(at))})
				}
				if err := c.set(f, value); err != nil {
					return c, err
				}
			}
		default:
			c.args = compat.Append(c.args, a)
		}
	}
	return c, nil
}

func (c *command) str(name string) string { return c.values[name] }

func (c *command) boolean(name string) bool {
	b, _ := parseBool(c.values[name])
	return b
}

// parseBool parses a boolean as strconv.ParseBool does.
func parseBool(s string) (value, ok bool) {
	switch s {
	case "1", "t", "T", "TRUE", "true", "True":
		return true, true
	case "0", "f", "F", "FALSE", "false", "False":
		return false, true
	}
	return false, false
}

func (c *command) integer(name string) int {
	n, _ := strconv.ParseInt(c.values[name], 0, 64)
	return int(n)
}

// envBrief reports whether $PLUTO_BRIEF asks for brief errors: set, and
// not 0 or false.
func envBrief() bool {
	switch strings.ToLower(compat.Getenv("PLUTO_BRIEF")) {
	case "", "0", "false", "no", "off":
		return false
	}
	return true
}

// execute runs pluto with the command-line arguments args and returns the
// exit status, as cmd/pluto's execute does: a command line the parser
// rejects prints the error and the command's usage, and a flag value the
// build rejects prints the error with the command's name; both exit with
// status 2. Any other error, such as a compile error, prints the error
// alone and exits with status 1.
func execute(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		say(stdout, "%s\n\n%s", rootLong, rootUsage)
		return 0
	}
	switch args[0] {
	case "version":
		say(stdout, "pluto version %s\ntarget: x86_64-linux\n", version)
		return 0
	case "doc":
		return docCommand(args[1:], stdout, stderr)
	case "explain":
		return explainCommand(args[1:], stdout, stderr)
	case "vet":
		return vetCommand(args[1:], stdout, stderr)
	case "test":
		return testCommand(args[1:], stdout, stderr)
	case "fmt":
		return fmtMain(args[1:], stdout, stderr)
	case "lsp":
		return lspMain(args[1:], stdout, stderr)
	case "build", "run":
	default:
		say(stderr, "pluto: unknown command %q for \"pluto\"\n%s", pu.Box(args[0]), rootUsage)
		return 2
	}
	c, err := parse(args[0], args[1:])
	path := compat.Concat("pluto ", c.name)
	if err == nil && c.boolean("help") {
		say(stdout, "%s\n\n%s", pu.Box(c.short()), pu.Box(c.usage()))
		return 0
	}
	if err == nil {
		err = checkArgs(c)
	}
	if err != nil {
		say(stderr, "%s: %s\n%s", pu.Box(path), pu.Box(err.Error()), pu.Box(c.usage()))
		return 2
	}
	brief := envBrief()
	if c.changed["brief"] {
		brief = c.boolean("brief")
	}
	if c.name == "run" {
		err = runCommand(c)
	} else {
		err = buildCommand(c)
	}
	if err == nil {
		return 0
	}
	if ferr, ok := err.(*flagError); ok {
		say(stderr, "%s: %s\n", pu.Box(path), pu.Box(ferr.msg))
		return 2
	}
	diag.Print(stderr, err, brief)
	return 1
}

// checkArgs checks the number of positional arguments: at most one, the
// program's directory (for run, before "--").
func checkArgs(c *command) error {
	n := len(c.args)
	if c.dashAt >= 0 {
		if c.name == "build" {
			n = len(c.args)
		} else {
			n = c.dashAt
		}
	}
	if n > 1 {
		return pu.New(usageError{msg: compat.Sprintfb("accepts at most 1 arg(s), received %d", compat.ArgI(int64(n)))})
	}
	return nil
}

func say(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

func buildCommand(c *command) error {
	cfg := buildConfig(c, c.args) or return
	defer closeListing(cfg)
	return build.Build(cfg)
}

func runCommand(c *command) error {
	dirArgs, progArgs := c.args, []string(nil)
	if c.dashAt >= 0 {
		dirArgs, progArgs = c.args[:c.dashAt], c.args[c.dashAt:]
	}
	cfg, err := buildConfig(c, dirArgs)
	if err != nil {
		return err
	}
	defer closeListing(cfg)
	if cfg.Kernel {
		return flagErrorf("cannot run a kernel; use pluto build --mode kernel")
	}
	tmp, err := os.MkdirTemp("", "pluto-run-")
	if err != nil {
		return err
	}
	cfg.Output = filepath.Join(tmp, "prog")
	err = build.Build(cfg)
	code := 0
	if err == nil {
		code, err = runProgram(cfg.Output, progArgs)
	}
	_ = os.RemoveAll(tmp)
	if err == nil && code != 0 {
		os.Exit(code)
	}
	return err
}

// buildConfig returns the build's configuration for the command line, as
// cmd/pluto's buildConfig does.
func buildConfig(c *command, args []string) (*build.Config, error) {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	target := c.str("target")
	mode := c.str("mode")
	format := c.str("format")
	genCheck := c.boolean("gencheck")
	// Unless asked, a build gets the mode's default: checks in hosted
	// builds, none in freestanding and kernel ones.
	askCheck, askNoCheck := c.changed["gencheck"] && genCheck, c.changed["gencheck"] && !genCheck
	fieldNames := "" // the mode's default
	if c.changed["fieldnames"] {
		fieldNames = "off"
		if c.boolean("fieldnames") {
			fieldNames = "on"
		}
	}

	arch, osName, ok := strings.Cut(target, "-")
	if !ok || arch != "x86_64" || (osName != "linux" && osName != "none") {
		return nil, flagErrorf("unsupported target %q (supported: x86_64-linux, x86_64-none)", pu.Box(target))
	}
	freestanding := osName == "none"
	kernel := false
	switch mode {
	case "":
	case "freestanding":
		freestanding = true
	case "kernel":
		freestanding = true
		kernel = true
	case "hosted":
		if osName == "none" {
			return nil, flagErrorf("hosted mode needs an operating system target")
		}
		freestanding = false
	default:
		return nil, flagErrorf("unknown mode %q (supported: hosted, freestanding, kernel)", pu.Box(mode))
	}
	if format != "elf" && format != "binary" {
		return nil, flagErrorf("unsupported format %q (supported: elf, binary)", pu.Box(format))
	}

	cfg := pu.New(build.Config{
		Dir:          dir,
		Output:       c.str("output"),
		Freestanding: freestanding,
		Kernel:       kernel,
		Tags:         c.tags,
		OptLevel:     c.integer("opt"),
		LinkerScript: c.str("ldscript"),
		Format:       format,
		SSADump:      c.str("ssa"),
		Stdlib:       stdlib(),
		Verbose:      c.boolean("verbose"),
		KeepWork:     c.boolean("work"),
		CompileOnly:  c.boolean("compile"),
		NoDWARF:      !c.boolean("debug"),
		Strip:        c.boolean("strip"),
		NoTrace:      !c.boolean("trace"),
		NoTrimPath:   !c.boolean("trimpath"),
		Alloc:        c.str("alloc"),
		GenCheck:     askCheck,
		NoGenCheck:   askNoCheck,
		FieldNames:   fieldNames,
	})
	if c.boolean("inlinenotes") {
		cfg.InlineNotes = os.Stderr
	}
	cfg.StdlibDir = stdlibDir()
	if err := cfg.Check(); err != nil {
		return nil, pu.New(flagError{msg: err.Error()})
	}
	switch asm := c.str("asm"); asm {
	case "":
	case "-":
		cfg.Listing = os.Stdout
	default:
		w := createListing(asm) or return
		cfg.Listing = w
	}
	return cfg, nil
}
