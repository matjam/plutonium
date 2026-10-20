// Package build drives a whole compilation: load, lower, optimise, generate
// code, assemble and link.
package build

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/matjam/plutonium/internal/backend"
	"github.com/matjam/plutonium/internal/debuginfo"
	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/escape"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/lower"
	"github.com/matjam/plutonium/internal/moves"
	"github.com/matjam/plutonium/internal/objfile"
	"github.com/matjam/plutonium/internal/opt"
	"github.com/matjam/plutonium/internal/race"
	"github.com/matjam/plutonium/internal/scoped"
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/types2"
)

// Config describes a build.
type Config struct {
	Dir          string // main package directory
	Output       string // output file
	Freestanding bool
	Kernel       bool     // --mode kernel: freestanding, ring 0, no FPU (implies Freestanding)
	Tags         []string // extra build tags
	OptLevel     int      // 0 = none, 1 = basic, 2 = full
	LinkerScript string
	Format       string    // "elf" (default) or "binary"
	Listing      io.Writer // if set, receives the machine code listing
	SSADump      string    // function name (or "all") whose SSA is dumped to stderr after each pass
	InlineNotes  io.Writer // if set, receives a note for each //pu:inline call left out of line (--inlinenotes)
	Stdlib       fs.FS
	Verbose      bool
	KeepWork     bool // keep the temporary directory
	CompileOnly  bool // write the object file to Output and stop

	// GenCheck asks for generation checks (abi.md section 1.3): the
	// allocators tag heap pointers and accesses check the tag. They are
	// the default of hosted builds; asking for them in a freestanding or
	// kernel build is an error.
	GenCheck bool
	// NoGenCheck builds a hosted program without generation checks: the
	// documented unchecked mode (pluto build --gencheck=false), in which a
	// use of freed heap memory is not detected.
	NoGenCheck bool
	// NoDWARF omits the DWARF debugging sections (pluto build -g=false).
	NoDWARF bool
	// Strip omits DWARF and the symbol table (pluto build -s). Panics still
	// print symbolised stack traces, which use a table of their own.
	Strip bool
	// NoTrace leaves the trace table empty (pluto build --trace=false), for
	// images that cannot spare its size: panics then print no stack trace.
	NoTrace bool
	// NoTrimPath records absolute source paths in the trace table and
	// DWARF (pluto build --trimpath=false). By default they record paths
	// relative to the module root and the standard library
	// (load.Program.TrimmedName), with "." as DWARF's compilation
	// directory, so that the output does not depend on where the source
	// is.
	NoTrimPath bool
	// StdlibDir, if set, is the directory Stdlib was read from; with
	// NoTrimPath, debugging information names standard library files under
	// it rather than $PLUTOROOT.
	StdlibDir string
	// Alloc selects the default allocator of a hosted program: "" for the
	// slab allocator, "debug" for the checking allocator in
	// pluto/alloc/debug (settings from $PLUTO_DEBUGALLOC at run time).
	Alloc string
	// FieldNames says whether type descriptors record struct field names,
	// which %+v and %#v print (pluto build --fieldnames): "" for the
	// default (on, except in kernel mode), "on" or "off".
	FieldNames string
}

// fieldNames reports whether the build keeps field names in type
// descriptors.
func (cfg *Config) fieldNames() bool {
	switch cfg.FieldNames {
	case "on":
		return true
	case "off":
		return false
	}
	return !cfg.Kernel
}

// Build compiles and links a program.
func Build(cfg *Config) error {
	lcfg, err := loadConfig(cfg)
	if err != nil {
		return err
	}
	prog, err := load.Load(lcfg, cfg.Dir)
	if err != nil {
		return err
	}
	return buildProgram(cfg, prog)
}

var sizes = types2.SizesFor("gc", "amd64")

// genCheckDefault is whether hosted builds that ask for neither GenCheck
// nor NoGenCheck get generation checks: yes, unless the PLUTO_GENCHECK
// environment variable is 0, which runs a whole test suite in the
// unchecked mode.
var genCheckDefault = os.Getenv("PLUTO_GENCHECK") != "0"

// genCheck reports whether a build uses the generation checks. Their tag
// layout assumes user-space addresses, so they are for hosted builds only
// (memory-safety design, known hole 9).
func (cfg *Config) genCheck() bool {
	switch {
	case cfg.Freestanding || cfg.Kernel:
		return false
	case cfg.GenCheck:
		return true
	case cfg.NoGenCheck:
		return false
	}
	return genCheckDefault
}

