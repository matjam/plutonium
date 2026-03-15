package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/matjam/plutonium/internal/buildtag"
	"github.com/matjam/plutonium/internal/config"
)

var buildCmd = &cobra.Command{
	Use:   "build [packages]",
	Short: "Compile and link Plutonium packages",
	RunE:  runBuild,
}

func init() {
	buildCmd.Flags().StringP("output", "o", "a.out", "output file")
	buildCmd.Flags().String("target", "x86_64-linux", "target triple")
	buildCmd.Flags().String("mode", "hosted", "build mode: hosted or freestanding")
	buildCmd.Flags().StringSlice("tags", nil, "build tags")
	buildCmd.Flags().String("ldscript", "", "linker script (freestanding mode)")
	buildCmd.Flags().String("format", "elf", "output format: elf, binary, pe32+")
	buildCmd.Flags().BoolP("verbose", "v", false, "verbose output")
	buildCmd.Flags().StringP("opt", "O", "0", "optimization level: 0, 1, 2, s")
	buildCmd.Flags().BoolP("asm", "S", false, "emit assembly instead of object files")
	rootCmd.AddCommand(buildCmd)
}

func runBuild(cmd *cobra.Command, args []string) error {
	outputFile, _ := cmd.Flags().GetString("output")
	targetTriple, _ := cmd.Flags().GetString("target")
	modeStr, _ := cmd.Flags().GetString("mode")
	tagList, _ := cmd.Flags().GetStringSlice("tags")
	ldscript, _ := cmd.Flags().GetString("ldscript")
	formatStr, _ := cmd.Flags().GetString("format")
	verbose, _ := cmd.Flags().GetBool("verbose")

	cfg := config.DefaultConfig()
	cfg.Target = targetTriple
	cfg.Arch, cfg.OS = config.ParseTarget(targetTriple)
	cfg.Verbose = verbose
	cfg.LinkerScript = ldscript
	cfg.OutputFile = outputFile
	// Derive base tags from the target triple, then layer any extra tags on top.
	cfg.Tags = config.DeriveTagsFromTarget(cfg.Arch, cfg.OS)
	for _, tag := range tagList {
		cfg.Tags[tag] = true
	}

	switch strings.ToLower(modeStr) {
	case "freestanding":
		cfg.Mode = config.ModeFreestanding
		cfg.Tags["freestanding"] = true
		cfg.Tags["none"] = true
	default:
		cfg.Mode = config.ModeHosted
	}

	switch strings.ToLower(formatStr) {
	case "binary":
		cfg.Format = config.FormatBinary
	case "pe32+":
		cfg.Format = config.FormatPE32Plus
	default:
		cfg.Format = config.FormatELF
	}

	// Collect .pu source files from args (directories or files)
	sourceFiles, err := collectSources(args, cfg)
	if err != nil {
		return err
	}
	if len(sourceFiles) == 0 {
		return fmt.Errorf("no .pu source files found")
	}

	if verbose {
		fmt.Printf("compiling %d source files for target %s\n", len(sourceFiles), cfg.Target)
	}

	// Compile to a temp .o file
	tmpDir := os.TempDir()
	objFile := filepath.Join(tmpDir, "pluto_output.o")

	if err := compileFiles(sourceFiles, objFile, cfg); err != nil {
		return err
	}

	objFiles := []string{objFile}

	// Collect and assemble .s files from stdlib directories.
	// Scan the pluto stdlib root (if found) for assembly stubs that match
	// current build tags.
	stdlibRoot := resolveStdlibRoot()
	if stdlibRoot != "" {
		asmDirs := []string{
			filepath.Join(stdlibRoot, "syscall"),
		}
		for _, dir := range asmDirs {
			if _, err := os.Stat(dir); err != nil {
				continue
			}
			asmFiles, err := collectAssemblyFiles(dir, cfg)
			if err != nil {
				return fmt.Errorf("collecting assembly files from %s: %w", dir, err)
			}
			for _, sf := range asmFiles {
				oFile, err := assembleFile(sf, cfg)
				if err != nil {
					return err
				}
				objFiles = append(objFiles, oFile)
			}
		}
	}

	// Link
	return linkObject(objFiles, outputFile, cfg)
}

