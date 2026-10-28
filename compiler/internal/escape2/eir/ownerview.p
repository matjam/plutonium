package eir

import (
	"compiler/compat"
	"compiler/internal/types2"
)

// Views of a local owner's memory (section 18.3, "A local owner's
// memory"; section 18.9). The memory an owner holds (a vector's buffer, a
// list's nodes) is released when the owner is dropped, so a view of it is
// bounded by the owner, as a view of the owner's own storage is. The
// analysis follows pointers for the frame rules only, and a field that
// holds nothing but heap memory loads as heap memory (the heap-only
// fields of sched): what v.Slice() returns would be a pointer to the heap.
//
// So the caller adds the bound. Each addressed frame variable that holds
// an owner has an ObjBoxView object of its lifetime (Func.OwnerMem): the
// memory the variable owns, a frame pointer for where a view of it goes
// and heap memory for what is stored in it and loaded from it. A local
// checked arena's memory is its Func.ArenaMem object. A static call that
// passes a pointer to an owner (or arena) gets, in each result that may
// point into the owner's memory, a view of that memory too (OpOwnerView of
// the argument): a pointer to a variable with such memory becomes one to
// its memory, a pointer to a box view stays, and other pointers add
// nothing (the callee's summary says what memory of a caller its results
// hold).
//
// Which results may point into the owner's memory is decided by type, on
// the declared (generic) function: a result's pointer, slice or
// unsafe.Pointer parts against the pointer and slice types by which a
// value can point into memory the owner's fields reach. A type parameter
// is opaque on both sides: an element of a Vec[*T] that At or Pop copies
// out is the vector's element, not a view of its buffer. Of a checked
// arena's methods, New, Slice and Alloc hand out views of its memory.
//
// When the call passes the address of the variable itself (or of a field
// of it), the heap or caller memory such a result points to is taken to
// be the owner's memory too (OpOwnerHeap): what the owner reaches through
// its fields is its own, even when its allocator came from a caller. A
// view of the owner stored there, as a directory listing stores views of
// its names in its entries, stays in what it views.
//
// A local owner loaded whole is moved, and its memory goes with it
// (OpOwnerMove): views of that memory in the value loaded are heap
// pointers from there on, as a constructor that keeps a view of a vector
// in a field of the value it returns.

// ownerMove returns r, the value of type T loaded from x, with the views
// of the memory a local owner owns made heap pointers when x is the
// address of that owner, loaded whole: a move, which takes the memory
// with it (section 18.3). A view of the memory kept in the owner's own
// fields, as Args in an exec.Cmd, goes with it.
func (fb *fnb) ownerMove(x Reg, T types2.Type, r Reg) Reg {
	if len(fb.f.OwnerMem) == 0 || fb.cur == nil {
		return r
	}
	o := NoObj
	if in := defIn(fb.cur.b, x); in != nil && in.Op == OpAddr && len(in.Path) == 0 {
		o = in.Obj
	}
	mem, ok := fb.f.OwnerMem[o]
	if !ok || !types2.Identical(T, fb.f.Objects[o].Type) {
		return r
	}
	return fb.emitDst(Instr{Op: OpOwnerMove, X: r, Obj: mem, Pos: fb.pos})
}

// ownsMemory reports whether a frame variable of type T gets an owner
// memory object: it holds an owner (or counted references, which are
// harmless here), and is neither a box (whose object is its box view), a
// checked arena (whose memory is Func.ArenaMem) nor a map walk.
func (b *builder) ownsMemory(T types2.Type) bool {
	return !isCheckedArena(T) && !isBox(T) && !isMapWalk(T) && b.needsDrop(T)
}

