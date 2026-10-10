package lower

import (
	"encoding/binary"
	"unicode/utf8"

	"github.com/matjam/plutonium/internal/ssa"
)

// The representation of a string (abi.md section 1, language spec section
// 6.10): three words.
//
//	0  data *byte   the first byte
//	8  len  int     the length in bytes
//	16 ctl  uintptr the control word
//
// The control word is the address of the counted buffer's header, which is
// 8-aligned, or'ed with flag bits. A control word below 8 (no header) marks
// a string that is not counted: a literal or other static string, the zero
// string, or a view made with unsafe.String. Copying and dropping such a
// string touches no count. The runtime half is pluto/rt/str.p.
const (
	strSize   = 24
	strCtlOff = 16

	// strNonASCII is set when the string may contain a byte >= 0x80. A
	// clear bit promises that every byte is ASCII, which gives the
	// library O(1) rune indexing and counting. Bits 1 and 2 are reserved;
	// the rest of the word is the header address.
	strNonASCII = 1
)

// strFlags returns the flag bits of a string whose bytes are known.
func strFlags(s string) int64 {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return strNonASCII
		}
	}
	return 0
}

// staticStringHeader returns the bytes and relocations of a static string
// header for s: its data, its length and an uncounted control word that
// holds only the flags.
func (l *Lowerer) staticStringHeader(s string) ([]byte, []ssa.DataReloc) {
	buf := make([]byte, strSize)
	binary.LittleEndian.PutUint64(buf[8:], uint64(len(s)))
	binary.LittleEndian.PutUint64(buf[strCtlOff:], uint64(strFlags(s)))
	if s == "" {
		return buf, nil
	}
	return buf, []ssa.DataReloc{{Off: 0, Sym: l.stringData(s)}}
}

// putStaticString writes the static header of s into d at off.
func (l *Lowerer) putStaticString(d *ssa.Data, off int64, s string) {
	buf, relocs := l.staticStringHeader(s)
	copy(d.Bytes[off:], buf)
	for _, r := range relocs {
		r.Off += off
		d.Relocs = append(d.Relocs, r)
	}
}
