// Package elf is the part of Go's debug/elf that the compiler's object
// writer uses: the ELF64 constants and the on-disk structures. It is written
// by hand (debug/elf's reader needs io.SectionReader, compression and
// reflection-based decoding, none of which the compiler uses); the values
// are those of debug/elf, which takes them from the ELF specification.
//
// The structures are written field by field, little-endian, by the encoders
// tools/go2pu generates for encoding/binary.Write calls.
package elf

// EI_NIDENT is the size of the identification bytes at the start of a file.
const EI_NIDENT = 16

// Version is the ELF format version.
type Version byte

const EV_CURRENT Version = 1

// Class is the word size of the file.
type Class byte

const ELFCLASS64 Class = 2

// Data is the byte order of the file.
type Data byte

const ELFDATA2LSB Data = 1

// Type is the file type.
type Type uint16

const ET_REL Type = 1

// Machine is the target architecture.
type Machine uint16

const EM_X86_64 Machine = 62

// SectionType is a section header's type.
type SectionType uint32

const (
	SHT_NULL     SectionType = 0
	SHT_PROGBITS SectionType = 1
	SHT_SYMTAB   SectionType = 2
	SHT_STRTAB   SectionType = 3
	SHT_RELA     SectionType = 4
	SHT_NOBITS   SectionType = 8
)

// SectionFlag is a section header's flags.
type SectionFlag uint32

const (
	SHF_WRITE     SectionFlag = 0x1
	SHF_ALLOC     SectionFlag = 0x2
	SHF_EXECINSTR SectionFlag = 0x4
	SHF_INFO_LINK SectionFlag = 0x40
)

// SymBind is a symbol's binding.
type SymBind int

const (
	STB_LOCAL  SymBind = 0
	STB_GLOBAL SymBind = 1
)

// SymType is a symbol's type.
type SymType int

const (
	STT_NOTYPE  SymType = 0
	STT_OBJECT  SymType = 1
	STT_FUNC    SymType = 2
	STT_SECTION SymType = 3
)

// R_X86_64 is an x86-64 relocation type.
type R_X86_64 int

const (
	R_X86_64_64    R_X86_64 = 1
	R_X86_64_PC32  R_X86_64 = 2
	R_X86_64_PLT32 R_X86_64 = 4
	R_X86_64_32    R_X86_64 = 10
	R_X86_64_32S   R_X86_64 = 11
)

// ST_INFO packs a symbol's binding and type into its info byte.
func ST_INFO(bind SymBind, typ SymType) uint8 {
	return uint8(bind)<<4 | uint8(typ)&0xf
}

// R_INFO packs a relocation's symbol index and type.
func R_INFO(sym, typ uint32) uint64 { return uint64(sym)<<32 | uint64(typ) }

// Header64 is the ELF64 file header.
type Header64 struct {
	Ident     [EI_NIDENT]byte
	Type      uint16
	Machine   uint16
	Version   uint32
	Entry     uint64
	Phoff     uint64
	Shoff     uint64
	Flags     uint32
	Ehsize    uint16
	Phentsize uint16
	Phnum     uint16
	Shentsize uint16
	Shnum     uint16
	Shstrndx  uint16
}

// Section64 is an ELF64 section header.
type Section64 struct {
	Name      uint32
	Type      uint32
	Flags     uint64
	Addr      uint64
	Off       uint64
	Size      uint64
	Link      uint32
	Info      uint32
	Addralign uint64
	Entsize   uint64
}

// Sym64 is an ELF64 symbol table entry.
type Sym64 struct {
	Name  uint32
	Info  uint8
	Other uint8
	Shndx uint16
	Value uint64
	Size  uint64
}

// Rela64 is an ELF64 relocation with an explicit addend.
type Rela64 struct {
	Off    uint64
	Info   uint64
	Addend int64
}
