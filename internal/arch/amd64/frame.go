package amd64

import (
	"sort"

	"github.com/matjam/plutonium/internal/ssa"
)

// Frame layout, without a frame pointer (the usual case):
//
//	[rsp_entry]              return address
//	                         saved callee-saved registers (pushed)
//	[rsp + frameSize ...]
//	[rsp + outArgs ...]      stack slots: locals, temporaries, spills
//	[rsp + 0 ...]            outgoing stack arguments
//
// Incoming stack argument k is at rsp + frameSize + 8*len(saved) + 8 + 8k.
//
// With over-aligned slots (more than 16 bytes) the function keeps RBP as a
// frame pointer: push rbp; mov rbp, rsp; push saved; and rsp, -align;
// sub rsp, frameSize. Incoming arguments are then at rbp + 16 + 8k.

// layoutFrame assigns slot offsets and resolves every frame-relative
// memory operand to RSP or RBP addressing.
func layoutFrame(f *MFunc) {
	outArgs := (f.outArgs + 15) &^ 15
	// Slots of 16 bytes or more are placed 16-aligned whatever their type
	// needs, so block copies and clears of them are aligned. The frame is
	// 16-aligned anyway, so this never forces a frame pointer.
	align := func(s *ssa.Slot) int64 {
		if s.Size >= 16 {
			return max(s.Align, 16)
		}
		return max(s.Align, 1)
	}
	slots := append([]*ssa.Slot(nil), f.Slots...)
	sort.SliceStable(slots, func(i, j int) bool { return align(slots[i]) > align(slots[j]) })
	off := outArgs
	maxAlign := int64(16)
	for _, s := range slots {
		al := align(s)
		maxAlign = max(maxAlign, al)
		off = (off + al - 1) &^ (al - 1)
		s.Offset = off
		off += max(s.Size, 1)
	}
	nSaved := int64(len(f.usedSaved))
	if f.frameFP {
		f.frameSize = (off + maxAlign - 1) &^ (maxAlign - 1)
	} else {
		// rsp_entry is 8 mod 16; after the pushes and the frame, rsp must
		// be 16-aligned at calls.
		size := (off + 15) &^ 15
		if (8+8*nSaved+size)%16 != 0 {
			size += 8
		}
		f.frameSize = size
	}
	f.maxAlign = maxAlign

	resolve := func(m *Mem) {
		switch {
		case m.Slot != nil && m.Base == NoReg:
			m.Base = RSP
			m.Disp += m.Slot.Offset
			m.Slot = nil
		case m.ArgSlot && m.Base == NoReg:
			if f.frameFP {
				m.Base = RBP
				m.Disp = 16 + m.ArgOff
			} else {
				m.Base = RSP
				m.Disp = f.frameSize + 8*nSaved + 8 + m.ArgOff
			}
			m.ArgSlot = false
		case m.OutArg && m.Base == NoReg:
			m.Base = RSP
			m.OutArg = false
		}
	}
	for _, b := range f.Blocks {
		for _, in := range b.Insts {
			switch in.Op {
			case MSPILL:
				m := Mem{Base: NoReg, Index: NoReg, Slot: in.Slot}
				resolve(&m)
				if f.classOf(in.Src.Reg) == ClassXMM {
					*in = MInst{Op: MMOVSF, Size: 8, Dst: MemOp(m), Src: in.Src}
				} else {
					*in = MInst{Op: MMOV, Size: 8, Dst: MemOp(m), Src: in.Src}
				}
				continue
			case MRELOAD:
				m := Mem{Base: NoReg, Index: NoReg, Slot: in.Slot}
				resolve(&m)
				if f.classOf(in.Dst.Reg) == ClassXMM {
					*in = MInst{Op: MMOVSF, Size: 8, Dst: in.Dst, Src: MemOp(m)}
				} else {
					*in = MInst{Op: MMOV, Size: 8, Dst: in.Dst, Src: MemOp(m)}
				}
				continue
			}
			if in.Dst.Kind == OpdMem {
				resolve(&in.Dst.Mem)
			}
			if in.Src.Kind == OpdMem {
				resolve(&in.Src.Mem)
			}
		}
	}
}

// prologue returns the instructions that set up the frame.
func prologue(f *MFunc) []*MInst {
	var out []*MInst
	if f.frameFP {
		out = append(out,
			&MInst{Op: MPUSH, Size: 8, Src: RegOp(RBP)},
			&MInst{Op: MMOV, Size: 8, Dst: RegOp(RBP), Src: RegOp(RSP)})
	}
	for _, r := range f.usedSaved {
		if f.frameFP && r == RBP {
			continue
		}
		out = append(out, &MInst{Op: MPUSH, Size: 8, Src: RegOp(r)})
	}
	if f.frameFP {
		out = append(out, &MInst{Op: MAND, Size: 8, Dst: RegOp(RSP), Src: ImmOp(-f.maxAlign)})
	}
	if f.frameSize > 0 {
		out = append(out, &MInst{Op: MSUB, Size: 8, Dst: RegOp(RSP), Src: ImmOp(f.frameSize)})
	}
	return out
}

// epilogue returns the instructions that tear the frame down and return.
func epilogue(f *MFunc) []*MInst {
	var out []*MInst
	saved := f.usedSaved
	if f.frameFP {
		n := 0
		for _, r := range saved {
			if r != RBP {
				n++
			}
		}
		out = append(out, &MInst{Op: MLEA, Size: 8, Dst: RegOp(RSP), Src: MemOp(BaseMem(RBP, int64(-8*n)))})
	} else if f.frameSize > 0 {
		out = append(out, &MInst{Op: MADD, Size: 8, Dst: RegOp(RSP), Src: ImmOp(f.frameSize)})
	}
	for i := len(saved) - 1; i >= 0; i-- {
		if f.frameFP && saved[i] == RBP {
			continue
		}
		out = append(out, &MInst{Op: MPOP, Size: 8, Dst: RegOp(saved[i])})
	}
	if f.frameFP {
		out = append(out, &MInst{Op: MPOP, Size: 8, Dst: RegOp(RBP)})
	}
	return append(out, &MInst{Op: MRET})
}

// insertFrame adds the prologue and expands each epilogue pseudo-op.
func insertFrame(f *MFunc) {
	entry := f.Blocks[0]
	entry.Insts = append(prologue(f), entry.Insts...)
	for _, b := range f.Blocks {
		var out []*MInst
		for _, in := range b.Insts {
			switch in.Op {
			case MEPILOGUE:
				out = append(out, epilogue(f)...)
			case MDEF, MUSE:
				// no code
			default:
				out = append(out, in)
			}
		}
		b.Insts = out
	}
}
