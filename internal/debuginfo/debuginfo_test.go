package debuginfo

import (
	"bytes"
	"testing"

	"github.com/matjam/plutonium/internal/arch/amd64"
	"github.com/matjam/plutonium/internal/syntax/token"
)

func TestLEB128(t *testing.T) {
	for _, c := range []struct {
		v    int64
		u, s []byte
	}{
		{0, []byte{0}, []byte{0}},
		{2, []byte{2}, []byte{2}},
		{127, []byte{0x7f}, []byte{0xff, 0}},
		{128, []byte{0x80, 1}, []byte{0x80, 1}},
		{624485, []byte{0xe5, 0x8e, 0x26}, []byte{0xe5, 0x8e, 0x26}},
	} {
		var u, s buf
		u.uleb(uint64(c.v))
		s.sleb(c.v)
		if !bytes.Equal(u.b, c.u) || !bytes.Equal(s.b, c.s) {
			t.Errorf("%d: uleb % x sleb % x, want % x and % x", c.v, u.b, s.b, c.u, c.s)
		}
	}
	for v, want := range map[int64][]byte{-1: {0x7f}, -2: {0x7e}, -128: {0x80, 0x7f}, -123456: {0xc0, 0xbb, 0x78}} {
		var s buf
		s.sleb(v)
		if !bytes.Equal(s.b, want) {
			t.Errorf("sleb(%d) = % x, want % x", v, s.b, want)
		}
	}
	for v, want := range map[int64]uint64{0: 0, -1: 1, 1: 2, -2: 3, 1000: 2000} {
		if got := zigzag(v); got != want {
			t.Errorf("zigzag(%d) = %d, want %d", v, got, want)
		}
	}
}

// TestCFIProgram checks the instructions that describe a prologue with a
// push and a frame, an epilogue, and the body state after the return.
func TestCFIProgram(t *testing.T) {
	saved := []amd64.RegSave{{Reg: amd64.RBX, Off: -16}}
	states := []amd64.FrameState{
		{Off: 0, CFAReg: amd64.RSP, CFAOff: 8},
		{Off: 1, CFAReg: amd64.RSP, CFAOff: 16, Saved: saved},
		{Off: 5, CFAReg: amd64.RSP, CFAOff: 48, Saved: saved},
		{Off: 100, CFAReg: amd64.RSP, CFAOff: 16, Saved: saved},
		{Off: 101, CFAReg: amd64.RSP, CFAOff: 8},
		{Off: 102, CFAReg: amd64.RSP, CFAOff: 48, Saved: saved},
		{Off: 400, CFAReg: amd64.RBP, CFAOff: 16, Saved: saved},
	}
	var b buf
	cfiProgram(&b, states)
	want := []byte{
		0x41, 0x0e, 16, 0x83, 2, // advance 1; def_cfa_offset 16; rbx at cfa-16
		0x44, 0x0e, 48, // advance 4; def_cfa_offset 48
		0x02, 95, 0x0e, 16, // advance_loc1 95; def_cfa_offset 16
		0x41, 0x0e, 8, 0xc3, // advance 1; def_cfa_offset 8; restore rbx
		0x41, 0x0e, 48, 0x83, 2, // advance 1; def_cfa_offset 48; rbx at cfa-16
		0x03, 0x2a, 0x01, 0x0c, 6, 16, // advance_loc2 298; def_cfa rbp+16
	}
	if !bytes.Equal(b.b, want) {
		t.Errorf("CFI\n% x\nwant\n% x", b.b, want)
	}
}

// TestLineRows checks where the prologue is marked to end: at the first
// line of the body, or at the end of the frame setup for a function that
// is all on one line.
func TestLineRows(t *testing.T) {
	fset := token.NewFileSet()
	file := fset.AddFile("a.p", -1, 100)
	for i := 0; i < 10; i++ {
		file.AddLine(i * 10)
	}
	line := func(n int) token.Pos { return file.Pos((n - 1) * 10) }
	tab := New(fset)

	f := &Func{Pos: line(3), Size: 40, Debug: &amd64.FuncDebug{PrologueEnd: 4, Lines: []amd64.LineEntry{
		{Off: 0, Pos: line(3)}, {Off: 10, Pos: line(4)}, {Off: 20, Pos: line(4) + 2}, {Off: 30, Pos: line(5)},
	}}}
	rows := tab.lineRows(f)
	if len(rows) != 3 || rows[1].off != 10 || !rows[1].prologueEnd || rows[0].prologueEnd {
		t.Errorf("body rows %+v", rows)
	}

	oneLine := &Func{Pos: line(7), Size: 20, Debug: &amd64.FuncDebug{PrologueEnd: 4, Lines: []amd64.LineEntry{{Off: 0, Pos: line(7)}}}}
	rows = tab.lineRows(oneLine)
	if len(rows) != 2 || rows[1].off != 4 || !rows[1].prologueEnd || rows[1].line != 7 {
		t.Errorf("one-line rows %+v", rows)
	}

	generated := &Func{Size: 20, Debug: &amd64.FuncDebug{}}
	if rows := tab.lineRows(generated); len(rows) != 0 {
		t.Errorf("generated rows %+v", rows)
	}
}
