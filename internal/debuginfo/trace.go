package debuginfo

import (
	"debug/elf"
	"strings"
	"unicode/utf8"

	"github.com/matjam/plutonium/internal/arch/amd64"
	"github.com/matjam/plutonium/internal/objfile"
)

// TraceSym is the symbol of the trace table. The runtime finds the table
// through it (lib/pluto/rt/trace_amd64.s).
const TraceSym = "pluto.tracetab"

// Layout of the trace table. Keep in sync with lib/pluto/rt/trace.p and
// docs/spec/debugging.md.
const (
	traceHeaderSize = 24
	traceFuncSize   = 32
	traceInlineSize = 16
	traceDeferSize  = 16

	traceFlagFP        = 1
	traceFlagInterrupt = 2
)

// savedBit numbers the callee-saved registers in the order the prologue
// pushes them.
var savedBit = map[amd64.Reg]uint8{amd64.RBX: 0, amd64.RBP: 1, amd64.R12: 2, amd64.R13: 3, amd64.R14: 4, amd64.R15: 5}

// WriteTraceTable adds the trace table to obj's read-only data. It maps
// each function's code to its name, its source lines (with the calls
// inlined into it) and the shape of its frame, which is all the runtime
// needs to walk the stack and print where each frame is.
//
//	header:  u32 nfunc, u32 strings, u32 lines, u32 inlines (offsets from the table),
//	         u32 ndefer, u32 defers (offset)
//	funcs:   nfunc entries of u64 start, u32 size, u32 name, u32 frame size,
//	         u16 prologue end, u8 saved-register mask, u8 flags,
//	         u32 line program offset, u32 first inline site
//	defers:  ndefer entries, by function, of u32 function index, u32 offset
//	         of its defer record from the stack pointer, u64 runner address
//	inlines: u32 parent (1-based within the function, 0 for none), u32 name,
//	         u32 call file, u32 call line
//	strings: u16 length, bytes
//	lines:   per function, rows of uvarint(zigzag(line delta)<<2 | file<<1 | inl),
//	         [uvarint file string], [uvarint inline site], uvarint length
//
// The runtime walks a panicking task's frames with the table to run their
// deferred calls (hosted builds, internal/lower/deferrec.go). If empty is
// set, panics print their message without a trace: the table lists no
// functions at all if none has a defer record, and otherwise only what the
// walk needs, without names (but those of the frames at the bottom of a
// stack), lines or inlined calls.
func (t *Table) WriteTraceTable(obj *objfile.Object, empty bool) {
	funcList := t.funcs
	frameOnly := false
	if empty {
		funcList = nil
		for _, f := range t.funcs {
			if f.DeferRunner != "" {
				funcList, frameOnly = t.funcs, true
				break
			}
		}
	}
	var strs buf
	strIndex := map[string]uint32{}
	str := func(s string) uint32 {
		if off, ok := strIndex[s]; ok {
			return off
		}
		off := uint32(len(strs.b))
		strIndex[s] = off
		// The runtime views these as strings (rt.traceString), which hold
		// valid UTF-8 (language spec section 18.8). A file name on disk is
		// any bytes, so replace invalid ones, and cut at a rune boundary.
		t := strings.ToValidUTF8(s, "\uFFFD")
		n := min(len(t), 0xffff)
		for n > 0 && n < len(t) && !utf8.RuneStart(t[n]) {
			n--
		}
		strs.u16(uint16(n))
		strs.b = append(strs.b, t[:n]...)
		return off
	}

	var funcs, defers, inlines, lines buf
	nInl, nDefer := 0, 0
	for i, f := range funcList {
		d := f.Debug
		if f.DeferRunner != "" {
			defers.u32(uint32(i))
			defers.u32(uint32(f.DeferOff))
			defers.addr(f.DeferRunner, 0)
			nDefer++
		}
		funcs.addr(f.Sym, 0)
		funcs.u32(uint32(f.Size))
		if frameOnly && !bottomFrame(f.Sym) {
			funcs.u32(str(""))
		} else {
			funcs.u32(str(f.Sym))
		}
		funcs.u32(uint32(d.FrameSize))
		funcs.u16(uint16(min(d.PrologueEnd, 0xffff)))
		var mask uint8
		for _, r := range d.Saved {
			mask |= 1 << savedBit[r]
		}
		funcs.u8(mask)
		var flags uint8
		if d.FP {
			flags |= traceFlagFP
		}
		if d.Interrupt {
			flags |= traceFlagInterrupt
		}
		funcs.u8(flags)
		funcs.u32(uint32(len(lines.b)))
		funcs.u32(uint32(nInl))
		if frameOnly {
			// One row of line 0 covering the function.
			lines.uleb(0)
			lines.uleb(uint64(f.Size))
			continue
		}

		for _, s := range f.Inlines {
			file, line := t.position(s.Pos)
			inlines.u32(uint32(s.Parent))
			inlines.u32(str(s.Func))
			inlines.u32(str(file))
			inlines.u32(uint32(line))
			nInl++
		}

		curLine, curFile, curInl := 0, "", int32(0)
		for _, r := range t.traceRows(f) {
			v := zigzag(int64(r.line-curLine)) << 2
			if r.file != curFile {
				v |= 2
			}
			if r.inl != curInl {
				v |= 1
			}
			lines.uleb(v)
			if r.file != curFile {
				lines.uleb(uint64(str(r.file)))
			}
			if r.inl != curInl {
				lines.uleb(uint64(r.inl))
			}
			lines.uleb(uint64(r.size))
			curLine, curFile, curInl = r.line, r.file, r.inl
		}
	}

	var tab buf
	n := len(funcList)
	deferOff := traceHeaderSize + len(funcs.b)
	inlOff := deferOff + len(defers.b)
	strOff := inlOff + len(inlines.b)
	lineOff := strOff + len(strs.b)
	tab.u32(uint32(n))
	tab.u32(uint32(strOff))
	tab.u32(uint32(lineOff))
	tab.u32(uint32(inlOff))
	tab.u32(uint32(nDefer))
	tab.u32(uint32(deferOff))
	for _, b := range []*buf{&funcs, &defers} {
		base := len(tab.b)
		tab.b = append(tab.b, b.b...)
		for _, r := range b.relocs {
			r.Off += uint64(base)
			tab.relocs = append(tab.relocs, r)
		}
	}
	tab.b = append(tab.b, inlines.b...)
	tab.b = append(tab.b, strs.b...)
	tab.b = append(tab.b, lines.b...)

	sec := obj.Section(".rodata", elf.SHT_PROGBITS, elf.SHF_ALLOC, 8)
	off := sec.Append(tab.b, 8)
	for _, r := range tab.relocs {
		r.Off += off
		sec.Relocs = append(sec.Relocs, r)
	}
	obj.Define(&objfile.Symbol{Name: TraceSym, Section: sec, Value: off, Size: uint64(len(tab.b)),
		Bind: elf.STB_GLOBAL, Type: elf.STT_OBJECT})
}

