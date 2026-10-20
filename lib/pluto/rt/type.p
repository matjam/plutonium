package rt

import "unsafe"

// Type descriptors, read-only. Plutonium has no reflection, but a type
// converted to an interface has a descriptor that says how its values are
// laid out (abi.md section 5): pluto/fmt walks composite values with it.
// Only the types a program converts to an interface, and the types they are
// made of, have descriptors.

// A Type is a type descriptor.
type Type = typeDesc

// A Field describes a field of a struct type.
type Field struct {
	Type     *Type
	Offset   uintptr
	Name     string // empty when the build strips field names (--fieldnames=false)
	Exported bool
	Embedded bool
}

// TypeOf returns the descriptor of v's dynamic type and v's data word: the
// value's bits for a direct type (Type.Direct), otherwise a pointer to the
// value. Both are nil for a nil interface.
func TypeOf(v any) (*Type, unsafe.Pointer) {
	words := (*[2]unsafe.Pointer)(unsafe.Pointer(&v))
	return staticType(words[0]), words[1]
}

// staticType returns the type descriptor or method table at p. Both are
// static data, never in a frame; going through uintptr says so to the
// escape analysis (language spec section 18.3), which otherwise could not
// tell the first word of an interface from the second.
func staticType(p unsafe.Pointer) *Type {
	return (*Type)(unsafe.Pointer(uintptr(p)))
}

// Make returns an empty interface value holding a value of type t with data
// word word (see TypeOf). The interface is a view of whatever word points
// at.
func Make(t *Type, word unsafe.Pointer) any {
	var v any
	words := (*[2]unsafe.Pointer)(unsafe.Pointer(&v))
	words[0] = unsafe.Pointer(t)
	words[1] = word
	return v
}

// Word returns the data word an interface holding the value of type t at p
// would have: the value's bits, zero-extended, if t is direct, otherwise p.
func (t *Type) Word(p unsafe.Pointer) unsafe.Pointer {
	if t.flags&flagDirect == 0 {
		return p
	}
	var w uintptr
	switch t.size {
	case 1:
		w = uintptr(*(*uint8)(p))
	case 2:
		w = uintptr(*(*uint16)(p))
	case 4:
		w = uintptr(*(*uint32)(p))
	case 8:
		// A word that is a pointer (a pointer, or a struct of one) is read
		// as one, not made from an integer: the escape analysis follows
		// what p holds into the data word (language spec section 18.3).
		return *(*unsafe.Pointer)(p)
	}
	return unsafe.Pointer(w)
}

// Kind returns t's kind, one of the Kind constants.
func (t *Type) Kind() uint8 { return t.kind }

// Size returns the size of a value of type t in bytes.
func (t *Type) Size() uintptr { return t.size }

// Name returns t as Go's reflect.Type.String writes it: main.T, []int,
// *hash.Map[string,int], interface {}.
func (t *Type) Name() string { return t.name }

// Direct reports whether a value of type t is held in an interface's data
// word itself.
func (t *Type) Direct() bool { return t.flags&flagDirect != 0 }

// HasMethods reports whether t has methods, so that an interface holding a
// t might satisfy a non-empty interface.
func (t *Type) HasMethods() bool { return t.flags&flagMethods != 0 }

// Elem returns the element type of an array, slice or ring, the type a
// pointer or owned or counted reference points at, or nil.
func (t *Type) Elem() *Type { return t.elem }

// Len returns the length of an array type, the capacity of a ring type, or
// the number of fields of a struct type.
func (t *Type) Len() int { return int(t.len) }

// Field returns field i of a struct type.
func (t *Type) Field(i int) Field {
	f := (*fieldDesc)(unsafe.Add(unsafe.Pointer(t.fields), uintptr(i)*unsafe.Sizeof(fieldDesc{})))
	return Field{
		Type:     f.typ,
		Offset:   f.offset,
		Name:     f.name,
		Exported: f.flags&fieldExported != 0,
		Embedded: f.flags&fieldEmbedded != 0,
	}
}

// PtrTo returns the descriptor of *T for a type T whose pointer has methods
// that T lacks, or nil.
func (t *Type) PtrTo() *Type { return t.ptrTo }

// IfaceAt returns the dynamic type and data word of the interface value of
// type t stored at p.
func (t *Type) IfaceAt(p unsafe.Pointer) (*Type, unsafe.Pointer) {
	words := (*[2]unsafe.Pointer)(p)
	tab := words[0]
	if tab == nil {
		return nil, nil
	}
	if t.flags&flagIfaceMethods != 0 {
		return (*itab)(unsafe.Pointer(uintptr(tab))).typ, words[1]
	}
	return staticType(tab), words[1]
}
