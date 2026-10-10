// Package build drives a whole compilation: load, lower, optimise, generate
// code, assemble and link.
package build

import (
	"bytes"
	"debug/elf"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/matjam/plutonium/internal/arch/amd64"
	"github.com/matjam/plutonium/internal/debuginfo"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/lower"
	"github.com/matjam/plutonium/internal/objfile"
	"github.com/matjam/plutonium/internal/opt"
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// Config describes a build.
type Config struct {
	Dir          string // main package directory
	Output       string // output file
	Freestanding bool
	Tags         []string // extra build tags
	OptLevel     int      // 0 = none, 1 = basic, 2 = full
	LinkerScript string
	Format       string    // "elf" (default) or "binary"
	Listing      io.Writer // if set, receives the machine code listing
	SSADump      string    // function name (or "all") whose SSA is dumped to stderr after each pass
	Stdlib       fs.FS
	Verbose      bool
	KeepWork     bool // keep the temporary directory
	CompileOnly  bool // write the object file to Output and stop

	// NoDWARF omits the DWARF debugging sections (pluto build -g=false).
	NoDWARF bool
	// Strip omits DWARF and the symbol table (pluto build -s). Panics still
	// print symbolised stack traces, which use a table of their own.
	Strip bool
	// StdlibDir, if set, is the directory Stdlib was read from; debugging
	// information names standard library files under it rather than
	// $PLUTOROOT.
	StdlibDir string
}

// Build compiles and links a program.
func Build(cfg *Config) error {
	tags := map[string]bool{"amd64": true}
	if cfg.Freestanding {
		tags["none"] = true
		tags["freestanding"] = true
	} else {
		tags["linux"] = true
		tags["hosted"] = true
	}
	for _, t := range cfg.Tags {
		tags[t] = true
	}
	sizes := types2.SizesFor("gc", "amd64")
	prog, err := load.Load(&load.Config{Tags: tags, Stdlib: cfg.Stdlib, Sizes: sizes}, cfg.Dir)
	if err != nil {
		return err
	}
	sp, err := lower.Lower(&lower.Config{Freestanding: cfg.Freestanding, Sizes: sizes}, prog)
	if err != nil {
		return err
	}
	if err := optimize(cfg, sp); err != nil {
		return err
	}

	obj, err := emitObject(cfg, sp, prog.Fset)
	if err != nil {
		return err
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
			src := filepath.Join(work, fmt.Sprintf("%s_%d.s", strings.ReplaceAll(p.Path, "/", "_"), i))
			if err := os.WriteFile(src, a.Src, 0o644); err != nil {
				return err
			}
			o := strings.TrimSuffix(src, ".s") + ".o"
			if err := run(cfg, "as", "--64", "-o", o, src); err != nil {
				return fmt.Errorf("assembling %s: %w", a.Name, err)
			}
			objs = append(objs, o)
		}
	}

	args := []string{"-static", "-nostdlib", "-z", "noexecstack", "-o", cfg.Output}
	if cfg.Strip {
		args = append(args, "-s")
	}
	if cfg.LinkerScript != "" {
		args = append(args, "-T", cfg.LinkerScript)
	} else {
		args = append(args, "-e", "_start")
	}
	if cfg.Format == "binary" {
		args = append(args, "--oformat=binary")
	}
	args = append(args, objs...)
	return run(cfg, "ld", args...)
}

func run(cfg *Config, name string, args ...string) error {
	if cfg.Verbose {
		fmt.Fprintln(os.Stderr, name, strings.Join(args, " "))
	}
	cmd := exec.Command(name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %v\n%s", name, err, stderr.String())
	}
	return nil
}

// optimize runs the SSA pipeline over every function.
func optimize(cfg *Config, p *ssa.Program) error {
	pipeline := opt.ForLevel(cfg.OptLevel)
	return pipeline.Run(p, func(pass string, f *ssa.Func) {
		if cfg.SSADump != "" && (cfg.SSADump == "all" || cfg.SSADump == f.Name) {
			fmt.Fprintf(os.Stderr, "--- after %s ---\n%s", pass, f)
		}
	})
}

