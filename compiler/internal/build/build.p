// Translated by go2pu from internal/build/build.go at the go-cutover tag; edited by hand since.

// Package build drives a whole compilation: load, lower, optimise, generate
// code, assemble and link.
package build

import (
	"compiler/compat"
	"compiler/compat/fs"
	"compiler/internal/backend"
	"compiler/internal/debuginfo"
	"compiler/internal/diag"
	"compiler/internal/load"
	"compiler/internal/lower"
	"compiler/internal/moves"
	"compiler/internal/objfile"
	"compiler/internal/opt"
	"compiler/internal/race"
	"compiler/internal/scoped"
	"compiler/internal/scratch"
	"compiler/internal/ssa"
	"compiler/internal/types2"
	"compiler/pu"
	"pluto/errors"
	"pluto/fmt"
	"pluto/io"
	"pluto/os"
	"pluto/path/filepath"
	"pluto/strings"
	"unsafe"
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
	// the default of every build. A checked freestanding or kernel program
	// exports the source of the runtime's metadata memory (E2588).
	GenCheck bool
	// NoGenCheck builds a program without generation checks: the
	// documented unchecked mode (pluto build --gencheck=false), in which a
	// use of freed heap memory is not detected (spec section 18.11).
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
	phase("load")
	prog, err := load.Load(lcfg, cfg.Dir)
	if err != nil {
		return err
	}
	return buildProgram(cfg, prog)
}

var sizes = types2.SizesFor("gc", "amd64")

// genCheckDefault is whether builds that ask for neither GenCheck nor
// NoGenCheck get generation checks: yes, unless the PLUTO_GENCHECK
// environment variable is 0, which runs a whole test suite in the
// unchecked mode.
var genCheckDefault = compat.Getenv("PLUTO_GENCHECK") != "0"

// genCheck reports whether a build uses the generation checks.
func (cfg *Config) genCheck() bool {
	switch {
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
		return compat.NewError(compat.Sprintfb("unknown allocator %q (supported: debug)", compat.ArgS(cfg.Alloc)))
	}
	if cfg.GenCheck && cfg.NoGenCheck {
		return fmt.Errorf("GenCheck and NoGenCheck both set")
	}
	switch cfg.FieldNames {
	case "", "on", "off":
	default:
		return compat.NewError(compat.Sprintfb("unknown --fieldnames setting %q (supported: on, off)", compat.ArgS(cfg.FieldNames)))
	}
	return nil
}

// loadConfig returns the package loader's configuration for a build.
func loadConfig(cfg *Config) (*load.Config, error) {
	cfg.Check() or return
	tags := load.BuildTags(cfg.Freestanding, cfg.Kernel, cfg.Tags)
	if cfg.genCheck() {
		tags["gencheck"] = true
	}
	var preload []string
	if cfg.Alloc == "debug" {
		tags["allocdebug"] = true
		tags["allocsites"] = true
		preload = compat.Append(preload, "pluto/alloc/debug")
	}
	return pu.New(load.Config{Tags: tags, Stdlib: cfg.Stdlib, Sizes: sizes, Preload: preload}), nil
}

