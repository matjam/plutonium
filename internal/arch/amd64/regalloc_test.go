package amd64

import (
	"fmt"
	"testing"
)

// TestRewriteSpillOrder checks that an instruction defining several spilled
// registers gets its spill stores in definition order, so that the output
// does not depend on map iteration order. Each round builds a fresh function
// so that a map-ordered rewrite would almost surely differ in some round.
func TestRewriteSpillOrder(t *testing.T) {
	const n = 8
	for round := range 20 {
		f := newMFunc("t")
		var defs []Reg
		for range n {
			defs = append(defs, f.newVReg(ClassGP))
		}
		def := &MInst{Op: MDEF, ImplicitDefs: append([]Reg(nil), defs...)}
		f.Blocks = []*MBlock{{Insts: []*MInst{def}}}
		a := newAllocator(f, ClassGP, map[Reg]bool{})
		a.spilled = defs
		a.rewrite(map[Reg]bool{})

		insts := f.Blocks[0].Insts
		if len(insts) != 1+n || insts[0] != def {
			t.Fatalf("round %d: got %d instructions, want the def and %d spills", round, len(insts), n)
		}
		for i, in := range insts[1:] {
			want := fmt.Sprintf("spill%d", defs[i]-FirstVirtual)
			if in.Op != MSPILL || in.Slot.Name != want || in.Src.Reg != def.ImplicitDefs[i] {
				t.Fatalf("round %d: spill %d is %s to %s from %s, want %s from %s",
					round, i, in.Op, in.Slot.Name, in.Src.Reg, want, def.ImplicitDefs[i])
			}
		}
	}
}
