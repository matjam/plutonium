package types2

// Portable types (language spec section 8.9).
//
// A type is portable if its values may be sent between address spaces: it is
// built only from booleans, numbers, enums and packed types, and from arrays,
// structs, unions, ring, volatile and aligned types of portable types. The
// rule is structural. portable is a predeclared constraint satisfied by
// exactly these types; it is a bit of the constraint's type set, like
// comparable.

import (
	"strings"

	. "github.com/matjam/plutonium/internal/types2/errors"
)

// A portableStep is one step from a type to a component of it.
type portableStep struct {
	kind byte   // 'f' field, 'e' element, 'v' variant
	name string // of the field or variant
}

// A PortableCause says why a type is not portable: the first component, in
// declaration order and depth first, that cannot cross, and where it is.
type PortableCause struct {
	// Path leads from the type to the component. It is empty if the type
	// itself is the component.
	path []portableStep
	// Type is the component that is not portable.
	typ Type
	// reason says why that component cannot cross.
	reason string
	// fix and code say what to send instead: fix completes "Instead,"
	// and code is an example, or "".
	fix, code string
}

// Component returns the part of the type that is not portable, which is
// the type itself if the path is empty.
func (c *PortableCause) Component() Type { return c.typ }

// Reason returns why the component is not portable, as a clause that
// completes "which is not portable:".
func (c *PortableCause) Reason() string { return c.reason }

// Where returns the path to the component in source form, such as
// "inner.s" or "frames[].p", and what the last step names: "field",
// "element" or "variant". It returns "" for the type itself.
func (c *PortableCause) Where() (path, noun string) {
	if len(c.path) == 0 {
		return "", ""
	}
	var b strings.Builder
	for i, s := range c.path {
		switch s.kind {
		case 'e':
			b.WriteString("[]")
		default:
			if i > 0 {
				b.WriteByte('.')
			}
			b.WriteString(s.name)
		}
	}
	switch c.path[len(c.path)-1].kind {
	case 'e':
		noun = "element"
	case 'v':
		noun = "variant"
	default:
		noun = "field"
	}
	return b.String(), noun
}

// Portable reports whether values of type T may be sent between address
// spaces.
func Portable(T Type) bool { return PortableCauseOf(T) == nil }

// PortableCauseOf returns why T is not portable, or nil if it is.
func PortableCauseOf(T Type) *PortableCause {
	return portableType(T, nil, map[Type]bool{})
}

// portableType returns why T is not portable, or nil. path is the way to T
// from the type being judged.
func portableType(T Type, path []portableStep, seen map[Type]bool) *PortableCause {
	if T == nil || seen[T] {
		return nil
	}
	seen[T] = true
	fail := func(reason, fix, code string) *PortableCause {
		return &PortableCause{path: append([]portableStep(nil), path...), typ: T, reason: reason, fix: fix, code: code}
	}
	T = Unalias(T)
	if tp, ok := T.(*TypeParam); ok {
		if tp.obj.ownerMark {
			return fail(ownerReason, ownerFix, "")
		}
		if tp.iface().typeSet().IsPortable() {
			return nil
		}
		return fail("the type parameter's constraint does not require a portable type", "require portable of the type parameter", "["+tp.obj.name+" portable]")
	}
	// An owner is not portable, whatever it is built from.
	if n, ok := T.(*Named); ok && n.Origin().obj.ownerMark {
		return fail(ownerReason, ownerFix, "")
	}
	elem := func(e Type) *PortableCause {
		return portableType(e, append(path[:len(path):len(path)], portableStep{kind: 'e'}), seen)
	}
	switch u := T.Underlying().(type) {
	case *Basic:
		switch {
		case u.kind == UnsafePointer:
			return fail("an unsafe.Pointer holds an address in the sender's address space", "send an offset or an index as a number, which the receiver resolves in its own memory", "")
		case u.info&IsString != 0:
			return fail("a string's bytes live in the sender's address space", "send the bytes in a fixed-size array, with their length as a number", "$ [16]byte\n$Len uint8")
		}
	case *Pointer:
		return fail("a pointer holds an address in the sender's address space", "send the value it points to, or an index or offset as a number", "")
	case *Slice:
		return fail("a slice refers to memory in the sender's address space", "send the elements in a fixed-size array, with their count as a number", "$ [8]int32\n$Len uint8")
	case *Signature:
		return fail("a function value refers to code and variables in the sender's address space", "send a number or an enum that names the operation, which the receiver maps to its own function", "")
	case *Chan:
		return fail("a channel is a reference to a queue in the sender's address space", "send the values that would go through it, and let the receiver make its own channel", "")
	case *Map:
		return fail("a map is a reference to a table in the sender's address space", "send its entries in a fixed-size array of key and value structs, with their count as a number", "")
	case *Interface:
		return fail("an interface value refers to a type and to data in the sender's address space", "send a union with one variant for each type the interface can hold", "type Shape union {\n\tCircle struct{ r float64 }\n\tRect   struct{ w, h float64 }\n}")
	case *Array:
		return elem(u.elem)
	case *Struct:
		for _, f := range u.fields {
			sub := append(path[:len(path):len(path)], portableStep{kind: 'f', name: f.name})
			if c := portableType(f.typ, sub, seen); c != nil {
				return c
			}
		}
	case *TaggedUnion:
		for _, v := range u.variants {
			sub := append(path[:len(path):len(path)], portableStep{kind: 'v', name: v.name})
			if c := portableType(v.typ, sub, seen); c != nil {
				return c
			}
		}
	case *Wrapper:
		switch u.kind {
		case WrapVolatile, WrapAligned, WrapRing:
			return elem(u.elem)
		case WrapOwned:
			return fail("an owned box is an owner that points to memory in the sender's address space", "send the value in the box, and let the receiver box its own", "")
		default:
			return fail("a "+u.kind.String()+" box is a counted reference to an object in the sender's address space", "send the value in the box, and let the receiver make its own", "")
		}
	}
	return nil
}

