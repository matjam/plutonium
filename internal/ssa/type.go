// Package ssa is Plutonium's intermediate representation: functions of basic
// blocks holding values in static single assignment form.
//
// The design follows Go's compiler SSA. Memory is an SSA value: a load takes
// the memory state it reads, and a store or call takes one memory state and
// produces the next. Ordering between memory operations is therefore explicit
// in the data flow, so optimisations that move or remove loads and stores need
// no separate alias analysis to stay correct.
//
// Types here are machine-level. Integers carry only a size; signedness lives in
// the operations (Div versus Divu, Less versus Lessu). Aggregates (structs,
// arrays, strings, slices, interfaces) never appear as SSA values: lowering keeps
// them in stack slots, and scalar replacement (sroa) splits slots whose address
// does not escape into scalar slots that mem2reg turns into SSA values.
package ssa

import "fmt"

// A Kind classifies an SSA type.
type Kind uint8

const (
	KVoid  Kind = iota // no value
	KMem               // memory state
	KBool              // boolean, one byte in memory
	KInt               // integer of Size bytes (1, 2, 4 or 8)
	KFloat             // IEEE float of Size bytes (4 or 8)
	KPtr               // pointer
	KTuple             // multiple results (calls); elements in Elems
	KFlags             // condition flags (backend only)
)

// A Type is an SSA value type.
type Type struct {
	Kind  Kind
	Size  int64
	Elems []*Type // for KTuple
}

// Predeclared types. Types other than tuples are compared by pointer.
var (
	TVoid  = &Type{Kind: KVoid}
	TMem   = &Type{Kind: KMem}
	TBool  = &Type{Kind: KBool, Size: 1}
	TI8    = &Type{Kind: KInt, Size: 1}
	TI16   = &Type{Kind: KInt, Size: 2}
	TI32   = &Type{Kind: KInt, Size: 4}
	TI64   = &Type{Kind: KInt, Size: 8}
	TF32   = &Type{Kind: KFloat, Size: 4}
	TF64   = &Type{Kind: KFloat, Size: 8}
	TPtr   = &Type{Kind: KPtr, Size: 8}
	TFlags = &Type{Kind: KFlags}
)

// IntType returns the integer type of the given size in bytes.
func IntType(size int64) *Type {
	switch size {
	case 1:
		return TI8
	case 2:
		return TI16
	case 4:
		return TI32
	case 8:
		return TI64
	}
	panic(fmt.Sprintf("ssa: no integer type of size %d", size))
}

// FloatType returns the float type of the given size in bytes.
func FloatType(size int64) *Type {
	switch size {
	case 4:
		return TF32
	case 8:
		return TF64
	}
	panic(fmt.Sprintf("ssa: no float type of size %d", size))
}

// TupleOf returns a tuple type.
func TupleOf(elems ...*Type) *Type {
	return &Type{Kind: KTuple, Elems: elems}
}

func (t *Type) IsMem() bool   { return t.Kind == KMem }
func (t *Type) IsInt() bool   { return t.Kind == KInt }
func (t *Type) IsFloat() bool { return t.Kind == KFloat }
func (t *Type) IsPtr() bool   { return t.Kind == KPtr }
func (t *Type) IsBool() bool  { return t.Kind == KBool }
func (t *Type) IsTuple() bool { return t.Kind == KTuple }
func (t *Type) IsVoid() bool  { return t.Kind == KVoid }
func (t *Type) IsFlags() bool { return t.Kind == KFlags }
func (t *Type) IsScalar() bool {
	return t.Kind == KBool || t.Kind == KInt || t.Kind == KFloat || t.Kind == KPtr
}
func (t *Type) IsInteger() bool { return t.Kind == KInt || t.Kind == KPtr || t.Kind == KBool }

// Bits returns the width of a scalar type in bits.
func (t *Type) Bits() int64 { return t.Size * 8 }

// Same reports whether two types are the same.
func (t *Type) Same(u *Type) bool {
	if t == u {
		return true
	}
	if t.Kind != u.Kind || t.Size != u.Size {
		return false
	}
	if t.Kind == KTuple {
		if len(t.Elems) != len(u.Elems) {
			return false
		}
		for i := range t.Elems {
			if !t.Elems[i].Same(u.Elems[i]) {
				return false
			}
		}
	}
	return true
}

func (t *Type) String() string {
	switch t.Kind {
	case KVoid:
		return "void"
	case KMem:
		return "mem"
	case KBool:
		return "bool"
	case KInt:
		return fmt.Sprintf("i%d", t.Size*8)
	case KFloat:
		return fmt.Sprintf("f%d", t.Size*8)
	case KPtr:
		return "ptr"
	case KFlags:
		return "flags"
	case KTuple:
		s := "("
		for i, e := range t.Elems {
			if i > 0 {
				s += ","
			}
			s += e.String()
		}
		return s + ")"
	}
	return "?"
}
