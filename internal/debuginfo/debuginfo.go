// Package debuginfo turns the backend's per-function debug information into
// DWARF sections for debuggers and profilers, and into the compact trace
// table the runtime reads to print stack traces when a program panics.
//
// See docs/spec/debugging.md for the formats.
package debuginfo

import (
	"debug/elf"
	"encoding/binary"
	"sort"

	"github.com/matjam/plutonium/internal/arch/amd64"
	"github.com/matjam/plutonium/internal/objfile"
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/token"
)

// A Func is one compiled function.
type Func struct {
	Sym     string // symbol of its code
	Pkg     string // import path of its package, or "" for generated code
	Pos     token.Pos
	Size    int
	Debug   *amd64.FuncDebug
	Inlines []ssa.InlineSite
	// DeferRunner, if set, is the runner of the function's defer record,
	// which lies DeferOff bytes above the stack pointer in its frame
	// (ssa.Func.DeferRecord).
	DeferRunner string
	DeferOff    int64
}

// A Table collects the compiled functions of a program.
type Table struct {
	fset  *token.FileSet
	funcs []*Func
	// FileName maps a source file name to the name recorded for it; nil
	// keeps names unchanged.
	FileName func(string) string
}

// New returns an empty table whose positions are in fset.
func New(fset *token.FileSet) *Table { return &Table{fset: fset} }

// Add records a compiled function.
func (t *Table) Add(f *Func) { t.funcs = append(t.funcs, f) }

// position returns the file name and line of p.
func (t *Table) position(p token.Pos) (string, int) {
	if !p.IsValid() || t.fset == nil {
		return "", 0
	}
	pos := t.fset.Position(p)
	name := pos.Filename
	if t.FileName != nil && name != "" {
		name = t.FileName(name)
	}
	return name, pos.Line
}

// column returns the column of p, or 0 if it has none.
func (t *Table) column(p token.Pos) int {
	if !p.IsValid() || t.fset == nil {
		return 0
	}
	return t.fset.Position(p).Column
}

// buf is a section under construction, with its relocations.
type buf struct {
	b      []byte
	relocs []objfile.Reloc
}

func (b *buf) u8(v uint8)   { b.b = append(b.b, v) }
func (b *buf) u16(v uint16) { b.b = binary.LittleEndian.AppendUint16(b.b, v) }
func (b *buf) u32(v uint32) { b.b = binary.LittleEndian.AppendUint32(b.b, v) }
func (b *buf) u64(v uint64) { b.b = binary.LittleEndian.AppendUint64(b.b, v) }
func (b *buf) str(s string) { b.b = append(append(b.b, s...), 0) }

func (b *buf) uleb(v uint64) {
	for {
		c := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			c |= 0x80
		}
		b.b = append(b.b, c)
		if v == 0 {
			return
		}
	}
}

func (b *buf) sleb(v int64) {
	for {
		c := byte(v & 0x7f)
		v >>= 7
		done := v == 0 && c&0x40 == 0 || v == -1 && c&0x40 != 0
		if !done {
			c |= 0x80
		}
		b.b = append(b.b, c)
		if done {
			return
		}
	}
}

// addr appends a 64-bit address of sym+add.
func (b *buf) addr(sym string, add int64) {
	b.relocs = append(b.relocs, objfile.Reloc{Off: uint64(len(b.b)), Sym: sym, Type: elf.R_X86_64_64, Addend: add})
	b.u64(0)
}

// secOff appends a 32-bit offset into another section, through that
// section's symbol (see emit).
func (b *buf) secOff(sym string, off int64) {
	b.relocs = append(b.relocs, objfile.Reloc{Off: uint64(len(b.b)), Sym: sym, Type: elf.R_X86_64_32, Addend: off})
	b.u32(0)
}

// patch32 stores v at off.
func (b *buf) patch32(off int, v uint32) { binary.LittleEndian.PutUint32(b.b[off:], v) }

// align pads with zero bytes to a multiple of n.
func (b *buf) align(n int, pad byte) {
	for len(b.b)%n != 0 {
		b.b = append(b.b, pad)
	}
}

// emit adds b as a section of obj, with a section symbol of the same name
// so that other sections can refer to offsets in it.
func (b *buf) emit(obj *objfile.Object, name string, flags elf.SectionFlag, align uint64) *objfile.Section {
	sec := obj.Section(name, elf.SHT_PROGBITS, flags, align)
	off := sec.Append(b.b, align)
	for _, r := range b.relocs {
		r.Off += off
		sec.Relocs = append(sec.Relocs, r)
	}
	obj.Define(&objfile.Symbol{Name: name, Section: sec, Value: 0, Bind: elf.STB_LOCAL, Type: elf.STT_SECTION})
	return sec
}

// packages groups the functions by package, in a stable order.
func (t *Table) packages() []pkgFuncs {
	byPkg := map[string][]*Func{}
	var names []string
	for _, f := range t.funcs {
		if _, ok := byPkg[f.Pkg]; !ok {
			names = append(names, f.Pkg)
		}
		byPkg[f.Pkg] = append(byPkg[f.Pkg], f)
	}
	sort.Strings(names)
	var out []pkgFuncs
	for _, n := range names {
		out = append(out, pkgFuncs{n, byPkg[n]})
	}
	return out
}

type pkgFuncs struct {
	path  string
	funcs []*Func
}