const (
	ownerReason = "an owner must be dropped exactly once, and sending it would also transfer that duty, which the structural rule does not express"
	ownerFix    = "send the plain data the owner is made of, in a struct that is not declared owner, and let the receiver build its own owner"
)

// IsPortable reports whether each type in the type set is portable.
func (s *_TypeSet) IsPortable() bool {
	if s.terms.isAll() {
		return s.portable
	}
	return s.is(func(t *term) bool {
		return t != nil && portableType(t.typ, nil, map[Type]bool{}) == nil
	})
}

// IsPortable reports whether each type in interface t's type set is
// portable: whether t is, or embeds, the predeclared portable.
func (t *Interface) IsPortable() bool { return t.typeSet().IsPortable() }

// portableArgError is a type argument that is not portable, for a type
// parameter whose constraint requires it.
type portableArgError struct {
	tpar  *TypeParam
	targ  Type
	cause *PortableCause
	msg   string
	ex    *Explanation
}

func (e *portableArgError) Error() string { return e.msg }

// portableTypeArg returns the error for a type argument that is not
// portable, for a type parameter that requires one, or nil. bound is the
// parameter's constraint.
func (check *Checker) portableTypeArg(tpar *TypeParam, bound Type, targ Type) *portableArgError {
	bi, _ := bound.Underlying().(*Interface)
	if bi == nil || !bi.IsPortable() || !isValid(targ) {
		return nil
	}
	cause := PortableCauseOf(targ)
	if cause == nil {
		return nil
	}
	e := &portableArgError{tpar: tpar, targ: targ, cause: cause}
	e.msg = check.portableMessage(targ, cause)
	e.ex = portableExplanation(cause)
	return e
}

// portableMessage words why targ is not portable: the field that stops it,
// named by its path, and the reason.
func (check *Checker) portableMessage(targ Type, c *PortableCause) string {
	t := check.sprintf("%s", targ)
	path, noun := c.Where()
	if path == "" {
		return t + " is not portable: " + c.reason
	}
	return t + " has " + noun + " " + path + " of type " + check.sprintf("%s", c.typ) + ", which is not portable: " + c.reason
}

// portableExplanation is the explanation of a type argument that is not
// portable.
func portableExplanation(c *PortableCause) *Explanation {
	text := "A portable type can be sent between address spaces, such as two operating system processes that share no memory. Its values mean the same to the receiver as to the sender because they refer to nothing in the sender's memory: they are made only of numbers, booleans, enums, packed types, and arrays, structs and unions of those."
	intro := "Instead, " + c.fix
	if path, _ := c.Where(); strings.HasPrefix(path, "[") {
		intro = "Instead of an element, " + c.fix
	} else if path != "" {
		intro = "Instead of " + path + ", " + c.fix
	}
	// An example for a field is written with the field's name.
	name := "data"
	if n := len(c.path); n > 0 && c.path[n-1].kind == 'f' {
		name = c.path[n-1].name
	}
	return &Explanation{Text: text, Fixes: []ExplainFix{{Intro: intro, Code: strings.ReplaceAll(c.code, "$", name)}}}
}

// reportPortable reports a type argument that is not portable.
func (check *Checker) reportPortable(at positioner, e *portableArgError, what string) {
	check.explainf(at, NotPortable, e.ex, "cannot use %s as %s in %s: %s", check.sprintf("%s", e.targ), e.tpar.obj.name, what, e.msg)
}
