// Package types implements the Plutonium type system and type checker.
// Plutonium is a kernel-safe subset of Go; this package enforces both
// type correctness and kernel-safety rules.
package types

import "fmt"

// Type is the interface for all Plutonium types.
type Type interface {
	typeNode()
	String() string
	Underlying() Type
}

// BasicKind identifies built-in types.
type BasicKind int

const (
	Invalid BasicKind = iota
	Bool
	Int8
	Int16
	Int32
	Int64
	Uint8
	Uint16
	Uint32
	Uint64
	Int
	Uint
	Uintptr
	Float32
	Float64
	Byte   // alias Uint8
	Rune   // alias Int32
	String // immutable bytes (no heap allocation in Plutonium)
	UnsafePointer
)

// Basic is a basic (predeclared) type.
type Basic struct {
	kind BasicKind
	name string
}

func (t *Basic) typeNode()        {}
func (t *Basic) String() string   { return t.name }
func (t *Basic) Underlying() Type { return t }
func (t *Basic) Kind() BasicKind  { return t.kind }

// Pointer is *T.
type Pointer struct {
	base Type
}

func (t *Pointer) typeNode()        {}
func (t *Pointer) String() string   { return "*" + t.base.String() }
func (t *Pointer) Underlying() Type { return t }
func (t *Pointer) Base() Type       { return t.base }

// Slice is []T.
type Slice struct {
	elem Type
}

func (t *Slice) typeNode()        {}
func (t *Slice) String() string   { return "[]" + t.elem.String() }
func (t *Slice) Underlying() Type { return t }
func (t *Slice) Elem() Type       { return t.elem }

// Array is [N]T.
type Array struct {
	len  int64
	elem Type
}

func (t *Array) typeNode()        {}
func (t *Array) String() string   { return fmt.Sprintf("[%d]%s", t.len, t.elem.String()) }
func (t *Array) Underlying() Type { return t }
func (t *Array) Len() int64       { return t.len }
func (t *Array) Elem() Type       { return t.elem }

// Struct is struct { Fields }.
type Struct struct {
	fields []*StructField
}

// StructField represents a single field in a struct type.
type StructField struct {
	name     string
	typ      Type
	tag      string
	embedded bool
}

func (f *StructField) Name() string   { return f.name }
func (f *StructField) Type() Type     { return f.typ }
func (f *StructField) Tag() string    { return f.tag }
func (f *StructField) Embedded() bool { return f.embedded }

func (t *Struct) typeNode()                {}
func (t *Struct) Underlying() Type         { return t }
func (t *Struct) NumFields() int           { return len(t.fields) }
func (t *Struct) Field(i int) *StructField { return t.fields[i] }
func (t *Struct) String() string           { return "struct{...}" }

// Signature is a function type.
type Signature struct {
	recv     *Var // nil for non-methods
	params   []*Var
	results  []*Var
	variadic bool
}

func (t *Signature) typeNode()        {}
func (t *Signature) Underlying() Type { return t }
func (t *Signature) Recv() *Var       { return t.recv }
func (t *Signature) Params() []*Var   { return t.params }
func (t *Signature) Results() []*Var  { return t.results }
func (t *Signature) Variadic() bool   { return t.variadic }
func (t *Signature) String() string   { return "func(...)" }

// Interface is an interface type (compile-time dispatch only).
type Interface struct {
	methods []*Func
}

func (t *Interface) typeNode()        {}
func (t *Interface) Underlying() Type { return t }
func (t *Interface) NumMethods() int  { return len(t.methods) }
func (t *Interface) String() string   { return "interface{...}" }
func (t *Interface) IsEmpty() bool    { return len(t.methods) == 0 }

// Named is a named type (type Foo struct{...} or type MyInt int32).
type Named struct {
	obj        *TypeName
	underlying Type
	methods    []*Func
}

func (t *Named) typeNode()          {}
func (t *Named) Underlying() Type   { return t.underlying }
func (t *Named) String() string     { return t.obj.name }
func (t *Named) Obj() *TypeName     { return t.obj }
func (t *Named) NumMethods() int    { return len(t.methods) }
func (t *Named) Method(i int) *Func { return t.methods[i] }
func (t *Named) AddMethod(m *Func)  { t.methods = append(t.methods, m) }

