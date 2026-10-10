package debuginfo

import (
	"debug/dwarf"
	"path"
	"strings"

	"github.com/matjam/plutonium/internal/arch/amd64"
	"github.com/matjam/plutonium/internal/objfile"
)

// DWARF 4 constants that debug/dwarf does not export.
const (
	dwLangGo = 0x16

	dwFormAddr        = 0x01
	dwFormData1       = 0x0b
	dwFormData4       = 0x06
	dwFormString      = 0x08
	dwFormUdata       = 0x0f
	dwFormSecOffset   = 0x17
	dwFormExprloc     = 0x18
	dwFormFlagPresent = 0x19

	dwOpCallFrameCFA = 0x9c

	dwLnsCopy            = 1
	dwLnsAdvancePC       = 2
	dwLnsAdvanceLine     = 3
	dwLnsSetFile         = 4
	dwLnsSetPrologueEnd  = 10
	dwLneEndSequence     = 1
	dwLneSetAddress      = 2
	lineBase             = -5
	lineRange            = 14
	lineOpcodeBase       = 13
	dwCFAAdvanceLoc      = 0x40
	dwCFAOffset          = 0x80
	dwCFARestore         = 0xc0
	dwCFAAdvanceLoc1     = 0x02
	dwCFAAdvanceLoc2     = 0x03
	dwCFAAdvanceLoc4     = 0x04
	dwCFADefCFA          = 0x0c
	dwCFADefCFAOffset    = 0x0e
	dwCFADefCFARegister  = 0x0d
	dwCFANop             = 0x00
	dataAlignmentFactor  = -8
	abbrevCompileUnit    = 1
	abbrevSubprogram     = 2
	abbrevSubprogramNoLn = 3
)

// Producer is recorded as DW_AT_producer.
var Producer = "plutonium"

// WriteDWARF adds .debug_abbrev, .debug_info, .debug_line, .debug_ranges,
// .debug_aranges and .debug_frame to obj, describing every function: one
// compile unit per package, a subprogram per function, a line table and
// call frame information. compDir is recorded as each unit's directory.
func (t *Table) WriteDWARF(obj *objfile.Object, compDir string) {
	var abbrev, info, line, ranges, aranges buf
	writeAbbrevs(&abbrev)
	for _, p := range t.packages() {
		cuOff := len(info.b)
		lineOff := len(line.b)
		files := t.writeLines(&line, p.funcs)

		rangesOff := len(ranges.b)
		for _, f := range p.funcs {
			ranges.addr(f.Sym, 0)
			ranges.addr(f.Sym, int64(f.Size))
		}
		ranges.u64(0)
		ranges.u64(0)

		// Compile unit header.
		info.u32(0) // length, patched below
		info.u16(4)
		info.secOff(".debug_abbrev", 0)
		info.u8(8)

		name := p.path
		if name == "" {
			name = "pluto"
		}
		info.uleb(abbrevCompileUnit)
		info.str(Producer)
		info.u8(dwLangGo)
		info.str(name)
		info.str(compDir)
		info.secOff(".debug_line", int64(lineOff))
		info.u64(0) // low_pc: ranges hold absolute addresses
		info.secOff(".debug_ranges", int64(rangesOff))
		for _, f := range p.funcs {
			file, ln := t.position(f.Pos)
			if ln > 0 {
				info.uleb(abbrevSubprogram)
			} else {
				info.uleb(abbrevSubprogramNoLn)
			}
			info.str(f.Sym)
			info.addr(f.Sym, 0)
			info.u32(uint32(f.Size))
			info.uleb(1)
			info.u8(dwOpCallFrameCFA)
			if ln > 0 {
				info.uleb(uint64(files[file]))
				info.uleb(uint64(ln))
			}
		}
		info.u8(0) // end of the unit's children
		info.patch32(cuOff, uint32(len(info.b)-cuOff-4))

		// Address ranges, for finding the unit of a PC quickly.
		arOff := len(aranges.b)
		aranges.u32(0)
		aranges.u16(2)
		aranges.secOff(".debug_info", int64(cuOff))
		aranges.u8(8)
		aranges.u8(0)
		aranges.u32(0) // pad the header to a multiple of 16 bytes
		for _, f := range p.funcs {
			aranges.addr(f.Sym, 0)
			aranges.u64(uint64(f.Size))
		}
		aranges.u64(0)
		aranges.u64(0)
		aranges.patch32(arOff, uint32(len(aranges.b)-arOff-4))
	}
	frame := t.debugFrame()

	abbrev.emit(obj, ".debug_abbrev", 0, 1)
	info.emit(obj, ".debug_info", 0, 1)
	line.emit(obj, ".debug_line", 0, 1)
	ranges.emit(obj, ".debug_ranges", 0, 1)
	aranges.emit(obj, ".debug_aranges", 0, 1)
	frame.emit(obj, ".debug_frame", 0, 8)
}