func zigzag(v int64) uint64 { return uint64(v<<1) ^ uint64(v>>63) }

// bottomFrame reports whether sym is a function at the bottom of a stack,
// where the runtime's walk stops (lib/pluto/rt/trace.p, frameWalk.bottom).
func bottomFrame(sym string) bool {
	return sym == "pluto.main" || sym == "pluto/rt.start" || sym == "pluto/rt.taskMain"
}

type traceRow struct {
	size int
	file string
	line int
	inl  int32
}

// traceRows covers a function's code with rows of the same file, line and
// inlined call, starting at offset 0.
func (t *Table) traceRows(f *Func) []traceRow {
	entries := f.Debug.Lines
	if len(entries) == 0 {
		// No positions at all: one row for the declaration.
		entries = []amd64.LineEntry{{Pos: f.Pos}}
	}
	var rows []traceRow
	for i, l := range entries {
		file, line := t.position(l.Pos)
		start, end := l.Off, f.Size
		if i == 0 {
			start = 0
		}
		if i+1 < len(entries) {
			end = entries[i+1].Off
		}
		if end <= start {
			continue
		}
		if n := len(rows); n > 0 && rows[n-1].file == file && rows[n-1].line == line && rows[n-1].inl == l.Inl {
			rows[n-1].size += end - start
			continue
		}
		rows = append(rows, traceRow{size: end - start, file: file, line: line, inl: l.Inl})
	}
	return rows
}
