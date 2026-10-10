package opt

import "github.com/matjam/plutonium/internal/ssa"

// slotRef is a pointer value known to be a stack slot's address plus a
// constant offset.
type slotRef struct {
	slot *ssa.Slot
	off  int64
}

// slotAccess is one memory operation on a slot.
type slotAccess struct {
	v     *ssa.Value
	off   int64 // offset of the access within the slot
	size  int64
	isSrc bool // for Move: the slot is the source
}

// slotInfo collects how a slot is used.
type slotInfo struct {
	slot     *ssa.Slot
	accesses []slotAccess
	escapes  bool // its address is used other than as a load/store address
}

// analyzeSlots finds every slot whose address is only ever used directly
// as the address of loads, stores, zeroing and moves, and lists those
// accesses.
func analyzeSlots(f *ssa.Func) map[*ssa.Slot]*slotInfo {
	refs := map[*ssa.Value]slotRef{}
	var resolve func(v *ssa.Value) (slotRef, bool)
	resolve = func(v *ssa.Value) (slotRef, bool) {
		if r, ok := refs[v]; ok {
			return r, true
		}
		switch v.Op {
		case ssa.OpSlotAddr:
			r := slotRef{slot: v.Aux.(*ssa.Slot)}
			refs[v] = r
			return r, true
		case ssa.OpOffPtr:
			if r, ok := resolve(v.Args[0]); ok {
				r.off += v.AuxInt
				refs[v] = r
				return r, true
			}
		}
		return slotRef{}, false
	}

	infos := map[*ssa.Slot]*slotInfo{}
	info := func(s *ssa.Slot) *slotInfo {
		if i := infos[s]; i != nil {
			return i
		}
		i := &slotInfo{slot: s}
		infos[s] = i
		return i
	}
	for _, s := range f.Slots {
		info(s)
	}

	for _, b := range f.Blocks {
		for _, v := range b.Values {
			for i, a := range v.Args {
				r, ok := resolve(a)
				if !ok {
					continue
				}
				in := info(r.slot)
				switch {
				case v.Op == ssa.OpOffPtr && i == 0:
					// A derived address; its own uses are checked.
				case v.Op == ssa.OpLoad && i == 0:
					in.accesses = append(in.accesses, slotAccess{v: v, off: r.off, size: v.Type.Size})
				case v.Op == ssa.OpStore && i == 0:
					in.accesses = append(in.accesses, slotAccess{v: v, off: r.off, size: v.StoreType().Size})
				case v.Op == ssa.OpZero && i == 0:
					in.accesses = append(in.accesses, slotAccess{v: v, off: r.off, size: v.AuxInt})
				case v.Op == ssa.OpMove && i == 0:
					in.accesses = append(in.accesses, slotAccess{v: v, off: r.off, size: v.AuxInt})
				case v.Op == ssa.OpMove && i == 1:
					in.accesses = append(in.accesses, slotAccess{v: v, off: r.off, size: v.AuxInt, isSrc: true})
				default:
					in.escapes = true
				}
			}
		}
		for _, c := range b.Controls {
			if r, ok := resolve(c); ok {
				info(r.slot).escapes = true
			}
		}
	}
	// A volatile access pins the slot in memory.
	for _, in := range infos {
		for _, a := range in.accesses {
			if a.v.IsVolatile() {
				in.escapes = true
			}
		}
	}
	return infos
}

// slotOf returns the slot a pointer value addresses, if it is one.
func slotOf(p *ssa.Value) (*ssa.Slot, bool) {
	for p.Op == ssa.OpOffPtr {
		p = p.Args[0]
	}
	if p.Op == ssa.OpSlotAddr {
		return p.Aux.(*ssa.Slot), true
	}
	return nil, false
}

// slotAddr returns a new SlotAddr value for s, placed at the start of the
// entry block.
func slotAddr(f *ssa.Func, s *ssa.Slot) *ssa.Value {
	entry := f.Entry
	at := entry.Values[0]
	for _, v := range entry.Values {
		if v.Op != ssa.OpPhi {
			at = v
			break
		}
	}
	v := entry.NewValueBefore(at, ssa.OpSlotAddr, ssa.TPtr)
	v.Aux = s
	return v
}

// zeroConst returns a zero of type t in the entry block.
func zeroConst(f *ssa.Func, t *ssa.Type) *ssa.Value {
	if t.IsFloat() {
		c := f.ConstInt(t, 0)
		c.Op = ssa.OpConstFloat
		return c
	}
	return f.ConstInt(t, 0)
}

// dropUnusedSlots removes slots no value refers to.
func dropUnusedSlots(f *ssa.Func) {
	used := map[*ssa.Slot]bool{}
	for _, b := range f.Blocks {
		for _, v := range b.Values {
			if s, ok := v.Aux.(*ssa.Slot); ok {
				used[s] = true
			}
		}
	}
	keep := f.Slots[:0]
	for _, s := range f.Slots {
		if used[s] {
			keep = append(keep, s)
		}
	}
	f.Slots = keep
}
