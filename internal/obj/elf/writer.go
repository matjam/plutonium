package elf

import (
	"debug/elf"
	"encoding/binary"
	"io"
)

// SectionID identifies which section a symbol or relocation belongs to.
type SectionID int

const (
	SectionUndef SectionID = iota
	SectionText
	SectionData
	SectionBSS
	SectionROData
)

// SymBind matches ELF symbol binding.
type SymBind = elf.SymBind

const (
	SymBindLocal  = elf.STB_LOCAL
	SymBindGlobal = elf.STB_GLOBAL
	SymBindWeak   = elf.STB_WEAK
)

// SymType matches ELF symbol type.
type SymType = elf.SymType

const (
	SymTypeNone    = elf.STT_NOTYPE
	SymTypeObject  = elf.STT_OBJECT
	SymTypeFunc    = elf.STT_FUNC
	SymTypeSection = elf.STT_SECTION
	SymTypeFile    = elf.STT_FILE
)

// RelocType is a relocation type.
type RelocType int

const (
	RelocPC32  RelocType = iota // R_X86_64_PC32
	RelocAbs64                  // R_X86_64_64
	Reloc32                     // R_X86_64_32
)

// symbol is an internal symbol entry.
type symbol struct {
	name    string
	section SectionID
	offset  uint64
	size    uint64
	bind    elf.SymBind
	typ     elf.SymType
}

// reloc is an internal relocation entry.
type reloc struct {
	section SectionID
	offset  uint64
	symIdx  int
	typ     RelocType
	addend  int64
}

// Writer builds a relocatable ELF64 object file.
type Writer struct {
	text   []byte
	data   []byte
	bss    uint64 // size of BSS (not stored, just size)
	rodata []byte
	syms   []symbol
	relocs []reloc
}

// NewWriter creates a new ELF writer.
func NewWriter() *Writer {
	return &Writer{}
}

// AddTextBytes appends bytes to the .text section.
func (w *Writer) AddTextBytes(code []byte) {
	w.text = append(w.text, code...)
}

// AddDataBytes appends bytes to the .data section.
func (w *Writer) AddDataBytes(data []byte) {
	w.data = append(w.data, data...)
}

// AddRODataBytes appends bytes to the .rodata section.
func (w *Writer) AddRODataBytes(data []byte) {
	w.rodata = append(w.rodata, data...)
}

// AddSymbol adds a symbol. Returns the symbol index (0-based in our table,
// but note ELF has a null symbol at index 0 in the .symtab).
func (w *Writer) AddSymbol(name string, section SectionID, offset, size uint64, bind elf.SymBind, typ elf.SymType) int {
	idx := len(w.syms)
	w.syms = append(w.syms, symbol{
		name:    name,
		section: section,
		offset:  offset,
		size:    size,
		bind:    bind,
		typ:     typ,
	})
	return idx
}

// AddRelocation adds a relocation entry.
// symIdx is the index returned by AddSymbol.
func (w *Writer) AddRelocation(section SectionID, offset uint64, symIdx int, typ RelocType, addend int64) {
	w.relocs = append(w.relocs, reloc{
		section: section,
		offset:  offset,
		symIdx:  symIdx,
		typ:     typ,
		addend:  addend,
	})
}

