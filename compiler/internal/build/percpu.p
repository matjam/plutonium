package build

// The Go build reads the linked kernel's symbol table with debug/elf
// (percpu.go); compiler/elf has the ELF types but no reader, so this
// hand-written file reads the section headers and the symbol table itself
// (self-hosting plan, section 8).

import (
	"pluto/errors"
	"compiler/compat"
	"compiler/internal/diag"
	"compiler/pu"
)

// checkPerCPU verifies, in a linked ELF executable, that the per-CPU block
// starts at address 0 with its self word, as GS-relative accesses assume.
// A linker script that places .percpu elsewhere would make every per-CPU
// access wrong, so this is an error rather than a warning.
func checkPerCPU(path string) error {
	data, err := compat.ReadFile(path)
	if err != nil {
		return err
	}
	if len(data) < 64 || data[0] != 0x7f || data[1] != 'E' || data[2] != 'L' || data[3] != 'F' || data[4] != 2 || data[5] != 1 {
		return errors.New("elf: not a little-endian 64-bit ELF file")
	}
	shoff := le64(data, 0x28)
	shentsize := le16(data, 0x3a)
	shnum := le16(data, 0x3c)
	if shentsize < 64 {
		return errors.New("elf: bad section header size")
	}
	for i := uint64(0); i < shnum; i++ {
		sh := shoff + i*shentsize
		if sh+64 > uint64(len(data)) {
			return errors.New("elf: section header out of range")
		}
		if le32(data, sh+4) != 2 { // SHT_SYMTAB
			continue
		}
		off, size, entsize := le64(data, sh+24), le64(data, sh+32), le64(data, sh+56)
		link := le32(data, sh+40)
		strh := shoff + link*shentsize
		if entsize < 24 || off+size > uint64(len(data)) || strh+64 > uint64(len(data)) {
			return errors.New("elf: bad symbol table")
		}
		stroff, strsize := le64(data, strh+24), le64(data, strh+32)
		if stroff+strsize > uint64(len(data)) {
			return errors.New("elf: bad string table")
		}
		strtab := data[stroff : stroff+strsize]
		// The first symbol is the null symbol, which debug/elf leaves out.
		for s := off + entsize; s+24 <= off+size; s += entsize {
			name := cstring(strtab, le32(data, s))
			value := le64(data, s+8)
			if name == perCPUSelf && value != 0 {
				return pu.New(diag.Diagnostic{Code: diag.PerCPUNotAtZero,
					Msg: compat.Sprintf("%s: %s is at %#x: the linker script must link section .percpu at address 0, with the Plutonium object first (abi.md section 10.6)", path, perCPUSelf, value)})
			}
			if name == perCPUTask && value != 8 {
				return pu.New(diag.Diagnostic{Code: diag.PerCPUNotAtZero,
					Msg: compat.Sprintf("%s: %s is at %#x, not 8: the linker script must link section .percpu at address 0, with the Plutonium object first (abi.md sections 10.6 and 12.3)", path, perCPUTask, value)})
			}
		}
		return nil
	}
	return nil // no symbol table: nothing to check
}

// cstring returns the NUL-terminated name at offset i of a string table.
func cstring(strtab []byte, i uint64) string {
	if i >= uint64(len(strtab)) {
		return ""
	}
	end := i
	for end < uint64(len(strtab)) && strtab[end] != 0 {
		end++
	}
	return compat.String(strtab[i:end])
}

func le16(b []byte, i uint64) uint64 { return uint64(b[i]) | uint64(b[i+1])<<8 }

func le32(b []byte, i uint64) uint64 { return le16(b, i) | le16(b, i+2)<<16 }

func le64(b []byte, i uint64) uint64 { return le32(b, i) | le32(b, i+4)<<32 }