func writeAbbrevs(b *buf) {
	attr := func(at dwarf.Attr, form int) {
		b.uleb(uint64(at))
		b.uleb(uint64(form))
	}
	b.uleb(abbrevCompileUnit)
	b.uleb(uint64(dwarf.TagCompileUnit))
	b.u8(1) // children
	attr(dwarf.AttrProducer, dwFormString)
	attr(dwarf.AttrLanguage, dwFormData1)
	attr(dwarf.AttrName, dwFormString)
	attr(dwarf.AttrCompDir, dwFormString)
	attr(dwarf.AttrStmtList, dwFormSecOffset)
	attr(dwarf.AttrLowpc, dwFormAddr)
	attr(dwarf.AttrRanges, dwFormSecOffset)
	b.u16(0)

	for _, withLine := range []bool{true, false} {
		if withLine {
			b.uleb(abbrevSubprogram)
		} else {
			b.uleb(abbrevSubprogramNoLn)
		}
		b.uleb(uint64(dwarf.TagSubprogram))
		b.u8(0)
		attr(dwarf.AttrName, dwFormString)
		attr(dwarf.AttrLowpc, dwFormAddr)
		attr(dwarf.AttrHighpc, dwFormData4)
		attr(dwarf.AttrFrameBase, dwFormExprloc)
		if withLine {
			attr(dwarf.AttrDeclFile, dwFormUdata)
			attr(dwarf.AttrDeclLine, dwFormUdata)
		}
		attr(dwarf.AttrExternal, dwFormFlagPresent)
		b.u16(0)
	}
	b.u8(0)
}

// writeLines appends the line number program of one compile unit and
// returns its file numbers.
func (t *Table) writeLines(b *buf, funcs []*Func) map[string]int {
	files := map[string]int{}
	var names []string
	file := func(name string) int {
		if n, ok := files[name]; ok {
			return n
		}
		names = append(names, name)
		files[name] = len(names)
		return len(names)
	}
	for _, f := range funcs {
		if name, _ := t.position(f.Pos); name != "" {
			file(name)
		}
		for _, l := range f.Debug.Lines {
			if name, _ := t.position(l.Pos); name != "" {
				file(name)
			}
		}
	}

	start := len(b.b)
	b.u32(0) // unit length
	b.u16(4) // version
	hdrLenAt := len(b.b)
	b.u32(0) // header length
	hdrStart := len(b.b)
	b.u8(1) // minimum instruction length
	b.u8(1) // maximum operations per instruction
	b.u8(1) // default is_stmt
	b.u8(256 + lineBase) // signed byte
	b.u8(lineRange)
	b.u8(lineOpcodeBase)
	b.b = append(b.b, 0, 1, 1, 1, 1, 0, 0, 0, 1, 0, 0, 1) // standard opcode lengths
	var dirs []string
	dirIndex := map[string]int{}
	for _, n := range names {
		d := path.Dir(n)
		if _, ok := dirIndex[d]; !ok && strings.Contains(n, "/") {
			dirs = append(dirs, d)
			dirIndex[d] = len(dirs)
		}
	}
	for _, d := range dirs {
		b.str(d)
	}
	b.u8(0)
	for _, n := range names {
		if i, ok := dirIndex[path.Dir(n)]; ok && strings.Contains(n, "/") {
			b.str(path.Base(n))
			b.uleb(uint64(i))
		} else {
			b.str(n)
			b.uleb(0)
		}
		b.uleb(0) // modification time
		b.uleb(0) // length
	}
	b.u8(0)
	b.patch32(hdrLenAt, uint32(len(b.b)-hdrStart))

	for _, f := range funcs {
		t.lineSequence(b, f, files)
	}
	b.patch32(start, uint32(len(b.b)-start-4))
	return files
}

// lineSequence appends one function's rows, as a sequence of its own.
func (t *Table) lineSequence(b *buf, f *Func, files map[string]int) {
	rows := t.lineRows(f)
	if len(rows) == 0 {
		return // generated code with no source
	}
	b.u8(0)
	b.uleb(9)
	b.u8(dwLneSetAddress)
	b.addr(f.Sym, 0)
	addr, ln, fileNo := 0, 1, 1
	row := func(off int, name string, line int, prologueEnd bool) {
		if n := files[name]; n != 0 && n != fileNo {
			b.u8(dwLnsSetFile)
			b.uleb(uint64(n))
			fileNo = n
		}
		if prologueEnd {
			b.u8(dwLnsSetPrologueEnd)
		}
		dl, da := line-ln, off-addr
		op := dl - lineBase + lineRange*da + lineOpcodeBase
		if dl >= lineBase && dl < lineBase+lineRange && op <= 255 {
			b.u8(byte(op))
		} else {
			if da != 0 {
				b.u8(dwLnsAdvancePC)
				b.uleb(uint64(da))
			}
			if dl != 0 {
				b.u8(dwLnsAdvanceLine)
				b.sleb(int64(dl))
			}
			b.u8(dwLnsCopy)
		}
		addr, ln = off, line
	}
	for _, r := range rows {
		row(r.off, r.file, r.line, r.prologueEnd)
	}
	if f.Size > addr {
		b.u8(dwLnsAdvancePC)
		b.uleb(uint64(f.Size - addr))
	}
	b.u8(0)
	b.uleb(1)
	b.u8(dwLneEndSequence)
}

