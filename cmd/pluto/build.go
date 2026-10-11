package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
)

var buildCmd = &cobra.Command{
	Use:   "build [dir]",
	Short: "Compile and link a Plutonium program",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := buildConfig(cmd, args)
		if err != nil {
			return err
		}
		return build.Build(cfg)
	},
}

var runCmd = &cobra.Command{
	Use:   "run [dir] [-- args...]",
	Short: "Build a hosted program and run it",
	Args: func(cmd *cobra.Command, args []string) error {
		if n := cmd.ArgsLenAtDash(); n >= 0 {
			args = args[:n]
		}
		return cobra.MaximumNArgs(1)(cmd, args)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		var progArgs []string
		if n := cmd.ArgsLenAtDash(); n >= 0 {
			progArgs = args[n:]
			args = args[:n]
		}
		cfg, err := buildConfig(cmd, args)
		if err != nil {
			return err
		}
		if cfg.Kernel {
			return flagErrorf("cannot run a kernel; use pluto build --mode kernel")
		}
		tmp, err := os.MkdirTemp("", "pluto-run-")
		if err != nil {
			return err
		}
		code, err := buildAndRun(cfg, tmp, progArgs)
		_ = os.RemoveAll(tmp)
		if err == nil && code != 0 {
			os.Exit(code)
		}
		return err
	},
}

// buildAndRun builds the program in directory tmp and runs it, returning
// its exit status.
func buildAndRun(cfg *build.Config, tmp string, args []string) (int, error) {
	cfg.Output = filepath.Join(tmp, "prog")
	if err := build.Build(cfg); err != nil {
		return 0, err
	}
	c := exec.Command(cfg.Output, args...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := c.Run()
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), nil
	}
	return 0, err
}

// A flagError is a flag value no build accepts, or flags that do not go
// together. pluto reports it with the command's name and without the usage
// text: the message names what is accepted, which the flag list would not.
type flagError struct{ error }

func flagErrorf(format string, args ...any) error {
	return flagError{fmt.Errorf(format, args...)}
}

func init() {
	for _, c := range []*cobra.Command{buildCmd, runCmd} {
		addBuildFlags(c)
	}
	rootCmd.AddCommand(buildCmd, runCmd)
}

// addBuildFlags gives c the flags of pluto build.
func addBuildFlags(c *cobra.Command) {
	c.Flags().StringP("output", "o", "a.out", "output file")
	c.Flags().String("target", "x86_64-linux", "target triple: x86_64-linux or x86_64-none")
	c.Flags().String("mode", "", "build mode: hosted, freestanding or kernel (default from the target)")
	c.Flags().StringSlice("tags", nil, "additional build tags")
	c.Flags().String("ldscript", "", "linker script (kernel mode has a default, see abi.md section 10.3)")
	c.Flags().String("format", "elf", "output format: elf or binary")
	c.Flags().BoolP("verbose", "v", false, "print the assembler and linker commands")
	c.Flags().IntP("opt", "O", 2, "optimisation level: 0, 1 or 2")
	c.Flags().StringP("asm", "S", "", "write a machine code listing to this file (- for stdout)")
	c.Flags().String("ssa", "", "dump the SSA of this function (or all) after each pass")
	c.Flags().Bool("inlinenotes", false, "print a note for each call to a //pu:inline function that was not inlined, and why")
	c.Flags().Bool("work", false, "keep the temporary work directory")
	c.Flags().BoolP("compile", "c", false, "write the object file instead of linking")
	c.Flags().BoolP("debug", "g", true, "emit DWARF debugging information (-g=false to omit it)")
	c.Flags().BoolP("strip", "s", false, "omit DWARF and the symbol table (panic traces still work)")
	c.Flags().Bool("trace", true, "emit the table panics use to print stack traces (--trace=false to omit it)")
	c.Flags().Bool("trimpath", true, "record source paths relative to the module root and the standard library (--trimpath=false for absolute paths)")
	c.Flags().String("alloc", "", "default allocator: debug for the checking allocator (hosted builds; tune with $PLUTO_DEBUGALLOC)")
	c.Flags().Bool("gencheck", true, "check heap pointers' generations on every access, so a use of freed memory panics (hosted builds; --gencheck=false is the unchecked mode)")
	c.Flags().Bool("fieldnames", true, "record struct field names in type descriptors, for %+v and %#v (default true, false in kernel mode)")
	c.Flags().Bool("brief", false, briefUsage)
}

