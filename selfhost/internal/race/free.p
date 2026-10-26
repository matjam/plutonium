// Translated by go2pu from internal/race/free.go at the go-cutover tag; edited by hand since.

package race

import (
	"pluto/strings"
	"selfhost/internal/syntax/ast"
	"selfhost/internal/syntax/token"
	"selfhost/internal/types2"
)

// Frees and drops (language spec sections 18.11 and 19.3, memory-model.md
// sections 3.8 and 4.1). A free that is not ordered with a use of the same
// memory is a data race, which safe code cannot make, so the analysis
// counts freeing memory, and dropping a value (which frees what it owns),
// as writing that memory. A view made from an owner before a task starts
// (p := v.Ptr(0), xs := v.Slice()) is a place in the owner's memory, so a
// task that grows the owner, which frees the storage the view points to,
// writes the place the view reads.
//
//   - Assigning a value to a place that holds a value that owns memory (an
//     owner, a box, or something holding one) drops the old value: a write
//     of P#, everything reachable from the place P (path.go), and for a
//     local variable, whose value is followed, of the memory its boxes and
//     owned view fields point to. So does clear on a slice of such values,
//     and unsafe.Drop of what its argument points to.
//   - A call of the Free method of an Allocator writes the memory its first
//     argument points to, seen through the conversions to unsafe.Pointer
//     that every such call makes.
//   - The intrinsics of pluto/rt that containers use to move, copy, drop
//     and initialize their elements read and write what their pointer
//     arguments point to.
//
// These writes carry the free flag, so that a race they take part in says
// that the memory is freed.

// dropsMemory reports whether dropping a value of type T may free memory:
// T is an owner (an owner type parameter included) or a box, or a struct or
// array that holds one.
func dropsMemory(T types2.Type) bool {
	return dropsMemoryIn(T, 0)
}

func dropsMemoryIn(T types2.Type, depth int) bool {
	if T == nil || depth > 8 {
		return false
	}
	if types2.IsOwner(T) || isBox(T) {
		return true
	}
	switch u := underlying(T).(type) {
	case *types2.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if dropsMemoryIn(u.Field(i).Type(), depth+1) {
				return true
			}
		}
	case *types2.Array:
		return dropsMemoryIn(u.Elem(), depth+1)
	case *types2.Wrapper:
		if !u.IsPointer() {
			return dropsMemoryIn(u.Elem(), depth+1) // ring, volatile, aligned
		}
	}
	return false
}

// dropWrite records that the values of type T at places ps are dropped:
// a write of everything they reach. Memory this function has allocated and
// not yet handed out holds zero values, which own nothing.
func (w *walker) dropWrite(ps []vpath, T types2.Type, pos token.Pos) {
	if !dropsMemory(T) {
		return
	}
	w.freeing++
	for _, p := range ps {
		if w.initializing(p, w.st) {
			continue
		}
		w.access(p.extend("#"), true, false, pos, T, false, "")
		if p.root.kind == rLocal && localPath(p.path) {
			// A local variable's value is followed: the memory it owns
			// has roots of its own (an allocation, a parameter's memory).
			w.dropOwned(w.placeValue([]vpath{p}), T, pos, 0)
		}
	}
	w.freeing--
}

// dropOwned records the writes of dropping a value of type T with origins
// vals: what its boxes point to, and what its owned view fields
// (ownedview.go) point to, and so on through the fields that hold such
// values. Other pointers in it, an allocator it was made with for one, are
// not freed with it.
func (w *walker) dropOwned(vals origins, T types2.Type, pos token.Pos, depth int) {
	if len(vals) == 0 || depth > 4 {
		return
	}
	if isBox(T) {
		if !isOwnedBox(T) && w.frame == nil {
			// Releasing a counted reference frees the object only with
			// the last one, which no other task then holds. Only the
			// parties of one frame share one reference, a variable.
			return
		}
		for _, o := range vals {
			if o.tag != "" {
				continue
			}
			switch o.vp.root.kind {
			case rFunc, rLit, rRegion:
				continue
			}
			q := o.vp
			if q.isAddr() {
				q.path = strings.Slice(q.path, 0, len(q.path)-1)
			} else {
				q = q.extend(w.derefStep(T))
			}
			w.access(q.extend("#"), true, false, pos, pointerElem(T), false, "")
		}
		return
	}
	s, ok := underlying(T).(*types2.Struct)
	if !ok {
		return
	}
	for i := 0; i < s.NumFields(); i++ {
		f := s.Field(i)
		switch {
		case w.a.ownsField(T, f):
			for _, q := range vals.field(f.Name()).untag().deref(w.derefStep(f.Type())) {
				w.access(q.extend("#"), true, false, pos, f.Type(), false, "")
			}
		case dropsMemory(f.Type()):
			w.dropOwned(vals.field(f.Name()), f.Type(), pos, depth+1)
		}
	}
}

