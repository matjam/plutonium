package ssatext_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/ssatext"
	"github.com/matjam/plutonium/internal/ssawrite"
	"github.com/matjam/plutonium/internal/syntax/token"
)

// program builds a program that uses every part of the form: a loop, a slot
// with leaves, pads and parts, a tuple type, a call with its info, an
// intrinsic, a string hold that refers to a value no block holds, data with
// relocations, and positions in a file with a line directive.
func program() (*ssa.Program, *token.FileSet) {
	fset := token.NewFileSet()
	file := fset.AddFile("a.p", -1, 100)
	file.SetLines([]int{0, 10, 20, 30})
	file.AddLineColumnInfo(20, "b.p", 7, 3)

	p := ssa.NewProgram()
	f := ssa.NewFunc("demo.f")
	f.Sym = p.Sym("demo.f", ssa.SymFunc)
	f.Params = []*ssa.Type{ssa.TI64}
	f.Results = []*ssa.Type{ssa.TI64}
	f.Pos, f.End, f.Pkg = file.Pos(5), file.Pos(40), "demo"
	f.Exports = []string{"f_export"}
	f.SetPos(file.Pos(12))
	entry := f.Entry
	mem := entry.NewValue(ssa.OpInitMem, ssa.TMem)
	n := entry.NewValueI(ssa.OpArg, ssa.TI64, 0)
	slot := f.NewSlot("v", 16, 8)
	slot.Leaves = []ssa.Leaf{{Off: 0, Type: ssa.TI64}}
	slot.Pads = []ssa.Pad{{Off: 8, Size: 8}}
	slot.Parts = []ssa.Part{{Off: 0, Size: 8, Align: 8, Closure: true, Leaves: []ssa.Leaf{{Off: 0, Type: ssa.TI64}}}}
	addr := entry.NewValueA(ssa.OpSlotAddr, ssa.TPtr, slot)
	m1 := entry.NewValueA(ssa.OpStore, ssa.TMem, ssa.TI64, addr, n, mem)
	callee := p.Sym("demo.g", ssa.SymExtern)
	tup := ssa.TupleOf(ssa.TMem, ssa.TI64)
	call := entry.NewValueA(ssa.OpCall, tup, callee, n, m1)
	call.Call = &ssa.CallInfo{Results: []*ssa.Type{ssa.TI64}, NoReturn: false, OnStack: []bool{false}}
	call.Pos = file.Pos(21)
	r := entry.NewValueI(ssa.OpSelectN, ssa.TI64, 1, call)
	cm := entry.NewValueI(ssa.OpSelectN, ssa.TMem, 0, call)
	intr := entry.NewValueA(ssa.OpIntrinsic, ssa.TupleOf(ssa.TMem), ssa.NewIntrinsic("x86.Cli"), r, cm)
	im := entry.NewValueI(ssa.OpSelectN, ssa.TMem, 0, intr)
	dead := entry.Func.Entry.NewValueA(ssa.OpCall, tup, callee)
	dead.Call = &ssa.CallInfo{}
	entry.Values = entry.Values[:len(entry.Values)-1] // in no block
	f.StrHolds = []ssa.StrHold{{Retains: []*ssa.Value{call}, Covered: []*ssa.Value{dead}}}
	header := f.NewBlock(ssa.BlockIf)
	body := f.NewBlock(ssa.BlockPlain)
	exit := f.NewBlock(ssa.BlockRet)
	header.Likely = ssa.Likely
	entry.AddEdgeTo(header)
	header.AddEdgeTo(body)
	header.AddEdgeTo(exit)
	body.AddEdgeTo(header)
	i := header.NewValue(ssa.OpPhi, ssa.TI64)
	header.SetControl(header.NewValue(ssa.OpLess, ssa.TBool, i, r))
	inext := body.NewValue(ssa.OpAdd, ssa.TI64, i, f.ConstInt(ssa.TI64, 1))
	i.AddArg(r)
	i.AddArg(inext)
	exit.SetControl(im, i)
	f.Inlines = []ssa.InlineSite{{Func: "demo.h", Pos: file.Pos(25), Start: file.Pos(1), End: file.Pos(9)}}
	p.Funcs = append(p.Funcs, f)

	p.Data = []*ssa.Data{
		{Sym: p.Sym("demo.d", ssa.SymData), Size: 16, Align: 8, Bytes: []byte{1, 2, 3}, Section: ".data.x",
			Relocs: []ssa.DataReloc{{Off: 8, Sym: callee, Addend: -4}}},
		{Sym: p.Sym("demo.z", ssa.SymData), Size: 8, Align: 8, ReadOnly: true, Local: true, Bytes: []byte{}},
	}
	p.Sym("pluto.percpu.self", ssa.SymData).PerCPU = true
	return p, fset
}

func write(t *testing.T, p *ssa.Program, fset *token.FileSet) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := ssawrite.Write(&buf, p, fset); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestRoundTrip writes a program, reads it back and writes it again: the two
// texts are the same.
func TestRoundTrip(t *testing.T) {
	p, fset := program()
	text := write(t, p, fset)
	q, fset2, err := ssatext.Read(text)
	if err != nil {
		t.Fatal(err)
	}
	if got := write(t, q, fset2); !bytes.Equal(got, text) {
		t.Errorf("the text changed\n--- first ---\n%s\n--- second ---\n%s", text, got)
	}
	if err := q.Funcs[0].Verify(); err != nil {
		t.Error(err)
	}
	if pos := fset2.Position(q.Funcs[0].Entry.Values[0].Pos); pos.Line == 0 {
		t.Errorf("a position was lost: %v", pos)
	}
	for _, v := range q.Funcs[0].Entry.Values {
		if v.Op == ssa.OpCall {
			if got := fset2.Position(v.Pos); got.Filename != "b.p" || got.Line != 7 {
				t.Errorf("the line directive was lost: %v", got)
			}
		}
	}
}

// TestMalformed cuts the text short at every length and damages it in
// several ways: reading must report an error and never panic.
func TestMalformed(t *testing.T) {
	p, fset := program()
	text := write(t, p, fset)
	for n := 0; n < len(text); n++ {
		if _, _, err := ssatext.Read(text[:n]); err == nil {
			t.Fatalf("a text cut at %d bytes was accepted", n)
		}
	}
	s := string(text)
	for _, c := range []struct{ old, new string }{
		{"ssatext 1", "ssatext 2"},
		{"\nops ", "\nops 9"},
		{"op 3 ", "op 4 "},
		{"\nsym 0 0 ", "\nsym 0 2 "},
		{"\nv 0 ", "\nv 99999 "},
		{"endfunc", "endfunc\nx"},
		{" i64", " i65"},
		{" #0", " #9"},
		{"x86.Cli", "x86.Cli\\q"},
	} {
		if !strings.Contains(s, c.old) {
			t.Fatalf("the text has no %q", c.old)
		}
		bad := strings.Replace(s, c.old, c.new, 1)
		if _, _, err := ssatext.Read([]byte(bad)); err == nil {
			t.Errorf("%q to %q was accepted", c.old, c.new)
		}
	}
}