// Write writes the ELF64 relocatable object file to out.
func (w *Writer) Write(out io.Writer) error {
	le := binary.LittleEndian

	// --- Build string tables ---
	shstrtab := buildStringTable([]string{
		"", // index 0: empty
		".text",
		".data",
		".bss",
		".rodata",
		".symtab",
		".strtab",
		".rela.text",
		".shstrtab",
	})

	strtab := []byte{0} // null terminator at start
	symNameOffsets := make([]uint32, len(w.syms))
	for i, s := range w.syms {
		symNameOffsets[i] = uint32(len(strtab))
		strtab = append(strtab, []byte(s.name)...)
		strtab = append(strtab, 0)
	}

	// --- Build symbol table ---
	// ELF Sym64: name(4), info(1), other(1), shndx(2), value(8), size(8) = 24 bytes
	const sym64Size = 24
	// Symbol table starts with null symbol
	symtab := make([]byte, sym64Size) // null symbol

	sectionIndex := func(sec SectionID) uint16 {
		switch sec {
		case SectionText:
			return 1
		case SectionData:
			return 2
		case SectionBSS:
			return 3
		case SectionROData:
			return 4
		default:
			return 0 // SHN_UNDEF
		}
	}

	symtabIdx := make([]int, len(w.syms)) // our sym idx → ELF symtab index
	for i, s := range w.syms {
		elfIdx := len(symtab) / sym64Size
		symtabIdx[i] = elfIdx

		sym := make([]byte, sym64Size)
		le.PutUint32(sym[0:4], symNameOffsets[i])       // st_name
		sym[4] = elfSymInfo(s.bind, s.typ)              // st_info
		sym[5] = 0                                      // st_other
		le.PutUint16(sym[6:8], sectionIndex(s.section)) // st_shndx
		le.PutUint64(sym[8:16], s.offset)               // st_value
		le.PutUint64(sym[16:24], s.size)                // st_size
		symtab = append(symtab, sym...)
	}

	// --- Build .rela.text ---
	// Rela64: offset(8), info(8), addend(8) = 24 bytes
	const rela64Size = 24
	var relaText []byte
	for _, r := range w.relocs {
		if r.section != SectionText {
			continue
		}
		rela := make([]byte, rela64Size)
		le.PutUint64(rela[0:8], r.offset)
		elfSymIdx := uint64(symtabIdx[r.symIdx])
		var relocType uint64
		switch r.typ {
		case RelocPC32:
			relocType = uint64(elf.R_X86_64_PC32)
		case RelocAbs64:
			relocType = uint64(elf.R_X86_64_64)
		case Reloc32:
			relocType = uint64(elf.R_X86_64_32)
		}
		le.PutUint64(rela[8:16], (elfSymIdx<<32)|relocType)
		le.PutUint64(rela[16:24], uint64(r.addend))
		relaText = append(relaText, rela...)
	}

	// --- Layout sections ---
	// Section indices:
	// 0: null
	// 1: .text
	// 2: .data
	// 3: .bss
	// 4: .rodata
	// 5: .symtab
	// 6: .strtab
	// 7: .rela.text
	// 8: .shstrtab

	const elfHeaderSize = 64
	const sectionHeaderSize = 64

	numSections := 9
	// Section data offsets (after ELF header)
	dataStart := elfHeaderSize
	textOff := uint64(dataStart)
	dataOff := textOff + uint64(len(w.text))
	// bss has no data, just size
	rodataOff := dataOff + uint64(len(w.data))
	symtabOff := rodataOff + uint64(len(w.rodata))
	strtabOff := symtabOff + uint64(len(symtab))
	relaOff := strtabOff + uint64(len(strtab))
	shstrtabOff := relaOff + uint64(len(relaText))

	// Section header table follows all data
	sectionHeadersOff := shstrtabOff + uint64(len(shstrtab))
	// Align to 8 bytes
	sectionHeadersOff = (sectionHeadersOff + 7) &^ 7

	// --- Write ELF header ---
	header := make([]byte, elfHeaderSize)
	// Magic
	copy(header[0:4], []byte{0x7F, 'E', 'L', 'F'})
	header[4] = 2 // ELFCLASS64
	header[5] = 1 // ELFDATA2LSB (little endian)
	header[6] = 1 // EV_CURRENT
	header[7] = 0 // ELFOSABI_NONE
	// Rest of ident is zero
	le.PutUint16(header[16:18], uint16(elf.ET_REL))    // e_type
	le.PutUint16(header[18:20], uint16(elf.EM_X86_64)) // e_machine
	le.PutUint32(header[20:24], 1)                     // e_version = EV_CURRENT
	le.PutUint64(header[24:32], 0)                     // e_entry = 0
	le.PutUint64(header[32:40], 0)                     // e_phoff = 0 (no program headers)
	le.PutUint64(header[40:48], sectionHeadersOff)     // e_shoff
	le.PutUint32(header[48:52], 0)                     // e_flags
	le.PutUint16(header[52:54], elfHeaderSize)         // e_ehsize
	le.PutUint16(header[54:56], 0)                     // e_phentsize
	le.PutUint16(header[56:58], 0)                     // e_phnum
	le.PutUint16(header[58:60], sectionHeaderSize)     // e_shentsize
	le.PutUint16(header[60:62], uint16(numSections))   // e_shnum
	le.PutUint16(header[62:64], 8)                     // e_shstrndx = .shstrtab = section 8

	// Write everything
	parts := [][]byte{
		header,
		w.text,
		w.data,
		// bss: no data
		w.rodata,
		symtab,
		strtab,
		relaText,
		shstrtab,
	}
	for _, p := range parts {
		if _, err := out.Write(p); err != nil {
			return err
		}
	}

	// Pad to section header alignment
	written := elfHeaderSize + len(w.text) + len(w.data) + len(w.rodata) +
		len(symtab) + len(strtab) + len(relaText) + len(shstrtab)
	padNeeded := int(sectionHeadersOff) - written
	if padNeeded > 0 {
		pad := make([]byte, padNeeded)
		if _, err := out.Write(pad); err != nil {
			return err
		}
	}

	// --- Write section headers ---
	shstrtabOffsets := shstrtabSectionOffsets(shstrtab)

	writeSH := func(
		nameStr string, typ elf.SectionType, flags elf.SectionFlag,
		off, size, link, info uint64, align, entsize uint64,
	) []byte {
		sh := make([]byte, sectionHeaderSize)
		le.PutUint32(sh[0:4], shstrtabOffsets[nameStr])
		le.PutUint32(sh[4:8], uint32(typ))
		le.PutUint64(sh[8:16], uint64(flags))
		le.PutUint64(sh[16:24], 0)    // sh_addr = 0
		le.PutUint64(sh[24:32], off)  // sh_offset
		le.PutUint64(sh[32:40], size) // sh_size
		le.PutUint32(sh[40:44], uint32(link))
		le.PutUint32(sh[44:48], uint32(info))
		le.PutUint64(sh[48:56], align)   // sh_addralign
		le.PutUint64(sh[56:64], entsize) // sh_entsize
		return sh
	}

	sectionHeaders := [][]byte{
		// 0: null
		writeSH("", elf.SHT_NULL, 0, 0, 0, 0, 0, 0, 0),
		// 1: .text
		writeSH(".text", elf.SHT_PROGBITS,
			elf.SHF_ALLOC|elf.SHF_EXECINSTR,
			textOff, uint64(len(w.text)), 0, 0, 16, 0),
		// 2: .data
		writeSH(".data", elf.SHT_PROGBITS,
			elf.SHF_ALLOC|elf.SHF_WRITE,
			dataOff, uint64(len(w.data)), 0, 0, 8, 0),
		// 3: .bss
		writeSH(".bss", elf.SHT_NOBITS,
			elf.SHF_ALLOC|elf.SHF_WRITE,
			dataOff+uint64(len(w.data)), w.bss, 0, 0, 8, 0),
		// 4: .rodata
		writeSH(".rodata", elf.SHT_PROGBITS,
			elf.SHF_ALLOC,
			rodataOff, uint64(len(w.rodata)), 0, 0, 8, 0),
		// 5: .symtab — link=.strtab(6), info=first global sym index
		writeSH(".symtab", elf.SHT_SYMTAB,
			0,
			symtabOff, uint64(len(symtab)), 6, uint64(firstGlobalIdx(w.syms)), 8, sym64Size),
		// 6: .strtab
		writeSH(".strtab", elf.SHT_STRTAB,
			0,
			strtabOff, uint64(len(strtab)), 0, 0, 1, 0),
		// 7: .rela.text — link=.symtab(5), info=.text(1)
		writeSH(".rela.text", elf.SHT_RELA,
			elf.SHF_INFO_LINK,
			relaOff, uint64(len(relaText)), 5, 1, 8, rela64Size),
		// 8: .shstrtab
		writeSH(".shstrtab", elf.SHT_STRTAB,
			0,
			shstrtabOff, uint64(len(shstrtab)), 0, 0, 1, 0),
	}

	for _, sh := range sectionHeaders {
		if _, err := out.Write(sh); err != nil {
			return err
		}
	}

	return nil
}

// --- Helpers ---

func elfSymInfo(bind elf.SymBind, typ elf.SymType) byte {
	return byte(bind)<<4 | byte(typ)&0xF
}

func buildStringTable(strs []string) []byte {
	var t []byte
	for _, s := range strs {
		t = append(t, []byte(s)...)
		t = append(t, 0)
	}
	return t
}

func shstrtabSectionOffsets(shstrtab []byte) map[string]uint32 {
	m := make(map[string]uint32)
	i := 0
	for i < len(shstrtab) {
		j := i
		for j < len(shstrtab) && shstrtab[j] != 0 {
			j++
		}
		name := string(shstrtab[i:j])
		m[name] = uint32(i)
		i = j + 1
	}
	return m
}

func firstGlobalIdx(syms []symbol) int {
	// +1 for the null symbol at index 0
	for i, s := range syms {
		if s.bind == elf.STB_GLOBAL {
			return i + 1
		}
	}
	return len(syms) + 1
}
