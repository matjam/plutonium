package codegen

import (
	"encoding/binary"

	"github.com/matjam/plutonium/internal/ir"
)

// Backend generates machine code for a specific architecture.
type Backend interface {
	Name() string
	Triple() string
	WordSize() int
	Endianness() binary.ByteOrder
	CompileFunction(fn *ir.Function) (*MachineFunc, error)
}

// MachineFunc is the compiled output for a single function.
type MachineFunc struct {
	Name   string
	Code   []byte  // machine code bytes
	Relocs []Reloc // relocations
	Size   int
}

// Reloc is a relocation entry.
type Reloc struct {
	Offset int    // byte offset within Code
	Symbol string // target symbol name
	Addend int64
	Type   RelocType
}

type RelocType int

const (
	RelocPC32  RelocType = iota // 32-bit PC-relative
	RelocAbs64                  // 64-bit absolute
)
