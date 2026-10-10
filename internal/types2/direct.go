package types2

// PointerShaped reports whether a value of type t is a single pointer at
// run time, so that an interface holding one stores it in its data word
// (language spec section 4.4). As in Go, that is:
//
//   - a pointer, unsafe.Pointer, a function value, or an owned, shared or
//     synced reference;
//   - a struct with exactly one field, whose type is pointer-shaped;
//   - an array of length 1 whose element type is pointer-shaped.
//
// Lowering stores such values directly instead of boxing them in the frame,
// and the escape checks treat converting one to an interface like converting
// the pointer it holds. Word-sized scalars (integers, floats, booleans) are
// also held directly, but by their bits; PointerShaped does not cover them,
// since only a single pointer makes a wrapper struct one word with the
// pointer's equality.
func PointerShaped(t Type) bool {
	switch u := Unalias(t).Underlying().(type) {
	case *Pointer, *Signature:
		return true
	case *Basic:
		return u.kind == UnsafePointer
	case *Wrapper:
		return u.IsPointer()
	case *Struct:
		return u.NumFields() == 1 && PointerShaped(u.Field(0).Type())
	case *Array:
		return u.Len() == 1 && PointerShaped(u.Elem())
	}
	return false
}