// emitObject generates code for every function and lays out the data.
func emitObject(cfg *Config, p *ssa.Program, fset *token.FileSet) (*objfile.Object, error) {
	obj := objfile.New()
	text := obj.Section(".text", elf.SHT_PROGBITS, elf.SHF_ALLOC|elf.SHF_EXECINSTR, 16)
	obj.Section(".note.GNU-stack", elf.SHT_PROGBITS, 0, 1)
	dbg := debuginfo.New(fset)
	if cfg.StdlibDir != "" {
		dbg.FileName = func(name string) string {
			if rest, ok := strings.CutPrefix(name, "$PLUTOROOT/"); ok {
				return filepath.Join(cfg.StdlibDir, rest)
			}
			return name
		}
	}

	var rodata *objfile.Section
	constDone := map[string]bool{}
	for _, f := range p.Funcs {
		amd64.Prepare(f)
		if err := f.Verify(); err != nil {
			return nil, err
		}
		code, err := amd64.Compile(f, cfg.Listing != nil)
		if err != nil {
			return nil, err
		}
		if cfg.Listing != nil {
			if _, err := fmt.Fprintln(cfg.Listing, code.Asm); err != nil {
				return nil, err
			}
		}
		sec := text
		if f.Section != "" {
			sec = obj.Section(f.Section, elf.SHT_PROGBITS, elf.SHF_ALLOC|elf.SHF_EXECINSTR, 16)
		}
		off := sec.Append(code.Bytes, 16)
		obj.Define(&objfile.Symbol{Name: f.Sym.Name, Section: sec, Value: off, Size: uint64(len(code.Bytes)),
			Bind: elf.STB_GLOBAL, Type: elf.STT_FUNC})
		dbg.Add(&debuginfo.Func{Sym: f.Sym.Name, Pkg: f.Pkg, Pos: f.Pos, Size: len(code.Bytes),
			Debug: code.Debug, Inlines: f.Inlines})
		for _, name := range f.Exports {
			obj.Define(&objfile.Symbol{Name: name, Section: sec, Value: off, Size: uint64(len(code.Bytes)),
				Bind: elf.STB_GLOBAL, Type: elf.STT_FUNC})
		}
		for _, r := range code.Relocs {
			typ := elf.R_X86_64_PC32
			switch r.Type {
			case amd64.RelPLT32:
				typ = elf.R_X86_64_PLT32
			case amd64.Rel64:
				typ = elf.R_X86_64_64
			case amd64.RelAbs32S:
				typ = elf.R_X86_64_32S
			}
			sec.Relocs = append(sec.Relocs, objfile.Reloc{Off: off + uint64(r.Off), Sym: r.Sym.Name, Type: typ, Addend: r.Addend})
		}
		// Pool constants are named by their contents, so one copy serves
		// every function. Packed SSE operands need 16-byte alignment.
		for _, c := range code.Consts {
			if constDone[c.Sym.Name] {
				continue
			}
			constDone[c.Sym.Name] = true
			if rodata == nil {
				rodata = obj.Section(".rodata", elf.SHT_PROGBITS, elf.SHF_ALLOC, 16)
			}
			off := rodata.Append(c.Bytes[:], 16)
			obj.Define(&objfile.Symbol{Name: c.Sym.Name, Section: rodata, Value: off, Size: 16,
				Bind: elf.STB_LOCAL, Type: elf.STT_OBJECT})
		}
	}

	dbg.WriteTraceTable(obj)
	if !cfg.NoDWARF && !cfg.Strip {
		dir, err := filepath.Abs(cfg.Dir)
		if err != nil {
			return nil, err
		}
		dbg.WriteDWARF(obj, dir)
	}

	data := append([]*ssa.Data(nil), p.Data...)
	sort.SliceStable(data, func(i, j int) bool { return data[i].Sym.Name < data[j].Sym.Name })
	for _, d := range data {
		var sec *objfile.Section
		switch {
		case d.Section != "":
			sec = obj.Section(d.Section, elf.SHT_PROGBITS, elf.SHF_ALLOC|elf.SHF_WRITE, 8)
		case d.ReadOnly:
			sec = obj.Section(".rodata", elf.SHT_PROGBITS, elf.SHF_ALLOC, 8)
		case d.Bytes == nil && len(d.Relocs) == 0:
			sec = obj.Section(".bss", elf.SHT_NOBITS, elf.SHF_ALLOC|elf.SHF_WRITE, 8)
		default:
			sec = obj.Section(".data", elf.SHT_PROGBITS, elf.SHF_ALLOC|elf.SHF_WRITE, 8)
		}
		var off uint64
		if sec.Type == elf.SHT_NOBITS {
			off = sec.Reserve(uint64(d.Size), uint64(d.Align))
		} else {
			buf := make([]byte, d.Size)
			copy(buf, d.Bytes)
			off = sec.Append(buf, uint64(max(d.Align, 1)))
			for _, r := range d.Relocs {
				sec.Relocs = append(sec.Relocs, objfile.Reloc{Off: off + uint64(r.Off), Sym: r.Sym.Name, Type: elf.R_X86_64_64, Addend: r.Addend})
			}
		}
		bind := elf.STB_GLOBAL
		if d.Local {
			bind = elf.STB_LOCAL
		}
		obj.Define(&objfile.Symbol{Name: d.Sym.Name, Section: sec, Value: off, Size: uint64(d.Size), Bind: bind, Type: elf.STT_OBJECT})
	}
	return obj, nil
}