// Check reports a setting of cfg that no build accepts, such as an unknown
// allocator or one that needs a hosted build.
func (cfg *Config) Check() error {
	switch cfg.Alloc {
	case "":
	case "debug":
		if cfg.Freestanding || cfg.Kernel {
			return fmt.Errorf("--alloc=debug needs a hosted build; a freestanding program installs the debug allocator itself with alloc.SetDefault(debug.New(...))")
		}
	default:
		return fmt.Errorf("unknown allocator %q (supported: debug)", cfg.Alloc)
	}
	if cfg.GenCheck && (cfg.Freestanding || cfg.Kernel) {
		return fmt.Errorf("--gencheck is for hosted builds only: freestanding and kernel builds have no generation tags")
	}
	if cfg.GenCheck && cfg.NoGenCheck {
		return fmt.Errorf("GenCheck and NoGenCheck both set")
	}
	switch cfg.FieldNames {
	case "", "on", "off":
	default:
		return fmt.Errorf("unknown --fieldnames setting %q (supported: on, off)", cfg.FieldNames)
	}
	return nil
}

// loadConfig returns the package loader's configuration for a build.
func loadConfig(cfg *Config) (*load.Config, error) {
	if err := cfg.Check(); err != nil {
		return nil, err
	}
	tags := load.BuildTags(cfg.Freestanding, cfg.Kernel, cfg.Tags)
	if cfg.genCheck() {
		tags["gencheck"] = true
	}
	var preload []string
	if cfg.Alloc == "debug" {
		tags["allocdebug"] = true
		tags["allocsites"] = true
		preload = append(preload, "pluto/alloc/debug")
	}
	return &load.Config{Tags: tags, Stdlib: cfg.Stdlib, Sizes: sizes, Preload: preload}, nil
}

// buildProgram compiles and links a loaded program.
// lowerProgram runs the front end on a loaded program: the lifetime and
// race checks, then lowering to SSA.
func lowerProgram(cfg *Config, prog *load.Program) (*ssa.Program, error) {
	// The move checker's account of moves tells lowering where owners are
	// destroyed. Lowering reports the constructs Plutonium does not
	// support; the escape analysis assumes a program without them. The
	// errors of both lifetime checks are reported together. The move
	// checker uses what the escape analysis finds about calls, to follow
	// views of local owners (spec section 18.3).
	calls, escErr := escape.Analyze(prog)
	info, moveErr := moves.AnalyzeWith(prog, calls)
	// The race check runs before lowering, which makes the checks it
	// leaves to run time (spec section 19.4), but its errors are reported
	// after the lifetime errors, as the check assumes every view stays
	// within its lifetime.
	var overlaps scoped.Overlaps
	var raceErr error
	lifetimeErr := joinLifetimeErrors(moveErr, escErr)
	if lifetimeErr == nil {
		overlaps, raceErr = race.AnalyzeWith(prog, info)
	}
	sp, err := lower.Lower(&lower.Config{
		Overlaps:     overlaps,
		Freestanding: cfg.Freestanding || cfg.Kernel,
		Kernel:       cfg.Kernel,
		Sizes:        sizes,
		NoFieldNames: !cfg.fieldNames(),
		GenCheck:     cfg.genCheck(),
		Moves:        info,
	}, prog)
	if err != nil {
		return nil, err
	}
	if lifetimeErr != nil {
		return nil, lifetimeErr
	}
	// Data races, once every view is known to stay within its lifetime.
	if raceErr != nil {
		return nil, raceErr
	}
	return sp, nil
}