// lowerProgram runs the front end on a loaded program: the lifetime and
// race checks, then lowering to SSA.
func lowerProgram(cfg *Config, prog *load.Program) (*ssa.Program, error) {
	// The move checker's account of moves tells lowering where owners are
	// destroyed. Lowering reports the constructs Plutonium does not
	// support; the escape analysis assumes a program without them. The
	// errors of both lifetime checks are reported together. The move
	// checker uses what the escape analysis finds about calls, to follow
	// views of local owners (spec section 18.3).
	phase("escape")
	calls, escErr := runEscapes(prog)
	phase("moves")
	info, moveErr := moves.AnalyzeWith(prog, calls)
	// The race check runs before lowering, which makes the checks it
	// leaves to run time (spec section 19.4), but its errors are reported
	// after the lifetime errors, as the check assumes every view stays
	// within its lifetime.
	var overlaps scoped.Overlaps
	var raceErr error
	lifetimeErr := joinLifetimeErrors(moveErr, escErr)
	if lifetimeErr == nil {
		phase("race")
		overlaps, raceErr = race.AnalyzeWith(prog, info)
	}
	phase("lower")
	sp, lowerErr := runLower(pu.New(lower.Config{
		Overlaps:     overlaps,
		Freestanding: cfg.Freestanding || cfg.Kernel,
		Kernel:       cfg.Kernel,
		Sizes:        sizes,
		NoFieldNames: !cfg.fieldNames(),
		GenCheck:     cfg.genCheck(),
		Moves:        info,
	}), prog)
	if lowerErr != nil {
		// The move errors come first. Lowering can fail on what a move
		// error already explains: x[k] on a container of owners (spec
		// section 11.11) also asks the library for a copy of an owner,
		// which lowering reports as E3306 at the library's line. So with
		// move errors, the copy errors of lowering are left out. The
		// escape errors are dropped, as that analysis assumes a program
		// that lowers.
		if moveErr != nil {
			return nil, joinLifetimeErrors(moveErr, withoutCode(lowerErr, diag.CopyOwners))
		}
		return nil, lowerErr
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

// lowerStep runs lowering as a scratch step: the lowerer's tables and
// what it built and dropped die with it, and only the lowered program and
// its errors are kept. Each lowered function's code is kept in memory of
// its own, which the optimiser's first step on it frees.
type lowerStep struct {
	cfg  *lower.Config
	prog *load.Program
	out  *ssa.Program
	pc   *ssa.ProgramCopier
	err  error
}

func (s *lowerStep) ScratchWork() {
	s.out, s.err = lower.Lower(s.cfg, s.prog)
	if s.err == nil {
		s.pc = ssa.NewProgramCopier(s.out)
	}
}

func (s *lowerStep) ScratchKeep() {
	if s.err != nil {
		s.out, s.err = nil, keepError(s.err)
		return
	}
	s.out = s.pc.Program()
	for i, f := range s.out.Funcs {
		scratch.BeginHome(unsafe.Pointer(f))
		s.pc.Func(i)
		scratch.EndHome()
	}
}

// runLower is lower.Lower in a scratch step of its own.
func runLower(cfg *lower.Config, prog *load.Program) (*ssa.Program, error) {
	s := pu.New(lowerStep{cfg: cfg, prog: prog})
	scratch.Do(s)
	scratch.Trim()
	return s.out, s.err
}

// buildProgram compiles and links a loaded program.
func buildProgram(cfg *Config, prog *load.Program) error {
	defer endPhase()
	sp, err := lowerProgram(cfg, prog)
	if err != nil {
		return err
	}
	phase("opt")
	if err := optimize(cfg, sp, prog); err != nil {
		return internal(err)
	}

	phase("emit")
	obj, err := emitObject(cfg, sp, prog)
	if err != nil {
		return internal(err)
	}

	work, err := os.MkdirTemp("", "pluto-build-")
	if err != nil {
		return err
	}
	if cfg.KeepWork {
		compat.Fprintfb(os.Stderr, "work directory: %s\n", compat.ArgS(work))
	} else {
		defer func() { _ = os.RemoveAll(work) }()
	}

	phase("write")
	mainObj := filepath.Join(work, "main.o")
	if cfg.CompileOnly {
		mainObj = cfg.Output
	}
	buf := pu.Alloc[compat.Buffer]()
	obj.Write(buf) or return
	os.WriteFile(mainObj, buf.Bytes(), 0o644) or return
	if cfg.CompileOnly {
		return nil
	}

	phase("link")
	objs := []string{mainObj}
	for _, p := range prog.Packages {
		for i, a := range p.AsmFiles {
			// Assemble in the work directory by relative names: an
			// assembler may record the source's name, and the work
			// directory's differs on every build.
			src := compat.Sprintfb("%s_%d.s", compat.ArgS(strings.ReplaceAll(p.Path, "/", "_")), compat.ArgI(int64(i)))
			os.WriteFile(filepath.Join(work, src), a.Src, 0o644) or return
			o := compat.Concat(strings.TrimSuffix(src, ".s"), ".o")
			if err := runIn(cfg, work, map[string]string{src: a.Name}, "as", "--64", "-o", o, src); err != nil {
				return prefixed(compat.Concat("assembling ", a.Name), err)
			}
			objs = compat.Append(objs, filepath.Join(work, o))
		}
		cobjs := compileC(cfg, work, p) or return
		objs = compat.Append(objs, cobjs...)
	}

	// No build ID: whether ld adds one by default depends on how it was
	// configured, and the output must not depend on the machine.
	args := []string{"-static", "-nostdlib", "-z", "noexecstack", "--build-id=none", "-o", cfg.Output}
	switch {
	case cfg.Strip:
		args = compat.Append(args, "-s")
	case cfg.NoDWARF:
		// Assembly in the runtime carries call frame information for
		// debuggers; -g=false drops it with the rest.
		args = compat.Append(args, "-S")
	}
	switch {
	case cfg.LinkerScript != "":
		args = compat.Append(args, "-T", cfg.LinkerScript)
	case cfg.Kernel:
		script := filepath.Join(work, "kernel.ld")
		os.WriteFile(script, compat.Bytes(KernelLinkerScript), 0o644) or return
		args = compat.Append(args, "-z", "max-page-size=0x1000", "-T", script)
	default:
		args = compat.Append(args, "-e", "_start")
	}
	if cfg.Format == "binary" {
		args = compat.Append(args, "--oformat=binary")
	}
	args = compat.Append(args, objs...)
	run(cfg, nil, "ld", args...) or return
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
		src := compat.Sprintfb("%s_c%d.c", compat.ArgS(prefix), compat.ArgI(int64(i)))
		os.WriteFile(filepath.Join(work, src), c.Src, 0o644) or return
		o := compat.Concat(strings.TrimSuffix(src, ".c"), ".o")
		args := []string{"-c", "-O2", "-ffreestanding", "-fno-stack-protector"}
		if cfg.Freestanding {
			args = compat.Append(args, "-mno-red-zone")
		}
		if st, err := fs.OSStat(p.Dir); err == nil && st.IsDir() {
			args = compat.Append(args, "-I", p.Dir) // headers next to the source
		}
		args = compat.Append(args, "-o", o, src)
		cc := compat.Fields(compat.Getenv("CC"))
		if len(cc) == 0 {
			cc = []string{"cc"}
		}
		if err := runIn(cfg, work, map[string]string{src: c.Name}, cc[0], compat.Append(cc[1:], args...)...); err != nil {
			return nil, prefixed(compat.Concat("compiling ", c.Name), err)
		}
		objs = compat.Append(objs, filepath.Join(work, o))
	}
	for i, obj := range p.ObjFiles {
		o := filepath.Join(work, compat.Sprintfb("%s_o%d.o", compat.ArgS(prefix), compat.ArgI(int64(i))))
		os.WriteFile(o, obj.Src, 0o644) or return
		objs = compat.Append(objs, o)
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
			compat.Fprintfb(os.Stderr, "cd %s && ", compat.ArgS(dir))
		}
		compat.Fprintfb(os.Stderr, "%v %v\n", compat.ArgS(name), compat.ArgS(compat.Join(args, " ")))
	}
	if r := runTool(dir, name, args); r.failed {
		return diag.FromTool(name, r.notFound, r.cause, r.stderr, rename)
	}
	return nil
}