type lineRow struct {
	off         int
	file        string
	line        int
	prologueEnd bool
}

// lineRows returns a function's line table rows, one where the file or line
// changes. Debuggers put a function's breakpoint at the row marked as the
// end of the prologue: the first line of the body after the frame is set
// up and the parameters are stored, or the end of the frame setup if the
// whole function is on its declaration's line.
func (t *Table) lineRows(f *Func) []lineRow {
	var rows []lineRow
	d := f.Debug
	add := func(r lineRow) {
		if n := len(rows); n > 0 && rows[n-1].file == r.file && rows[n-1].line == r.line {
			return
		}
		rows = append(rows, r)
	}
	for _, l := range d.Lines {
		name, line := t.position(l.Pos)
		if line == 0 {
			continue
		}
		add(lineRow{off: l.Off, file: name, line: line})
	}
	if len(rows) == 0 {
		if name, line := t.position(f.Pos); line > 0 {
			rows = append(rows, lineRow{file: name, line: line})
		}
	}
	pe := d.PrologueEnd
	if pe <= 0 || pe >= f.Size || len(rows) == 0 {
		return rows
	}
	declFile, declLine := t.position(f.Pos)
	for i := range rows {
		if rows[i].off >= pe && (rows[i].file != declFile || rows[i].line != declLine) {
			rows[i].prologueEnd = true
			return rows
		}
	}
	// Split the row covering the end of the prologue.
	i := len(rows) - 1
	for i > 0 && rows[i].off > pe {
		i--
	}
	if rows[i].off >= pe {
		rows[i].prologueEnd = true
		return rows
	}
	r := rows[i]
	r.off, r.prologueEnd = pe, true
	return append(rows[:i+1], append([]lineRow{r}, rows[i+1:]...)...)
}

// debugFrame builds .debug_frame: one CIE, and an FDE per function whose
// instructions follow the frame states the backend reported.
func (t *Table) debugFrame() *buf {
	b := &buf{}
	// CIE: on entry the CFA is RSP+8 and the return address is at CFA-8.
	b.u32(0)
	b.u32(0xffffffff)
	b.u8(3) // version
	b.str("")
	b.uleb(1)
	b.sleb(dataAlignmentFactor)
	b.uleb(amd64.DWARFReturnAddress)
	b.u8(dwCFADefCFA)
	b.uleb(uint64(amd64.DWARFReg(amd64.RSP)))
	b.uleb(8)
	b.u8(dwCFAOffset | amd64.DWARFReturnAddress)
	b.uleb(1)
	b.align(8, dwCFANop)
	b.patch32(0, uint32(len(b.b)-4))

	for _, f := range t.funcs {
		start := len(b.b)
		b.u32(0)
		b.secOff(".debug_frame", 0)
		b.addr(f.Sym, 0)
		b.u64(uint64(f.Size))
		cfiProgram(b, f.Debug.CFI)
		b.align(8, dwCFANop)
		b.patch32(start, uint32(len(b.b)-start-4))
	}
	return b
}

// cfiProgram appends the call frame instructions that move between the
// given states.
func cfiProgram(b *buf, states []amd64.FrameState) {
	prev := amd64.FrameState{CFAReg: amd64.RSP, CFAOff: 8}
	for _, s := range states {
		if d := s.Off - prev.Off; d > 0 {
			switch {
			case d < 64:
				b.u8(dwCFAAdvanceLoc | byte(d))
			case d < 1<<8:
				b.u8(dwCFAAdvanceLoc1)
				b.u8(uint8(d))
			case d < 1<<16:
				b.u8(dwCFAAdvanceLoc2)
				b.u16(uint16(d))
			default:
				b.u8(dwCFAAdvanceLoc4)
				b.u32(uint32(d))
			}
		}
		switch {
		case s.CFAReg != prev.CFAReg && s.CFAOff != prev.CFAOff:
			b.u8(dwCFADefCFA)
			b.uleb(uint64(amd64.DWARFReg(s.CFAReg)))
			b.uleb(uint64(s.CFAOff))
		case s.CFAReg != prev.CFAReg:
			b.u8(dwCFADefCFARegister)
			b.uleb(uint64(amd64.DWARFReg(s.CFAReg)))
		case s.CFAOff != prev.CFAOff:
			b.u8(dwCFADefCFAOffset)
			b.uleb(uint64(s.CFAOff))
		}
		old := map[amd64.Reg]int64{}
		for _, r := range prev.Saved {
			old[r.Reg] = r.Off
		}
		now := map[amd64.Reg]bool{}
		for _, r := range s.Saved {
			now[r.Reg] = true
			if o, ok := old[r.Reg]; !ok || o != r.Off {
				b.u8(dwCFAOffset | byte(amd64.DWARFReg(r.Reg)))
				b.uleb(uint64(r.Off / dataAlignmentFactor))
			}
		}
		for _, r := range prev.Saved {
			if !now[r.Reg] {
				b.u8(dwCFARestore | byte(amd64.DWARFReg(r.Reg)))
			}
		}
		prev = s
	}
}