// isMapWalk reports whether T is rt.MapIter, the walk a map's range loop
// and All, Keys and Values keep in a local. It owns no memory of its own:
// what it returns points into the map's table, a view of the map.
func isMapWalk(T types2.Type) bool {
	n, ok := types2.Unalias(T).(*types2.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == "pluto/rt" && n.Obj().Name() == "MapIter"
}

// ownerCall adds to the results res of the call c the views of the
// memory of the owners c passes by address.
func (fb *fnb) ownerCall(c *Call, res []Reg) []Reg {
	if fb.cur == nil || c == nil || c.Kind != CallStatic || c.Sig == nil || len(res) == 0 {
		return res
	}
	if c.Callee == NoFunc || int(c.Callee) >= len(fb.b.p.Funcs) {
		return res
	}
	fn := fb.b.p.Funcs[c.Callee].Key.Obj
	if fn == nil {
		return res
	}
	decl, _ := fn.Type().(*types2.Signature)
	if decl == nil || decl.Results().Len() != len(res) {
		return res
	}
	params := sigParams(c.Sig)
	declParams := sigParams(decl)
	if len(params) != len(declParams) {
		return res
	}
	out := res
	copied := false
	for j, a := range c.Args {
		if a == 0 || j >= len(params) {
			continue
		}
		O := pointerElem(params[j])
		if O == nil || isBox(params[j]) {
			continue
		}
		arena := isCheckedArena(O)
		if !arena && !types2.IsOwner(O) {
			continue
		}
		var owned []types2.Type
		if !arena {
			owned = ownedLeaves(pointerElem(declParams[j]))
			if len(owned) == 0 {
				continue
			}
		}
		view := Reg(0)
		mem := NoObj
		for i, r := range out {
			if r == 0 {
				continue
			}
			if arena {
				if j != 0 || c.Sig.Recv() == nil {
					continue
				}
				switch fn.Name() {
				case "New", "Slice", "Alloc":
				default:
					continue
				}
			} else if !mayPointInto(decl.Results().At(i).Type(), owned) {
				continue
			}
			if view == 0 {
				view = fb.emitDst(Instr{Op: OpOwnerView, X: a, Pos: fb.pos})
				if !arena && len(fb.f.OwnerMem) > 0 {
					if m, ok := fb.f.OwnerMem[fb.frameObjOf(a)]; ok {
						mem = m
					}
				}
			}
			if !copied {
				out = compat.AppendPlain([]Reg(nil), res...)
				copied = true
			}
			if mem != NoObj {
				// The heap memory the method reaches through the owner is
				// the owner's: a view of it stored there stays in it.
				r = fb.emitDst(Instr{Op: OpOwnerHeap, X: r, Obj: mem, Pos: fb.pos})
			}
			out[i] = fb.union(r, compat.Clone([]Reg{view}), c.Sig.Results().At(i).Type())
		}
	}
	return out
}

// frameObjOf returns the frame object x points into when x is its
// address (or a field's), else NoObj.
func (fb *fnb) frameObjOf(x Reg) ObjID {
	for {
		in := fb.defOf(x)
		if in == nil {
			return NoObj
		}
		switch in.Op {
		case OpAddr:
			return in.Obj
		case OpField, OpCopy, OpLend:
			x = in.X
			continue
		}
		return NoObj
	}
}

// defOf returns the instruction that defines x, or nil.
func (fb *fnb) defOf(x Reg) *Instr {
	if x == 0 {
		return nil
	}
	if fb.cur != nil {
		if in := defIn(fb.cur.b, x); in != nil {
			return in
		}
	}
	for _, b := range fb.f.Blocks {
		if in := defIn(b, x); in != nil {
			return in
		}
	}
	return nil
}

// defIn returns the instruction of block b that defines x, or nil.
func defIn(b *Block, x Reg) *Instr {
	if b == nil {
		return nil
	}
	for i := len(b.Instrs) - 1; i >= 0; i-- {
		if b.Instrs[i].Dst == x {
			return &b.Instrs[i]
		}
	}
	return nil
}

// sigParams returns the receiver's and parameters' types of sig, in the
// order of a static call's arguments.
func sigParams(sig *types2.Signature) []types2.Type {
	var out []types2.Type
	if r := sig.Recv(); r != nil {
		out = compat.Append(out, r.Type())
	}
	for i := 0; i < sig.Params().Len(); i++ {
		out = compat.Append(out, sig.Params().At(i).Type())
	}
	return out
}

// mayPointInto reports whether a value of type T may point into memory
// reached by the pointer and slice types owned.
func mayPointInto(T types2.Type, owned []types2.Type) bool {
	for _, r := range valueLeaves(T, 0) {
		if isUnsafePointer(r) {
			return true
		}
		for _, o := range owned {
			if types2.Identical(r, o) {
				return true
			}
		}
	}
	return false
}

// valueLeaves returns the pointer, slice and unsafe.Pointer types a value
// of type T holds by value (in its fields and elements), leaving out
// owners (a new value, not a view), type parameters, interfaces, func
// values, boxes and counted values.
func valueLeaves(T types2.Type, depth int) []types2.Type {
	T = types2.Unalias(T)
	if T == nil || depth > 8 || types2.IsOwner(T) || isTypeParam(T) {
		return nil
	}
	var out []types2.Type
	switch u := unwrap(T).Underlying().(type) {
	case *types2.Basic:
		if u.Kind() == types2.UnsafePointer {
			out = compat.Append(out, T)
		}
	case *types2.Pointer, *types2.Slice:
		out = compat.Append(out, T)
	case *types2.Array:
		out = valueLeaves(u.Elem(), depth+1)
	case *types2.Struct:
		for i := 0; i < u.NumFields(); i++ {
			out = compat.Append(out, valueLeaves(u.Field(i).Type(), depth+1)...)
		}
	case *types2.TaggedUnion:
		for i := 0; i < u.NumVariants(); i++ {
			if v := u.Variant(i); v.HasPayload() {
				out = compat.Append(out, valueLeaves(v.Payload(), depth+1)...)
			}
		}
	}
	return out
}

// ownedLeaves returns the pointer and slice types by which a value can
// point into memory reached from the fields of an owner of type T: the
// pointer and slice fields, at any depth through what they point to,
// pointers to every part of the objects they reach, and slices of their
// arrays. A type parameter is not looked into.
func ownedLeaves(T types2.Type) []types2.Type {
	var out []types2.Type
	seen := map[string]bool{}
	var obj func(X types2.Type, top bool)
	obj = func(X types2.Type, top bool) {
		X = types2.Unalias(X)
		if X == nil {
			return
		}
		k := types2.TypeString(X, nil)
		if seen[k] {
			return
		}
		seen[k] = true
		if !top {
			out = compat.Append(out, types2.Type(types2.NewPointer(X)))
		}
		if isTypeParam(X) {
			return
		}
		switch u := unwrap(X).Underlying().(type) {
		case *types2.Pointer:
			out = compat.Append(out, X)
			obj(u.Elem(), false)
		case *types2.Slice:
			out = compat.Append(out, X)
			obj(u.Elem(), false)
		case *types2.Array:
			out = compat.Append(out, types2.Type(types2.NewSlice(u.Elem())))
			obj(u.Elem(), top)
		case *types2.Struct:
			for i := 0; i < u.NumFields(); i++ {
				obj(u.Field(i).Type(), top)
			}
		case *types2.Wrapper:
			if u.IsBox() {
				obj(u.Elem(), false)
			}
		case *types2.TaggedUnion:
			for i := 0; i < u.NumVariants(); i++ {
				if v := u.Variant(i); v.HasPayload() {
					obj(v.Payload(), top)
				}
			}
		}
	}
	obj(T, true)
	return out
}