// prefixed returns err with prefix and ": " before its message, keeping
// its code if it is a diagnostic.
func prefixed(prefix string, err error) error {
	d := pu.Alloc[*diag.Diagnostic]()
	if errors.As(err, d) {
		c := pu.New(*(*d))
		c.Msg = compat.Concat(prefix, ": ", (*d).Msg)
		return c
	}
	return compat.Wrap(compat.Sprintfb("%s: %w", compat.ArgS(prefix), compat.ArgW(err)), err)
}

// internal turns an error from the compiler's later stages, which all mean
// a bug in the compiler, into a diagnostic.
// joinLifetimeErrors returns the errors of the move checker and the escape
// analysis (language spec sections 18.3 and 18.5) as one list.
func joinLifetimeErrors(errs ...error) error {
	all := pu.Alloc[diag.List]()
	for _, err := range errs {
		if l, ok := err.(*diag.List); ok {
			(*all) = diag.List(compat.Append((*all), *l...))
		} else if err != nil {
			return err
		}
	}
	return all.Err()
}

// withoutCode returns err without its diagnostics of the given code, or
// err itself when it is not a list of diagnostics.
func withoutCode(err error, code diag.Code) error {
	l, ok := err.(*diag.List)
	if !ok {
		return err
	}
	rest := pu.Alloc[diag.List]()
	for _, d := range *l {
		if d.Code != code {
			(*rest) = diag.List(compat.Append((*rest), d))
		}
	}
	return rest.Err()
}

