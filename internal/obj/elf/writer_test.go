package elf_test

import (
	"bytes"
	"debug/elf"
	"testing"

	elfwriter "github.com/matjam/plutonium/internal/obj/elf"
)

func TestWriteEmptyObject(t *testing.T) {
	w := elfwriter.NewWriter()
	var buf bytes.Buffer
	if err := w.Write(&buf); err != nil {
		t.Fatalf("write error: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("expected non-empty output")
	}

	// Verify it's valid ELF
	r, err := elf.NewFile(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("not valid ELF: %v", err)
	}
	if r.Type != elf.ET_REL {
		t.Errorf("expected ET_REL, got %v", r.Type)
	}
	if r.Machine != elf.EM_X86_64 {
		t.Errorf("expected EM_X86_64, got %v", r.Machine)
	}
}

func TestWriteWithText(t *testing.T) {
	w := elfwriter.NewWriter()

	// Add a simple function: PUSH RBP; MOV RBP, RSP; POP RBP; RET
	code := []byte{0x55, 0x48, 0x89, 0xE5, 0x5D, 0xC3}
	sym := w.AddSymbol("myfunc", elfwriter.SectionText, 0, uint64(len(code)),
		elfwriter.SymBindGlobal, elfwriter.SymTypeFunc)
	w.AddTextBytes(code)
	_ = sym

	var buf bytes.Buffer
	if err := w.Write(&buf); err != nil {
		t.Fatalf("write error: %v", err)
	}

	r, err := elf.NewFile(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("not valid ELF: %v", err)
	}

	// Find .text section
	textSec := r.Section(".text")
	if textSec == nil {
		t.Fatal("no .text section")
	}
	textData, err := textSec.Data()
	if err != nil {
		t.Fatalf("read .text: %v", err)
	}
	if !bytes.Equal(textData, code) {
		t.Errorf(".text content mismatch: got %x, want %x", textData, code)
	}
}

func TestWriteWithSymbols(t *testing.T) {
	w := elfwriter.NewWriter()

	code := []byte{0x90} // NOP
	w.AddTextBytes(code)
	w.AddSymbol("_start", elfwriter.SectionText, 0, 1, elfwriter.SymBindGlobal, elfwriter.SymTypeFunc)

	var buf bytes.Buffer
	if err := w.Write(&buf); err != nil {
		t.Fatalf("write error: %v", err)
	}

	r, err := elf.NewFile(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("not valid ELF: %v", err)
	}

	syms, err := r.Symbols()
	if err != nil {
		t.Fatalf("read symbols: %v", err)
	}

	found := false
	for _, s := range syms {
		if s.Name == "_start" {
			found = true
		}
	}
	if !found {
		t.Error("symbol '_start' not found in symbol table")
	}
}

func TestWriteWithRelocation(t *testing.T) {
	w := elfwriter.NewWriter()

	// A call instruction with a placeholder: E8 00 00 00 00
	code := []byte{0xE8, 0x00, 0x00, 0x00, 0x00}
	w.AddTextBytes(code)
	callerIdx := w.AddSymbol("caller", elfwriter.SectionText, 0, uint64(len(code)),
		elfwriter.SymBindGlobal, elfwriter.SymTypeFunc)

	// Add the target symbol (undefined)
	targetIdx := w.AddSymbol("target", elfwriter.SectionUndef, 0, 0,
		elfwriter.SymBindGlobal, elfwriter.SymTypeFunc)
	_ = callerIdx

	// Add relocation: at offset 1 (the 4-byte imm in CALL), PC-relative to target
	w.AddRelocation(elfwriter.SectionText, 1, targetIdx, elfwriter.RelocPC32, -4)

	var buf bytes.Buffer
	if err := w.Write(&buf); err != nil {
		t.Fatalf("write error: %v", err)
	}

	r, err := elf.NewFile(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("not valid ELF: %v", err)
	}

	// Check .rela.text exists
	relaSec := r.Section(".rela.text")
	if relaSec == nil {
		t.Fatal("no .rela.text section")
	}
}