func buildProgram(cfg *Config, prog *load.Program) error {
	sp, err := lowerProgram(cfg, prog)
	if err != nil {
		return err
	}
	if err := optimize(cfg, sp, prog); err != nil {
		return internal(err)
	}

	obj, err := emitObject(cfg, sp, prog)
	if err != nil {
		return internal(err)
	}

	work, err := os.MkdirTemp("", "pluto-build-")
	if err != nil {
		return err
	}
	if cfg.KeepWork {
		fmt.Fprintf(os.Stderr, "work directory: %s\n", work)
	} else {
		defer func() { _ = os.RemoveAll(work) }()
	}

	mainObj := filepath.Join(work, "main.o")
	if cfg.CompileOnly {
		mainObj = cfg.Output
	}
	var buf bytes.Buffer
	if err := obj.Write(&buf); err != nil {
		return err
	}
	if err := os.WriteFile(mainObj, buf.Bytes(), 0o644); err != nil {
		return err
	}
	if cfg.CompileOnly {
		return nil
	}

	objs := []string{mainObj}
	for _, p := range prog.Packages {
		for i, a := range p.AsmFiles {
			// Assemble in the work directory by relative names: an
			// assembler may record the source's name, and the work
			// directory's differs on every build.
			src := fmt.Sprintf("%s_%d.s", strings.ReplaceAll(p.Path, "/", "_"), i)
			if err := os.WriteFile(filepath.Join(work, src), a.Src, 0o644); err != nil {
				return err
			}
			o := strings.TrimSuffix(src, ".s") + ".o"
			if err := runIn(cfg, work, map[string]string{src: a.Name}, "as", "--64", "-o", o, src); err != nil {
				return prefixed("assembling "+a.Name, err)
			}
			objs = append(objs, filepath.Join(work, o))
		}
		cobjs, err := compileC(cfg, work, p)
		if err != nil {
			return err
		}
		objs = append(objs, cobjs...)
	}

	// No build ID: whether ld adds one by default depends on how it was
	// configured, and the output must not depend on the machine.
	args := []string{"-static", "-nostdlib", "-z", "noexecstack", "--build-id=none", "-o", cfg.Output}
	switch {
	case cfg.Strip:
		args = append(args, "-s")
	case cfg.NoDWARF:
		// Assembly in the runtime carries call frame information for
		// debuggers; -g=false drops it with the rest.
		args = append(args, "-S")
	}
	switch {
	case cfg.LinkerScript != "":
		args = append(args, "-T", cfg.LinkerScript)
	case cfg.Kernel:
		script := filepath.Join(work, "kernel.ld")
		if err := os.WriteFile(script, []byte(KernelLinkerScript), 0o644); err != nil {
			return err
		}
		args = append(args, "-z", "max-page-size=0x1000", "-T", script)
	default:
		args = append(args, "-e", "_start")
	}
	if cfg.Format == "binary" {
		args = append(args, "--oformat=binary")
	}
	args = append(args, objs...)
	if err := run(cfg, nil, "ld", args...); err != nil {
		return err
	}
	if cfg.Kernel && cfg.Format != "binary" {
		return checkPerCPU(cfg.Output)
	}
	return nil
}

// compileC compiles a package's .c files with the system C compiler ($CC,
// or cc) and copies its .o files into the work directory, returning the
// objects to link. Programs link without libc, so C code is compiled
// freestanding and without a stack protector (which needs libc and TLS);
// freestanding builds also turn off the red zone, which an interrupt taken
// on the same stack would overwrite (abi.md section 10.1).
func compileC(cfg *Config, work string, p *load.Package) ([]string, error) {
	var objs []string
	prefix := strings.ReplaceAll(p.Path, "/", "_")
	for i, c := range p.CFiles {
		// Compiled in the work directory by relative names, like
		// assembly, so that no temporary path reaches the object.
		src := fmt.Sprintf("%s_c%d.c", prefix, i)
		if err := os.WriteFile(filepath.Join(work, src), c.Src, 0o644); err != nil {
			return nil, err
		}
		o := strings.TrimSuffix(src, ".c") + ".o"
		args := []string{"-c", "-O2", "-ffreestanding", "-fno-stack-protector"}
		if cfg.Freestanding {
			args = append(args, "-mno-red-zone")
		}
		if st, err := os.Stat(p.Dir); err == nil && st.IsDir() {
			args = append(args, "-I", p.Dir) // headers next to the source
		}
		args = append(args, "-o", o, src)
		cc := strings.Fields(os.Getenv("CC"))
		if len(cc) == 0 {
			cc = []string{"cc"}
		}
		if err := runIn(cfg, work, map[string]string{src: c.Name}, cc[0], append(cc[1:], args...)...); err != nil {
			return nil, prefixed("compiling "+c.Name, err)
		}
		objs = append(objs, filepath.Join(work, o))
	}
	for i, obj := range p.ObjFiles {
		o := filepath.Join(work, fmt.Sprintf("%s_o%d.o", prefix, i))
		if err := os.WriteFile(o, obj.Src, 0o644); err != nil {
			return nil, err
		}
		objs = append(objs, o)
	}
	return objs, nil
}

// run runs an external tool in the current directory, like runIn.
func run(cfg *Config, rename map[string]string, name string, args ...string) error {
	return runIn(cfg, "", rename, name, args...)
}