func internal(err error) error {
	if _, ok := errors.AsType[*diag.Diagnostic](err); err == nil || ok {
		return err
	}
	return pu.New(diag.Diagnostic{Code: diag.InternalError, Msg: compat.Concat("internal compiler error: ", err.Error())})
}

// optimize runs the SSA pipeline over every function.
func optimize(cfg *Config, p *ssa.Program, prog *load.Program) error {
	pipeline := opt.ForLevel(cfg.OptLevel)
	if phases {
		pipeline.OnStep = func(name string) {
			compat.Fprintfb(os.Stderr, "%v %v\n", compat.ArgS("pluto: opt step"), compat.ArgS(name))
		}
		pipeline.OnTime = passTime
	}
	if cfg.InlineNotes != nil {
		pipeline.ReportInlining()
	}
	// Without a dump the function passes run in parallel.
	var after func(pass string, f *ssa.Func)
	if cfg.SSADump != "" {
		after = func(pass string, f *ssa.Func) {
			if cfg.SSADump == "all" || cfg.SSADump == f.Name {
				fmt.Fprintf(os.Stderr, "--- after %s ---\n%s", pass, f)
			}
		}
	}
	err := pipeline.Run(p, after)
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

// trimmedNames names files in the debugging information as
// load.Program.TrimmedName does: relative to the module root and the
// standard library.
type trimmedNames struct{ prog *load.Program }

func (n trimmedNames) FileName(name string) string { return n.prog.TrimmedName(name) }

// stdlibNames names a standard library file under cfg.StdlibDir, for the
// debugging information of a build with NoTrimPath; other names are
// unchanged.
type stdlibNames struct{ cfg *Config }

func (n stdlibNames) FileName(name string) string {
	if rest, ok := strings.CutPrefix(name, load.StdPrefix); ok {
		return filepath.Join(n.cfg.StdlibDir, rest)
	}
	return name
}

// emitObject generates code for every function and lays out the data.
func emitObject(cfg *Config, p *ssa.Program, prog *load.Program) (*objfile.Object, error) {
	dbg := debuginfo.New(prog.Fset)
	switch {
	case !cfg.NoTrimPath:
		dbg.Names = trimmedNames{prog}
	case cfg.StdlibDir != "":
		dbg.Names = stdlibNames{cfg}
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
	return backend.Emit(pu.New(backend.Config{Listing: cfg.Listing, NoTrace: cfg.NoTrace,
		NoDWARF: cfg.NoDWARF || cfg.Strip, CompDir: dir}), p, dbg)
}
