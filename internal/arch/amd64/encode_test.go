package amd64

import (
	"bytes"
	"testing"

	"github.com/matjam/plutonium/internal/ssa"
)

// TestBranchRelaxation checks that near branches get the 2-byte forms, far
// ones keep rel32, and code and relocations after a shortened branch move
// with it.
func TestBranchRelaxation(t *testing.T) {
	b0 := &MBlock{ID: 0}
	b1 := &MBlock{ID: 1}
	b2 := &MBlock{ID: 2}
	b3 := &MBlock{ID: 3}
	sym := &ssa.Sym{Name: "callee"}
	// b0: jl b2 (near); jmp b3 (far, past 64 ud2s)
	b0.Insts = []*MInst{
		{Op: MJCC, Cond: CondL, Target: b2},
		{Op: MJMP, Target: b3},
	}
	// b1: call callee; jmp b0 (backwards, near)
	b1.Insts = []*MInst{
		{Op: MCALL, Sym: sym},
		{Op: MJMP, Target: b0},
	}
	b2.Insts = []*MInst{{Op: MRET}}
	for range 64 {
		b2.Insts = append(b2.Insts, &MInst{Op: MUD2})
	}
	b3.Insts = []*MInst{{Op: MRET}}

	code, err := encodeFunc(&MFunc{}, []*MBlock{b0, b1, b2, b3})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0x7C, 0x0C, // jl b2 (+12)
		0xE9, 0x88, 0x00, 0x00, 0x00, // jmp b3 (+136)
		0xE8, 0, 0, 0, 0, // call callee
		0xEB, 0xF2, // jmp b0 (-14)
		0xC3, // b2: ret
	}
	if !bytes.Equal(code.Bytes[:len(want)], want) {
		t.Errorf("code % x\nwant % x", code.Bytes[:len(want)], want)
	}
	if got := len(code.Bytes); got != len(want)+64*2+1 {
		t.Errorf("length %d, want %d", got, len(want)+64*2+1)
	}
	if len(code.Relocs) != 1 || code.Relocs[0].Off != 8 {
		t.Errorf("relocs %+v, want one at offset 8", code.Relocs)
	}
}