func buildConfig(cmd *cobra.Command, args []string) (*build.Config, error) {
	f := cmd.Flags()
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	output, _ := f.GetString("output")
	target, _ := f.GetString("target")
	mode, _ := f.GetString("mode")
	tags, _ := f.GetStringSlice("tags")
	ldscript, _ := f.GetString("ldscript")
	format, _ := f.GetString("format")
	verbose, _ := f.GetBool("verbose")
	optLevel, _ := f.GetInt("opt")
	asm, _ := f.GetString("asm")
	ssaDump, _ := f.GetString("ssa")
	inlineNotes, _ := f.GetBool("inlinenotes")
	work, _ := f.GetBool("work")
	compileOnly, _ := f.GetBool("compile")
	debug, _ := f.GetBool("debug")
	strip, _ := f.GetBool("strip")
	trace, _ := f.GetBool("trace")
	trimpath, _ := f.GetBool("trimpath")
	allocator, _ := f.GetString("alloc")
	genCheck, _ := f.GetBool("gencheck")
	// Unless asked, a build gets the mode's default: checks in hosted
	// builds, none in freestanding and kernel ones.
	askCheck, askNoCheck := f.Changed("gencheck") && genCheck, f.Changed("gencheck") && !genCheck
	fieldNames := "" // the mode's default
	if f.Changed("fieldnames") {
		fieldNames = "off"
		if on, _ := f.GetBool("fieldnames"); on {
			fieldNames = "on"
		}
	}

	arch, osName, ok := strings.Cut(target, "-")
	if !ok || arch != "x86_64" || (osName != "linux" && osName != "none") {
		return nil, flagErrorf("unsupported target %q (supported: x86_64-linux, x86_64-none)", target)
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
		return nil, flagErrorf("unknown mode %q (supported: hosted, freestanding, kernel)", mode)
	}
	if format != "elf" && format != "binary" {
		return nil, flagErrorf("unsupported format %q (supported: elf, binary)", format)
	}

	cfg := &build.Config{
		Dir:          dir,
		Output:       output,
		Freestanding: freestanding,
		Kernel:       kernel,
		Tags:         tags,
		OptLevel:     optLevel,
		LinkerScript: ldscript,
		Format:       format,
		SSADump:      ssaDump,
		Stdlib:       stdlib(),
		Verbose:      verbose,
		KeepWork:     work,
		CompileOnly:  compileOnly,
		NoDWARF:      !debug,
		Strip:        strip,
		NoTrace:      !trace,
		NoTrimPath:   !trimpath,
		Alloc:        allocator,
		GenCheck:     askCheck,
		NoGenCheck:   askNoCheck,
		FieldNames:   fieldNames,
	}
	if inlineNotes {
		cfg.InlineNotes = os.Stderr
	}
	if root := os.Getenv("PLUTOROOT"); root != "" {
		if abs, err := filepath.Abs(root); err == nil {
			cfg.StdlibDir = abs
		}
	}
	if err := cfg.Check(); err != nil {
		return nil, flagError{err}
	}
	switch asm {
	case "":
	case "-":
		cfg.Listing = os.Stdout
	default:
		w, err := os.Create(asm)
		if err != nil {
			return nil, err
		}
		cfg.Listing = w
		cobra.OnFinalize(func() { _ = w.Close() })
	}
	return cfg, nil
}

// stdlib returns the standard library: $PLUTOROOT if set (a directory
// containing pluto/), otherwise the copy embedded in the compiler.
func stdlib() fs.FS {
	if root := os.Getenv("PLUTOROOT"); root != "" {
		return os.DirFS(root)
	}
	return plutonium.Stdlib()
}

var _ io.Writer = os.Stdout
