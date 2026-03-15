package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/matjam/plutonium/internal/ast"
	"github.com/matjam/plutonium/internal/buildtag"
	x86 "github.com/matjam/plutonium/internal/codegen/x86_64"
	"github.com/matjam/plutonium/internal/config"
	"github.com/matjam/plutonium/internal/ir"
	"github.com/matjam/plutonium/internal/mono"
	elfwriter "github.com/matjam/plutonium/internal/obj/elf"
	"github.com/matjam/plutonium/internal/parser"
	"github.com/matjam/plutonium/internal/sema"
	"github.com/matjam/plutonium/internal/token"
	"github.com/matjam/plutonium/internal/types"
)

var compileCmd = &cobra.Command{
	Use:   "compile [files...]",
	Short: "Compile .pu source files to object files",
	RunE:  runCompile,
}

func init() {
	compileCmd.Flags().StringP("output", "o", "", "output object file")
	compileCmd.Flags().String("target", "x86_64-linux", "target triple")
	compileCmd.Flags().StringSlice("tags", nil, "build tags (comma-separated)")
	compileCmd.Flags().BoolP("verbose", "v", false, "verbose output")
	rootCmd.AddCommand(compileCmd)
}

func runCompile(cmd *cobra.Command, args []string) error {
	outputFile, _ := cmd.Flags().GetString("output")
	targetTriple, _ := cmd.Flags().GetString("target")
	tagList, _ := cmd.Flags().GetStringSlice("tags")
	verbose, _ := cmd.Flags().GetBool("verbose")

	cfg := config.DefaultConfig()
	cfg.Verbose = verbose
	cfg.Target = targetTriple
	cfg.Arch, cfg.OS = config.ParseTarget(targetTriple)
	// Derive base tags from target triple, then layer extra tags on top.
	cfg.Tags = config.DeriveTagsFromTarget(cfg.Arch, cfg.OS)
	for _, tag := range tagList {
		cfg.Tags[tag] = true
	}

	if len(args) == 0 {
		return fmt.Errorf("no source files specified")
	}

	// Filter files by build tags
	var sourceFiles []string
	for _, f := range args {
		ok, err := buildtag.FileMatches(f, nil, cfg.Tags)
		if err != nil {
			return fmt.Errorf("evaluating build tags for %s: %w", f, err)
		}
		if ok {
			sourceFiles = append(sourceFiles, f)
		}
	}
	if len(sourceFiles) == 0 {
		return fmt.Errorf("no source files match current build tags")
	}

	// Determine output file name
	if outputFile == "" {
		base := filepath.Base(sourceFiles[0])
		ext := filepath.Ext(base)
		outputFile = base[:len(base)-len(ext)] + ".o"
	}

	return compileFiles(sourceFiles, outputFile, cfg)
}

// compileFiles runs the full frontend+backend pipeline and writes an ELF .o file.
func compileFiles(sourceFiles []string, outputFile string, cfg *config.Config) error {
	fset := token.NewFileSet()
	var files []*ast.File

	// Parse all source files
	for _, sf := range sourceFiles {
		src, err := os.ReadFile(sf)
		if err != nil {
			return fmt.Errorf("reading %s: %w", sf, err)
		}
		f, err := parser.ParseFile(fset, sf, src, 0)
		if err != nil {
			return err
		}
		files = append(files, f)
	}

	// Set up import resolution
	loadCfg := resolveLoadConfig(cfg, fset)

	// Type check
	pkg, err := types.Check("main", fset, files, nil, loadCfg)
	if err != nil {
		return err
	}

	// Semantic / kernel-safety validation
	if err := sema.Check(pkg, fset); err != nil {
		return err
	}

	// Monomorphize
	monoResult, err := mono.Monomorphize(pkg, fset)
	if err != nil {
		return err
	}

	// Lower to IR
	prog, err := ir.Lower(monoResult, fset)
	if err != nil {
		return err
	}

	// Code generation
	backend := x86.New()
	writer := elfwriter.NewWriter()

	for _, irPkg := range prog.Packages {
		for _, fn := range irPkg.Functions {
			mf, err := backend.CompileFunction(fn)
			if err != nil {
				return fmt.Errorf("compiling %s: %w", fn.Name, err)
			}
			// Record code offset before adding
			offset := uint64(len(writer.TextBytes()))
			writer.AddSymbol(mf.Name, elfwriter.SectionText, offset, uint64(len(mf.Code)),
				elfwriter.SymBindGlobal, elfwriter.SymTypeFunc)
			writer.AddTextBytes(mf.Code)

			// Add relocations
			for _, reloc := range mf.Relocs {
				symIdx := writer.SymbolIndex(reloc.Symbol)
				if symIdx < 0 {
					// Forward reference — add undefined symbol
					symIdx = writer.AddSymbol(reloc.Symbol, elfwriter.SectionUndef, 0, 0,
						elfwriter.SymBindGlobal, elfwriter.SymTypeFunc)
				}
				writer.AddRelocation(elfwriter.SectionText, offset+uint64(reloc.Offset),
					symIdx, elfwriter.RelocPC32, reloc.Addend)
			}
		}
	}

	// Emit string literals to .rodata
	for _, irPkg := range prog.Packages {
		for _, lit := range irPkg.Strings {
			symName := fmt.Sprintf(".str.%d", lit.ID)
			offset := uint64(len(writer.RODataBytes()))
			writer.AddSymbol(symName, elfwriter.SectionROData, offset, uint64(len(lit.Data)),
				elfwriter.SymBindLocal, elfwriter.SymTypeObject)
			writer.AddRODataBytes(lit.Data)
		}
	}

	// Write output
	f, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("creating %s: %w", outputFile, err)
	}
	defer f.Close()

	if cfg.Verbose {
		fmt.Printf("writing %s\n", outputFile)
	}

	return writer.Write(f)
}

// resolveLoadConfig builds a LoadConfig for import resolution using the
// shared resolveStdlibRoot helper.
func resolveLoadConfig(cfg *config.Config, fset *token.FileSet) *types.LoadConfig {
	stdlibRoot := resolveStdlibRoot()
	if stdlibRoot == "" {
		// No stdlib found — imports will fail at load time with a clear error
		return nil
	}

	return &types.LoadConfig{
		StdlibRoot: stdlibRoot,
		Tags:       cfg.Tags,
		Fset:       fset,
	}
}
