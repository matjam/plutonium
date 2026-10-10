package main

import (
	"fmt"
	"math"
)

type kind int

const (
	kInt kind = iota
	kBool
	kArray
	kSlice
	kStruct
	kPtr
)

// A typ is a type the generator can produce. Types are canonical: two
// typs with the same spelling are the same pointer (see gen.canon).
type typ struct {
	kind   kind
	name   string // spelling for ints, bool and structs
	bits   int
	signed bool
	elem   *typ    // arrays, slices and pointers
	n      int     // array length
	fields []field // structs
}

type field struct {
	name string
	t    *typ
}

func (t *typ) String() string {
	switch t.kind {
	case kArray:
		return fmt.Sprintf("[%d]%s", t.n, t.elem)
	case kSlice:
		return "[]" + t.elem.String()
	case kPtr:
		return "*" + t.elem.String()
	}
	return t.name
}

func (t *typ) scalar() bool { return t.kind == kInt || t.kind == kBool }

// ref reports whether a value of t refers to memory it does not hold.
func (t *typ) ref() bool { return t.kind == kSlice || t.kind == kPtr }

// minS and maxS are the range of a signed type; maxU is the largest value
// of an unsigned type.
func (t *typ) minS() int64 { return -1 << (t.bits - 1) }
func (t *typ) maxS() int64 { return 1<<(t.bits-1) - 1 }
func (t *typ) maxU() uint64 {
	if t.bits == 64 {
		return math.MaxUint64
	}
	return 1<<t.bits - 1
}

// minLit is the spelling of the type's minimum value.
func (t *typ) minLit() string {
	if t.signed {
		return fmt.Sprint(t.minS())
	}
	return "0"
}

var (
	tBool    = &typ{kind: kBool, name: "bool"}
	tInt     = &typ{kind: kInt, name: "int", bits: 64, signed: true}
	tUint    = &typ{kind: kInt, name: "uint", bits: 64}
	tUint8   = &typ{kind: kInt, name: "uint8", bits: 8}
	intTypes = []*typ{
		{kind: kInt, name: "int8", bits: 8, signed: true},
		{kind: kInt, name: "int16", bits: 16, signed: true},
		{kind: kInt, name: "int32", bits: 32, signed: true},
		{kind: kInt, name: "int64", bits: 64, signed: true},
		tUint8,
		{kind: kInt, name: "uint16", bits: 16},
		{kind: kInt, name: "uint32", bits: 32},
		tUint64,
		tInt,
		tUint,
		{kind: kInt, name: "uintptr", bits: 64},
	}
	tUint64 = &typ{kind: kInt, name: "uint64", bits: 64}
)

// unsignedTypes returns the unsigned integer types.
func unsignedTypes() []*typ {
	var out []*typ
	for _, t := range intTypes {
		if !t.signed {
			out = append(out, t)
		}
	}
	return out
}