// runIn runs an external tool in directory dir ("" for the current one).
// If it fails, the error is a diagnostic that explains the common
// failures; rename maps the temporary file names the tool saw to the names
// the user knows, for its messages.
func runIn(cfg *Config, dir string, rename map[string]string, name string, args ...string) error {
	if cfg.Verbose {
		if dir != "" {
			fmt.Fprintf(os.Stderr, "cd %s && ", dir)
		}
		fmt.Fprintln(os.Stderr, name, strings.Join(args, " "))
	}
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = os.Stderr
	if err := cmd.Run(); err != nil {
		return diag.FromTool(name, errors.Is(err, exec.ErrNotFound), err.Error(), stderr.String(), rename)
	}
	return nil
}

// prefixed returns err with prefix and ": " before its message, keeping
// its code if it is a diagnostic.
func prefixed(prefix string, err error) error {
	var d *diag.Diagnostic
	if errors.As(err, &d) {
		c := *d
		c.Msg = prefix + ": " + d.Msg
		return &c
	}
	return fmt.Errorf("%s: %w", prefix, err)
}

// internal turns an error from the compiler's later stages, which all mean
// a bug in the compiler, into a diagnostic.
// joinLifetimeErrors returns the errors of the move checker and the escape
// analysis (language spec sections 18.3 and 18.5) as one list.
func joinLifetimeErrors(errs ...error) error {
	var all diag.List
	for _, err := range errs {
		if l, ok := err.(*diag.List); ok {
			all = append(all, *l...)
		} else if err != nil {
			return err
		}
	}
	return all.Err()
}

func internal(err error) error {
	var d *diag.Diagnostic
	if err == nil || errors.As(err, &d) {
		return err
	}
	return &diag.Diagnostic{Code: diag.InternalError, Msg: "internal compiler error: " + err.Error()}
}

// optimize runs the SSA pipeline over every function.
func optimize(cfg *Config, p *ssa.Program, prog *load.Program) error {
	pipeline := opt.ForLevel(cfg.OptLevel)
	if cfg.InlineNotes != nil {
		pipeline.ReportInlining()
	}
	err := pipeline.Run(p, func(pass string, f *ssa.Func) {
		if cfg.SSADump != "" && (cfg.SSADump == "all" || cfg.SSADump == f.Name) {
			fmt.Fprintf(os.Stderr, "--- after %s ---\n%s", pass, f)
		}
	})
	if err != nil || cfg.InlineNotes == nil {
		return err
	}
	return writeInlineNotes(cfg.InlineNotes, pipeline.InlineNotes(), prog)
}

// writeInlineNotes writes a note for each call to a //pu:inline function
// that was left out of line, and each func value of one, in the program's
// own packages: the standard library's are left out, as go build -m
// reports only the packages it is asked to build.
func writeInlineNotes(w io.Writer, notes []opt.InlineNote, prog *load.Program) error {
	for _, n := range notes {
		pos := prog.Fset.Position(n.Pos)
		if pos.Filename == "" || strings.HasPrefix(pos.Filename, load.StdPrefix) {
			continue
		}
		var err error
		if n.FuncValue {
			_, err = fmt.Fprintf(w, "%s: note: %s marked //pu:inline is used as a func value: %s\n", pos, n.Callee, n.Reason)
		} else {
			_, err = fmt.Fprintf(w, "%s: note: call to %s marked //pu:inline was not inlined: %s\n", pos, n.Callee, n.Reason)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// emitObject generates code for every function and lays out the data.
func emitObject(cfg *Config, p *ssa.Program, prog *load.Program) (*objfile.Object, error) {
	dbg := debuginfo.New(prog.Fset)
	switch {
	case !cfg.NoTrimPath:
		dbg.FileName = prog.TrimmedName
	case cfg.StdlibDir != "":
		dbg.FileName = func(name string) string {
			if rest, ok := strings.CutPrefix(name, load.StdPrefix); ok {
				return filepath.Join(cfg.StdlibDir, rest)
			}
			return name
		}
	}
	// Trimmed names are relative to the module root, so a debugger
	// run there finds the program's sources (debugging.md section 3).
	// They carry no compilation directory: ld would join "." to
	// ./main.p and report ././main.p.
	dir := ""
	if cfg.NoTrimPath && !cfg.NoDWARF && !cfg.Strip {
		var err error
		if dir, err = filepath.Abs(cfg.Dir); err != nil {
			return nil, err
		}
	}
	return backend.Emit(&backend.Config{Listing: cfg.Listing, NoTrace: cfg.NoTrace,
		NoDWARF: cfg.NoDWARF || cfg.Strip, CompDir: dir}, p, dbg)
}
