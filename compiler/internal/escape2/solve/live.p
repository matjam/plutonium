// Translated by go2pu from internal/escape2/solve/live.go at the go-cutover tag; edited by hand since.

package solve

import (
	"pluto/bits"
	"compiler/internal/escape2/eir"
)

// checkDrops is the OpDrop rule (rules.go): a register live after the drop
// of a statement temporary must not hold a pointer to it (E3009). Liveness
// is the usual backward analysis over the CFG, run only when the function
// drops a statement temporary.
func (s *solver) checkDrops() {
	if len(s.drops.s) == 0 {
		return
	}
	f := s.f
	words := (f.NumRegs + 64) / 64
	nb := len(f.Blocks)
	var liveIn, liveOut vec
	liveIn.resize(s.a, nb*words)
	liveOut.resize(s.a, nb*words)
	in := func(b int) []uint64 { return liveIn.s[b*words : (b+1)*words] }
	out := func(b int) []uint64 { return liveOut.s[b*words : (b+1)*words] }
	var cur vec
	cur.resize(s.a, words)
	for changed := true; changed; {
		changed = false
		for b := nb - 1; b >= 0; b-- {
			blk := f.Blocks[b]
			o := out(b)
			for _, succ := range blk.Succs {
				orInto(o, in(int(succ)))
				s.phiUses(o, blk.ID, f.Blocks[succ])
			}
			copy(cur.s, o)
			for i := len(blk.Instrs) - 1; i >= 0; i-- {
				transfer(cur.s, &blk.Instrs[i])
			}
			if !equal(cur.s, in(b)) {
				copy(in(b), cur.s)
				changed = true
			}
		}
	}
	for _, d := range s.drops.s {
		b, at := int(d>>32), int(uint32(d))
		blk := f.Blocks[b]
		copy(cur.s, out(b))
		for i := len(blk.Instrs) - 1; i > at; i-- {
			transfer(cur.s, &blk.Instrs[i])
		}
		drop := &blk.Instrs[at]
		o := uint32(drop.Obj)
		why := s.why(drop.Pos, whyOwn, 0, 0, 0, 0)
		for w, word := range cur.s {
			for word != 0 {
				r := w*64 + bits.TrailingZeros64(word)
				word &= word - 1
				if s.holds(uint32(r), o) {
					s.report(cE3009, o, why)
				}
			}
		}
	}
}

// holds reports whether register r may hold a pointer into object o.
func (s *solver) holds(r, o uint32) bool {
	if r == 0 || int(r) >= len(s.nodeRep.s) {
		return false
	}
	for _, id := range s.pts(s.find(r)) {
		if t := s.tokAt(id); t.kind == tObj && t.base == o {
			return true
		}
	}
	return false
}

// phiUses marks the phi arguments of succ that come from block pred.
func (s *solver) phiUses(live []uint64, pred eir.BlockID, succ *eir.Block) {
	for k, p := range succ.Preds {
		if p != pred {
			continue
		}
		for i := range succ.Instrs {
			in := &succ.Instrs[i]
			if in.Op != eir.OpPhi {
				break
			}
			if k < len(in.Args) {
				set(live, in.Args[k])
			}
		}
	}
}

// transfer moves live backward over one instruction: its definitions die,
// its uses become live. A phi's uses belong to its predecessors.
func transfer(live []uint64, in *eir.Instr) {
	unset(live, in.Dst)
	if in.Call != nil {
		for _, r := range in.Call.Results {
			unset(live, r)
		}
	}
	if in.Op == eir.OpPhi {
		return
	}
	set(live, in.X)
	set(live, in.Y)
	for _, a := range in.Args {
		set(live, a)
	}
	if in.Call != nil {
		set(live, in.Call.Target)
		for _, a := range in.Call.Args {
			set(live, a)
		}
	}
}

func set(live []uint64, r eir.Reg) {
	if r != 0 && int(r/64) < len(live) {
		live[r/64] |= 1 << (r % 64)
	}
}

func unset(live []uint64, r eir.Reg) {
	if r != 0 && int(r/64) < len(live) {
		live[r/64] &^= 1 << (r % 64)
	}
}

func orInto(dst, src []uint64) {
	for i := range dst {
		dst[i] |= src[i]
	}
}

func equal(a, b []uint64) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
