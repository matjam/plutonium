// Translated by go2pu from internal/types2/ownership_kind.go at the go-cutover tag; edited by hand since.

package types2

// OwnershipKind is a type's static assignment and lifetime kind (language
// spec section 8.7). Its spelling is also the label used by documentation
// and editors (section 18.1).
type OwnershipKind uint8

const (
	KindUnknown OwnershipKind = iota
	KindValue
	KindCounted
	KindView
	KindOwner
)

func (k OwnershipKind) String() string {
	switch k {
	case KindValue:
		return "value"
	case KindCounted:
		return "counted"
	case KindView:
		return "view"
	case KindOwner:
		return "owner (moves)"
	}
	return ""
}

// KindOf classifies T without following borrowed or counted referents.
// Ordinary interfaces, including Error interfaces, are statically views;
// their dynamic counting rules do not change this label (section 16.7).
func KindOf(T Type) OwnershipKind {
	return ownershipKind(T, make(map[Type]bool))
}

func ownershipKind(T Type, visiting map[Type]bool) OwnershipKind {
	if T == nil || visiting[T] {
		return KindUnknown
	}
	visiting[T] = true
	defer delete(visiting, T)
	switch t := Unalias(T).(type) {
	case *Named:
		if IsOwner(t) {
			return KindOwner
		}
		return ownershipKind(t.Underlying(), visiting)
	case *Basic:
		switch t.Kind() {
		case Invalid, UntypedNil:
			return KindUnknown
		case String, UntypedString:
			return KindCounted
		case UnsafePointer:
			return KindView
		}
		return KindValue
	case *Pointer, *Slice, *Signature, *Interface:
		return KindView
	case *Map, *Chan:
		return KindCounted
	case *Wrapper:
		switch t.Kind() {
		case WrapOwned:
			return KindOwner
		case WrapShared, WrapSynced, WrapWeak:
			return KindCounted
		}
		return ownershipKind(t.Elem(), visiting)
	case *TypeParam:
		if IsOwnerTypeParam(t) {
			return KindOwner
		}
		return KindValue
	case *Packed:
		return KindValue
	case *Array:
		return ownershipKind(t.Elem(), visiting)
	case *Struct:
		kind := KindValue
		for i := range t.NumFields() {
			kind = max(kind, ownershipKind(t.Field(i).Type(), visiting))
		}
		return kind
	case *TaggedUnion:
		kind := KindValue
		for i := range t.NumVariants() {
			kind = max(kind, ownershipKind(t.Variant(i).Payload(), visiting))
		}
		return kind
	}
	return KindUnknown
}
