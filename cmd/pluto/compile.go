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

	// Collect transitive dependencies from the user's parsed AST.
	stdlibRoot := resolveStdlibRoot()
	var deps []importedPackage
	if stdlibRoot != "" {
		deps, err = collectDependencies(files, stdlibRoot, cfg.Tags, fset)
		if err != nil {
			return fmt.Errorf("resolving dependencies: %w", err)
		}
	}

	// Monomorphize the user's package.
	monoResult, err := mono.Monomorphize(pkg, fset)
	if err != nil {
		return err
	}

	// Track already-added function names to avoid duplicates.
	addedFuncs := make(map[string]bool)
	for _, fe := range monoResult.Functions {
		addedFuncs[fe.Name] = true
	}

	// Monomorphize each dependency package and merge its functions
	// into the single mono result. Functions are prefixed with the
	// package name (e.g. "syscall.Syscall") to match the qualified
	// names the IR lowerer emits for cross-package calls.
	for _, dep := range deps {
		depPkg := types.GetCachedPackage(dep.ImportPath)
		if depPkg == nil {
			continue
		}
		depMono, monoErr := mono.Monomorphize(depPkg, fset)
		if monoErr != nil {
			return fmt.Errorf("monomorphizing %s: %w", dep.ImportPath, monoErr)
		}
		pkgName := depPkg.Name()
		for _, fe := range depMono.Functions {
			qualifiedName := pkgName + "." + fe.Name
			if addedFuncs[qualifiedName] {
				continue
			}
			addedFuncs[qualifiedName] = true
			fe.Name = qualifiedName
			monoResult.Functions = append(monoResult.Functions, fe)
		}
	}

	// Lower ALL functions to IR together (single compilation unit).
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

	// Synthesize _start: call main, then exit(0)
	startCode := synthesizeStart()
	startOffset := uint64(len(writer.TextBytes()))
	writer.AddSymbol("_start", elfwriter.SectionText, startOffset, uint64(len(startCode)),
		elfwriter.SymBindGlobal, elfwriter.SymTypeFunc)
	writer.AddTextBytes(startCode)

	// Add relocation for the CALL main instruction within _start.
	// Layout of synthesizeStart():
	//   xor ebp, ebp     = 2 bytes  (offset 0)
	//   and rsp, -16     = 4 bytes  (offset 2)
	//   call main        = 5 bytes  (offset 6: E8 xx xx xx xx)
	//   The rel32 operand is at offset 7, instruction ends at offset 11.
	const callMainRelOffset = 7
	mainSymIdx := writer.SymbolIndex("main")
	if mainSymIdx < 0 {
		// main is not yet defined — add as undefined (linker will resolve)
		mainSymIdx = writer.AddSymbol("main", elfwriter.SectionUndef, 0, 0,
			elfwriter.SymBindGlobal, elfwriter.SymTypeFunc)
	}
	writer.AddRelocation(elfwriter.SectionText,
		startOffset+callMainRelOffset, mainSymIdx,
		elfwriter.RelocPC32, -4)

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

// synthesizeStart generates machine code for the _start entry point.
//
//	_start:
//	  xor ebp, ebp       ; clear frame pointer (ABI requirement)
//	  and rsp, -16       ; align stack to 16 bytes
//	  call main          ; call main() — needs relocation
//	  mov rdi, rax       ; exit code = main's return value
//	  mov rax, 231       ; SYS_EXIT_GROUP
//	  syscall
func synthesizeStart() []byte {
	var code []byte
	// xor ebp, ebp (31 ED) — zeros RBP, standard ABI
	code = append(code, 0x31, 0xED)
	// and rsp, -16 (48 83 E4 F0) — align stack
	code = append(code, 0x48, 0x83, 0xE4, 0xF0)
	// call main — E8 00 00 00 00 (placeholder, needs relocation)
	code = append(code, 0xE8, 0x00, 0x00, 0x00, 0x00)
	// mov rdi, rax (48 89 C7) — pass return value as exit code
	code = append(code, 0x48, 0x89, 0xC7)
	// mov rax, 231 (48 C7 C0 E7 00 00 00) — SYS_EXIT_GROUP
	code = append(code, 0x48, 0xC7, 0xC0, 0xE7, 0x00, 0x00, 0x00)
	// syscall (0F 05)
	code = append(code, 0x0F, 0x05)
	return code
}
