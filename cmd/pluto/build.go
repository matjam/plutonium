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
		tmp, err := os.MkdirTemp("", "pluto-run-")
		if err != nil {
			return err
		}
		defer func() { _ = os.RemoveAll(tmp) }()
		cfg.Output = filepath.Join(tmp, "prog")
		if err := build.Build(cfg); err != nil {
			return err
		}
		c := exec.Command(cfg.Output, progArgs...)
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := c.Run(); err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				os.Exit(ee.ExitCode())
			}
			return err
		}
		return nil
	},
}

func init() {
	for _, c := range []*cobra.Command{buildCmd, runCmd} {
		c.Flags().StringP("output", "o", "a.out", "output file")
		c.Flags().String("target", "x86_64-linux", "target triple: x86_64-linux or x86_64-none")
		c.Flags().String("mode", "", "build mode: hosted or freestanding (default from the target)")
		c.Flags().StringSlice("tags", nil, "additional build tags")
		c.Flags().String("ldscript", "", "linker script")
		c.Flags().String("format", "elf", "output format: elf or binary")
		c.Flags().BoolP("verbose", "v", false, "print the assembler and linker commands")
		c.Flags().IntP("opt", "O", 2, "optimisation level: 0, 1 or 2")
		c.Flags().StringP("asm", "S", "", "write a machine code listing to this file (- for stdout)")
		c.Flags().String("ssa", "", "dump the SSA of this function (or all) after each pass")
		c.Flags().Bool("work", false, "keep the temporary work directory")
		c.Flags().BoolP("compile", "c", false, "write the object file instead of linking")
		c.Flags().Bool("gencheck", false, "prototype: tag heap pointers with generations and check every access")
	}
	rootCmd.AddCommand(buildCmd, runCmd)
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
	work, _ := f.GetBool("work")
	compileOnly, _ := f.GetBool("compile")
	genCheck, _ := f.GetBool("gencheck")

	arch, osName, ok := strings.Cut(target, "-")
	if !ok || arch != "x86_64" || (osName != "linux" && osName != "none") {
		return nil, fmt.Errorf("unsupported target %q (supported: x86_64-linux, x86_64-none)", target)
	}
	freestanding := osName == "none"
	switch mode {
	case "":
	case "freestanding":
		freestanding = true
	case "hosted":
		if osName == "none" {
			return nil, fmt.Errorf("hosted mode needs an operating system target")
		}
		freestanding = false
	default:
		return nil, fmt.Errorf("unknown mode %q", mode)
	}
	if format != "elf" && format != "binary" {
		return nil, fmt.Errorf("unsupported format %q (supported: elf, binary)", format)
	}

	cfg := &build.Config{
		Dir:          dir,
		Output:       output,
		Freestanding: freestanding,
		Tags:         tags,
		OptLevel:     optLevel,
		LinkerScript: ldscript,
		Format:       format,
		SSADump:      ssaDump,
		Stdlib:       stdlib(),
		Verbose:      verbose,
		KeepWork:     work,
		CompileOnly:  compileOnly,
		GenCheck:     genCheck,
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
