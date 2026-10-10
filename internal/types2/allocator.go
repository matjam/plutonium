package types2

// Plutonium: the allocator argument of new and make (language spec section
// 4.2).

import (
	"go/constant"
	"sync"

	"github.com/matjam/plutonium/internal/syntax/token"
	. "github.com/matjam/plutonium/internal/types2/errors"
)

var (
	allocatorOnce  sync.Once
	allocatorIface *Interface
)

// AllocatorInterface returns the method set an allocator argument of new or
// make must have: that of pluto/rt.Allocator, which pluto/alloc re-exports
// as alloc.Allocator.
//
//	Alloc(size, align uintptr) unsafe.Pointer
//	Free(p unsafe.Pointer, size, align uintptr)
func AllocatorInterface() *Interface {
	allocatorOnce.Do(func() {
		uptr := Typ[UnsafePointer]
		word := Typ[Uintptr]
		param := func(name string, t Type) *Var { return NewParam(token.NoPos, nil, name, t) }
		alloc := NewFunc(token.NoPos, nil, "Alloc", NewSignatureType(nil, nil, nil,
			NewTuple(param("size", word), param("align", word)), NewTuple(param("", uptr)), false))
		free := NewFunc(token.NoPos, nil, "Free", NewSignatureType(nil, nil, nil,
			NewTuple(param("p", uptr), param("size", word), param("align", word)), nil, false))
		allocatorIface = NewInterfaceType([]*Func{alloc, free}, nil).Complete()
	})
	return allocatorIface
}

// isSizeOperand reports whether an argument of make after the type is a
// length or capacity (an integer, or an untyped numeric constant) rather
// than an allocator. An invalid operand counts as a size, so that it is
// reported once, as an invalid size.
func isSizeOperand(x *operand) bool {
	if !x.isValid() {
		return true
	}
	if x.mode() == constant_ && isNumeric(x.typ()) {
		return true
	}
	return allInteger(x.typ())
}

// sizeArg checks a length or capacity argument of make, as index does for an
// expression, and returns its type and its value if it is a constant (or -1).
func (check *Checker) sizeArg(x *operand) (Type, int64) {
	if !check.isValidIndex(x, InvalidIndex, "index", false) {
		return Typ[Invalid], -1
	}
	if x.mode() != constant_ || x.val.Kind() == constant.Unknown {
		return x.typ(), -1
	}
	v, ok := constant.Int64Val(x.val)
	assert(ok)
	return x.typ(), v
}

// allocatorArg checks the allocator argument of new or make. Any value
// whose type has the methods of alloc.Allocator will do; nil is accepted and
// panics with "no allocator" when the allocation runs.
func (check *Checker) allocatorArg(x *operand, builtin string) bool {
	if !x.isValid() {
		return false
	}
	iface := AllocatorInterface()
	if ok, _ := x.assignableTo(check, iface, nil); !ok {
		check.errorf(x, InvalidCall, invalidArg+"%s is not an allocator: the final argument of %s must have the methods Alloc(size, align uintptr) unsafe.Pointer and Free(p unsafe.Pointer, size, align uintptr), as alloc.Allocator does", x, builtin)
		return false
	}
	check.assignment(x, iface, "allocator argument to "+builtin)
	return x.isValid()
}