// newVar reports whether id declares the variable it names here, so that it
// holds no old value to drop.
func (w *walker) newVar(id *ast.Ident) bool {
	_, ok := w.a.defs[id].(*types2.Var)
	return ok
}

// isAllocatorFree reports whether fn is the Free method of an allocator:
// of the runtime's Allocator interface or of a type that implements it.
func (w *walker) isAllocatorFree(fn *types2.Func) bool {
	if fn == nil || fn.Name() != "Free" {
		return false
	}
	sig, _ := fn.Type().(*types2.Signature)
	if sig == nil || sig.Recv() == nil || sig.Params().Len() != 3 || !isUnsafePointer(sig.Params().At(0).Type()) {
		return false
	}
	R := sig.Recv().Type()
	if obj := w.a.allocatorIface(); obj != nil && types2.Identical(R, obj.Type()) {
		return true
	}
	if el := pointerElem(R); el != nil {
		R = el
	}
	return implementsAllocator(w.a, R)
}

// freedPlaces returns the memory that x, the pointer given to an
// allocator's Free, points to: x seen through conversions to
// unsafe.Pointer and unsafe.SliceData. It is evaluated quietly, since the
// call's arguments have been walked already.
func (w *walker) freedPlaces(x ast.Expr) []vpath {
	for {
		x = ast.Unparen(x)
		c, ok := x.(*ast.CallExpr)
		if !ok || len(c.Args) != 1 {
			break
		}
		if tv := w.a.types[ast.Unparen(c.Fun)]; tv.IsType() && isUnsafePointer(tv.Type) {
			x = c.Args[0]
			continue
		}
		if b, ok := w.a.uses[calleeIdent(c)].(*types2.Builtin); ok && b.Name() == "SliceData" {
			x = c.Args[0]
			continue
		}
		break
	}
	T := w.typeOf(x)
	if T == nil || isUnsafePointer(T) || isString(T) {
		return nil
	}
	w.quiet++
	v := w.value(x)
	w.quiet--
	return v.deref(w.derefStep(T))
}

// freeCall records the write of the memory a call of an allocator's Free
// frees.
func (w *walker) freeCall(e *ast.CallExpr, fn *types2.Func) {
	if len(e.Args) == 0 || !w.isAllocatorFree(fn) {
		return
	}
	ps := w.freedPlaces(e.Args[0])
	w.freeing++
	for _, p := range ps {
		w.access(p, true, false, e.Pos(), nil, false, "")
	}
	w.freeing--
}

// rtIntrinsic records what an intrinsic of pluto/rt that works on the
// values a pointer argument points to reads and writes. args are the
// call's arguments.
func (w *walker) rtIntrinsic(e *ast.CallExpr, fn *types2.Func, args []origins) {
	if fn == nil || fn.Pkg() == nil || fn.Pkg().Path() != "pluto/rt" {
		return
	}
	if sig, _ := fn.Type().(*types2.Signature); sig == nil || sig.Recv() != nil {
		return
	}
	use := func(i int, write bool) {
		if i >= len(args) || i >= len(e.Args) {
			return
		}
		T := pointerElem(w.typeOf(e.Args[i]))
		for _, p := range args[i].derefOr("*", w.unknownFor(e.Args[i])) {
			w.access(p, write, false, e.Pos(), T, false, "")
		}
	}
	switch fn.Name() {
	case "InitValue", "ZeroValues":
		use(0, true)
	case "CopyValue", "RetainValues":
		use(0, false)
	case "ReleaseValues":
		use(0, true) // drops the strings the values hold
	case "MoveValues", "CopyValues":
		use(0, true)
		use(1, false)
	}
}

// isOwnedBox reports whether T is an owned[T] box.
func isOwnedBox(T types2.Type) bool {
	wr, ok := underlying(T).(*types2.Wrapper)
	return ok && wr.Kind() == types2.WrapOwned
}
