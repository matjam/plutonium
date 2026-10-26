package opt

import (
	"github.com/matjam/plutonium/internal/ssa"
)

// Mem2Reg promotes stack slots that hold a single scalar, and whose address
// never escapes, to SSA values: loads become the value last stored, stores
// disappear, and phis are placed at the iterated dominance frontiers of the
// stores (Cytron et al.).
//
// The function must be scheduled: within a block, memory operations appear
// in program order.
func Mem2Reg(f *ssa.Func) {
	infos := analyzeSlots(f)
	var promote []*slotInfo
	types := map[*ssa.Slot]*ssa.Type{}
	for _, s := range f.Slots {
		in := infos[s]
		if in == nil || in.escapes {
			continue
		}
		if t := scalarSlotType(in); t != nil {
			types[s] = t
			promote = append(promote, in)
		}
	}
	if len(promote) == 0 {
		return
	}

	dom := f.Dom()
	df := dom.Frontiers(f)

	// Which slot does each access touch?
	accessSlot := map[*ssa.Value]*ssa.Slot{}
	for _, in := range promote {
		for _, a := range in.accesses {
			accessSlot[a.v] = in.slot
		}
	}

	// Phi placement.
	phiSlot := map[*ssa.Value]*ssa.Slot{}
	phiArgs := map[*ssa.Value][]*ssa.Value{}
	var phis []*ssa.Value // creation order, for a deterministic fill below
	for _, in := range promote {
		s := in.slot
		var work []*ssa.Block
		hasDef := map[*ssa.Block]bool{}
		for _, a := range in.accesses {
			if a.v.Op != ssa.OpLoad && !hasDef[a.v.Block] {
				hasDef[a.v.Block] = true
				work = append(work, a.v.Block)
			}
		}
		hasPhi := map[*ssa.Block]bool{}
		for len(work) > 0 {
			b := work[len(work)-1]
			work = work[:len(work)-1]
			for _, y := range df[b.ID] {
				if hasPhi[y] {
					continue
				}
				hasPhi[y] = true
				phi := newPhiAtStart(y, types[s])
				phiSlot[phi] = s
				phiArgs[phi] = make([]*ssa.Value, len(y.Preds))
				phis = append(phis, phi)
				if !hasDef[y] {
					hasDef[y] = true
					work = append(work, y)
				}
			}
		}
	}

	// Renaming along the dominator tree. cur holds each slot's value on
	// the path from the entry; undo logs the values it replaced, so that
	// leaving a block puts them back without copying cur for every block.
	cur := map[*ssa.Slot]*ssa.Value{}
	var undo []slotUndo
	set := func(s *ssa.Slot, v *ssa.Value) {
		undo = append(undo, slotUndo{s, cur[s]})
		cur[s] = v
	}
	// One zero constant per type serves every read before a store: it is
	// in the entry block, which dominates them all. Making one per read
	// was quadratic, since each is inserted at the start of the entry.
	zeros := map[*ssa.Type]*ssa.Value{}
	zero := func(t *ssa.Type) *ssa.Value {
		z := zeros[t]
		if z == nil {
			z = zeroConst(f, t)
			zeros[t] = z
		}
		return z
	}
	current := func(s *ssa.Slot) *ssa.Value {
		if v := cur[s]; v != nil {
			return v
		}
		// Read before any store on this path: the zero value.
		z := zero(types[s])
		set(s, z)
		return z
	}
	var vals []*ssa.Value
	var rename func(b *ssa.Block)
	rename = func(b *ssa.Block) {
		mark := len(undo)
		// zeroConst may add values to b, so walk a copy of its values.
		vals = append(vals[:0], b.Values...)
		for _, v := range vals {
			if s, ok := phiSlot[v]; ok {
				set(s, v)
				continue
			}
			s, ok := accessSlot[v]
			if !ok {
				continue
			}
			switch v.Op {
			case ssa.OpLoad:
				v.CopyOf(current(s))
			case ssa.OpStore:
				set(s, v.Args[1])
				v.CopyOf(v.Args[2])
			case ssa.OpZero:
				set(s, zero(types[s]))
				v.CopyOf(v.Args[1])
			}
		}
		for _, e := range b.Succs {
			for _, v := range e.B.Values {
				if v.Op != ssa.OpPhi {
					break
				}
				if s, ok := phiSlot[v]; ok {
					phiArgs[v][e.I] = current(s)
				}
			}
		}
		for _, c := range dom.Children(b) {
			rename(c)
		}
		for len(undo) > mark {
			u := undo[len(undo)-1]
			undo = undo[:len(undo)-1]
			if u.v == nil {
				delete(cur, u.s)
			} else {
				cur[u.s] = u.v
			}
		}
	}
	rename(f.Entry)

	for _, phi := range phis {
		for _, a := range phiArgs[phi] {
			if a == nil {
				// Predecessor not reached by renaming (unreachable).
				a = zero(phi.Type)
			}
			phi.AddArg(a)
		}
	}
	ssa.CopyElim(f)
	ssa.DeadCode(f)
	dropUnusedSlots(f)
}

// scalarSlotType returns the scalar type held in a slot if every access is a
// load or store of that type at offset 0 covering the slot, or a Zero of it.
func scalarSlotType(in *slotInfo) *ssa.Type {
	var t *ssa.Type
	for _, a := range in.accesses {
		if a.off != 0 || a.size != in.slot.Size {
			return nil
		}
		var at *ssa.Type
		switch a.v.Op {
		case ssa.OpLoad:
			at = a.v.Type
		case ssa.OpStore:
			at = a.v.StoreType()
		case ssa.OpZero:
			continue
		default:
			return nil
		}
		if t == nil {
			t = at
		} else if !t.Same(at) {
			return nil
		}
	}
	if t == nil && len(in.slot.Leaves) == 1 {
		t = in.slot.Leaves[0].Type
	}
	return t
}

// newPhiAtStart adds an argument-less phi at the start of b.
func newPhiAtStart(b *ssa.Block, t *ssa.Type) *ssa.Value {
	if len(b.Values) == 0 {
		return b.NewValue(ssa.OpPhi, t)
	}
	return b.NewValueBefore(b.Values[0], ssa.OpPhi, t)
}

// A slotUndo is a slot's value before Mem2Reg's renaming replaced it.
type slotUndo struct {
	s *ssa.Slot
	v *ssa.Value
}
