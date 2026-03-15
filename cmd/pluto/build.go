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

	// Link
	return linkObject([]string{objFile}, outputFile, cfg)
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

// extractDirectives pulls //pu:build directive texts from source.
func extractDirectives(src string) []string {
	var dirs []string
	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "//pu:") {
			dirs = append(dirs, strings.TrimPrefix(line, "//"))
		}
	}
	return dirs
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
