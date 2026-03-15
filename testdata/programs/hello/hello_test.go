package hello_test

import (
	"bytes"
	"debug/elf"
	"os"
	"testing"

	"github.com/matjam/plutonium/internal/ast"
	cg "github.com/matjam/plutonium/internal/codegen/x86_64"
	"github.com/matjam/plutonium/internal/ir"
	"github.com/matjam/plutonium/internal/mono"
	elfwriter "github.com/matjam/plutonium/internal/obj/elf"
	"github.com/matjam/plutonium/internal/parser"
	"github.com/matjam/plutonium/internal/sema"
	"github.com/matjam/plutonium/internal/token"
	"github.com/matjam/plutonium/internal/types"
)

func TestHelloPipelineEndToEnd(t *testing.T) {
	src, err := os.ReadFile("main.pu")
	if err != nil {
		t.Fatalf("reading main.pu: %v", err)
	}

	// Parse
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.pu", src, 0)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	// Type check
	pkg, err := types.Check("main", fset, []*ast.File{f}, nil, nil)
	if err != nil {
		t.Fatalf("type check error: %v", err)
	}

	// Sema validation
	if err := sema.Check(pkg, fset); err != nil {
		t.Fatalf("sema error: %v", err)
	}

	// Monomorphize
	monoResult, err := mono.Monomorphize(pkg, fset)
	if err != nil {
		t.Fatalf("mono error: %v", err)
	}

	// Lower to IR
	prog, err := ir.Lower(monoResult, fset)
	if err != nil {
		t.Fatalf("IR lowering error: %v", err)
	}
	if len(prog.Packages) == 0 {
		t.Fatal("no packages in IR program")
	}

	// Code generation
	backend := cg.New()
	writer := elfwriter.NewWriter()

	for _, irPkg := range prog.Packages {
		for _, fn := range irPkg.Functions {
			mf, err := backend.CompileFunction(fn)
			if err != nil {
				t.Fatalf("codegen error for %s: %v", fn.Name, err)
			}
			offset := uint64(len(writer.TextBytes()))
			writer.AddSymbol(mf.Name, elfwriter.SectionText, offset, uint64(len(mf.Code)),
				elfwriter.SymBindGlobal, elfwriter.SymTypeFunc)
			writer.AddTextBytes(mf.Code)
		}
	}

	// Write ELF .o
	var buf bytes.Buffer
	if err := writer.Write(&buf); err != nil {
		t.Fatalf("ELF write error: %v", err)
	}

	// Validate the ELF output
	elfFile, err := elf.NewFile(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("ELF validation error: %v", err)
	}
	if elfFile.Type != elf.ET_REL {
		t.Errorf("expected ET_REL, got %v", elfFile.Type)
	}
	if elfFile.Machine != elf.EM_X86_64 {
		t.Errorf("expected EM_X86_64, got %v", elfFile.Machine)
	}
	textSec := elfFile.Section(".text")
	if textSec == nil {
		t.Fatal("no .text section in output")
	}
	textData, err := textSec.Data()
	if err != nil {
		t.Fatalf("reading .text: %v", err)
	}
	if len(textData) == 0 {
		t.Error("expected non-empty .text section")
	}
	t.Logf("compiled hello/main.pu: %d bytes of .text", len(textData))

	// Check symbols
	syms, err := elfFile.Symbols()
	if err != nil {
		t.Fatalf("reading symbols: %v", err)
	}
	foundMain := false
	for _, s := range syms {
		if s.Name == "main" {
			foundMain = true
		}
	}
	if !foundMain {
		t.Error("expected 'main' symbol in output")
	}
}

func TestInvalidProgramsRejected(t *testing.T) {
	cases := []struct {
		file string
		name string
	}{
		{"../../invalid/goroutine.pu", "goroutine"},
		{"../../invalid/chan_type.pu", "chan type"},
		{"../../invalid/new_call.pu", "new() call"},
		{"../../invalid/panic_call.pu", "panic() call"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src, err := os.ReadFile(c.file)
			if err != nil {
				t.Fatalf("reading %s: %v", c.file, err)
			}
			fset := token.NewFileSet()
			f, parseErr := parser.ParseFile(fset, c.file, src, 0)
			if parseErr != nil {
				// Parser rejection is also valid
				return
			}
			pkg, typeErr := types.Check("test", fset, []*ast.File{f}, nil, nil)
			if typeErr != nil {
				return
			}
			semaErr := sema.Check(pkg, fset)
			if semaErr == nil {
				t.Errorf("%s: expected rejection, but program was accepted", c.name)
			}
		})
	}
}
