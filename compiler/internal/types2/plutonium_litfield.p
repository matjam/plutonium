// Translated by go2pu from internal/types2/plutonium_litfield.go at the go-cutover tag; edited by hand since.

package types2

import (
	"compiler/compat"
)

// LiteralFields returns the fields that the key name of a struct literal
// of type T selects, from the outermost to the field itself: the embedded
// fields traversed to reach a promoted field, none of them a pointer
// (language spec section 11.3), followed by the field. A key of a field of
// T itself gives one field. pkg is the package of the key's field, for an
// unexported name. It returns nil if name selects no field.
func LiteralFields(T Type, pkg *Package, name string) []*Var {
	obj, index, indirect := lookupFieldOrMethod(T, false, pkg, name, false)
	if _, ok := obj.(*Var); !ok || indirect || len(index) == 0 {
		return nil
	}
	out := compat.MakeCap[*Var](0, len(index))
	cur := T
	for _, i := range index {
		s, ok := cur.Underlying().(*Struct)
		if !ok || i >= s.NumFields() {
			return nil
		}
		f := s.Field(i)
		out = compat.Append(out, f)
		cur = f.Type()
	}
	return out
}