// TypeParam is a generic type parameter (T in func Foo[T constraint]).
type TypeParam struct {
	name       string
	constraint Type // the constraint type
}

func (t *TypeParam) typeNode()        {}
func (t *TypeParam) Underlying() Type { return t.constraint }
func (t *TypeParam) String() string   { return t.name }
func (t *TypeParam) Constraint() Type { return t.constraint }

// AnyType represents the built-in `any` type — a tagged fat pointer
// { typeID uint64, data uint64 } for type-safe value erasure without
// reflection. This is NOT the empty interface; it's a concrete 16-byte type.
type AnyType struct{}

func (t *AnyType) typeNode()        {}
func (t *AnyType) Underlying() Type { return t }
func (t *AnyType) String() string   { return "any" }

// Endianness specifies the byte order for a packed type.
type Endianness int

const (
	EndianNative Endianness = iota // no annotation — native byte order
	EndianBig                      // be — big-endian
	EndianLittle                   // le — little-endian
)

// Packed represents a packed bitfield type.
type Packed struct {
	backing    Type        // the backing storage type (e.g., Basic for uint32)
	fields     []*BitField // field definitions
	totalBits  int         // sum of all field widths
	endianness Endianness  // byte order annotation
}

func (t *Packed) typeNode()              {}
func (t *Packed) Underlying() Type       { return t }
func (t *Packed) String() string         { return "packed{...}" }
func (t *Packed) Backing() Type          { return t.backing }
func (t *Packed) NumFields() int         { return len(t.fields) }
func (t *Packed) Field(i int) *BitField  { return t.fields[i] }
func (t *Packed) TotalBits() int         { return t.totalBits }
func (t *Packed) Endianness() Endianness { return t.endianness }

// BitField represents a single field in a packed type.
type BitField struct {
	name       string
	accessType Type   // the T in bits[T, N]
	width      int    // the N in bits[T, N]
	offset     int    // computed: bit offset from LSB
	groupName  string // non-empty if this field belongs to a bits { ... } group
}

func (f *BitField) Name() string      { return f.name }
func (f *BitField) AccessType() Type  { return f.accessType }
func (f *BitField) Width() int        { return f.width }
func (f *BitField) Offset() int       { return f.offset }
func (f *BitField) GroupName() string { return f.groupName }

// Volatile is a type wrapper guaranteeing every read/write produces exactly
// one load/store instruction in program order, with no coalescing or elision.
// T must be a scalar type.
type Volatile struct {
	elem Type
}

func (t *Volatile) typeNode()        {}
func (t *Volatile) Underlying() Type { return t }
func (t *Volatile) Elem() Type       { return t.elem }
func (t *Volatile) String() string   { return "volatile[" + t.elem.String() + "]" }

// Aligned is a type wrapper guaranteeing N-byte alignment.
// N must be a power of 2.
type Aligned struct {
	elem      Type
	alignment int
}

func (t *Aligned) typeNode()        {}
func (t *Aligned) Underlying() Type { return t }
func (t *Aligned) Elem() Type       { return t.elem }
func (t *Aligned) Alignment() int   { return t.alignment }
func (t *Aligned) String() string {
	return fmt.Sprintf("aligned[%s, %d]", t.elem.String(), t.alignment)
}

// Ring represents a ring[T, N] lock-free ring buffer type.
// T is the element type and N is the buffer capacity (must be a power of 2).
// Layout: { head uint64, tail uint64, data [N]T }
type Ring struct {
	elem     Type
	capacity int
}

func (t *Ring) typeNode()        {}
func (t *Ring) Underlying() Type { return t }
func (t *Ring) Elem() Type       { return t.elem }
func (t *Ring) Capacity() int    { return t.capacity }
func (t *Ring) String() string {
	return fmt.Sprintf("ring[%s, %d]", t.elem.String(), t.capacity)
}

// Union represents a type union constraint (int32 | int64).
type Union struct {
	terms []Type
}

func (t *Union) typeNode()        {}
func (t *Union) Underlying() Type { return t }
func (t *Union) Terms() []Type    { return t.terms }
func (t *Union) String() string {
	s := ""
	for i, term := range t.terms {
		if i > 0 {
			s += " | "
		}
		s += term.String()
	}
	return s
}