// resolveStdlibRoot locates the pluto stdlib directory. It checks
// PLUTONIUM_ROOT, the compiler binary's parent, and the working directory.
// Returns "" if no stdlib directory is found.
func resolveStdlibRoot() string {
	root := os.Getenv("PLUTONIUM_ROOT")
	if root != "" {
		return filepath.Join(root, "pluto")
	}
	// Try relative to the compiler binary
	exe, err := os.Executable()
	if err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "..", "pluto")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	// Fallback: try current working directory
	if info, err := os.Stat("pluto"); err == nil && info.IsDir() {
		return "pluto"
	}
	return ""
}

// collectSources finds .pu files from the given paths, applying build tag filtering.
func collectSources(args []string, cfg *config.Config) ([]string, error) {
	var sources []string
	for _, arg := range args {
		info, err := os.Stat(arg)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", arg, err)
		}
		if info.IsDir() {
			entries, err := os.ReadDir(arg)
			if err != nil {
				return nil, fmt.Errorf("reading dir %s: %w", arg, err)
			}
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				name := e.Name()
				if !strings.HasSuffix(name, ".pu") {
					continue
				}
				path := filepath.Join(arg, name)
				// Read directives from file for build tag evaluation
				src, err := os.ReadFile(path)
				if err != nil {
					return nil, err
				}
				directives := extractDirectives(string(src))
				ok, err := buildtag.FileMatches(name, directives, cfg.Tags)
				if err != nil {
					return nil, err
				}
				if ok {
					sources = append(sources, path)
				}
			}
		} else if strings.HasSuffix(arg, ".pu") {
			sources = append(sources, arg)
		}
	}
	return sources, nil
}

// extractDirectives pulls pu:build directive texts from source.
// It recognises both //pu: (Plutonium source) and #pu: (GNU assembly) comment
// styles so that .s files can carry build tags too.
func extractDirectives(src string) []string {
	var dirs []string
	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "//pu:"):
			dirs = append(dirs, strings.TrimPrefix(line, "//"))
		case strings.HasPrefix(line, "#pu:"):
			dirs = append(dirs, strings.TrimPrefix(line, "#"))
		}
	}
	return dirs
}

// collectAssemblyFiles finds .s files in dir, filtered by build tags.
func collectAssemblyFiles(dir string, cfg *config.Config) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var asmFiles []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".s") {
			continue
		}
		path := filepath.Join(dir, name)
		// Read file for build tag directives
		src, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		directives := extractDirectives(string(src))
		ok, err := buildtag.FileMatches(name, directives, cfg.Tags)
		if err != nil {
			continue
		}
		if ok {
			asmFiles = append(asmFiles, path)
		}
	}
	return asmFiles, nil
}

// assembleFile assembles a .s file to a .o file using the system assembler.
func assembleFile(sFile string, cfg *config.Config) (string, error) {
	oFile := filepath.Join(os.TempDir(), filepath.Base(sFile)+".o")
	var args []string
	switch cfg.Arch {
	case "x86_64":
		args = []string{"--64", "-o", oFile, sFile}
	case "aarch64":
		args = []string{"-o", oFile, sFile}
	default:
		args = []string{"-o", oFile, sFile}
	}

	if cfg.Verbose {
		fmt.Printf("assembling: as %s\n", strings.Join(args, " "))
	}

	cmd := exec.Command("as", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("assembling %s: %w", sFile, err)
	}
	return oFile, nil
}

// linkObject invokes the system linker to produce the final output.
func linkObject(objFiles []string, outputFile string, cfg *config.Config) error {
	var ldArgs []string

	ldArgs = append(ldArgs, "-static", "-nostdlib", "-e", "_start")

	switch cfg.Format {
	case config.FormatBinary:
		ldArgs = append(ldArgs, "-T", cfg.LinkerScript, "--oformat=binary")
	default:
		// ELF: standard link
		if cfg.LinkerScript != "" {
			ldArgs = append(ldArgs, "-T", cfg.LinkerScript)
		}
	}

	ldArgs = append(ldArgs, "-o", outputFile)
	ldArgs = append(ldArgs, objFiles...)

	if cfg.Verbose {
		fmt.Printf("linking: ld %s\n", strings.Join(ldArgs, " "))
	}

	ld := exec.Command("ld", ldArgs...)
	ld.Stdout = os.Stdout
	ld.Stderr = os.Stderr
	if err := ld.Run(); err != nil {
		return fmt.Errorf("linker error: %w", err)
	}
	return nil
}
