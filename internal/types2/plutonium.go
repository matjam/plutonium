package types2

// Plutonium language rules layered on the Go type checker.
//
// Plutonium is a kernel-safe language with Go syntax. Constructs that need
// a garbage collector, a scheduler or runtime type metadata are rejected
// here, at the point the checker first meets them, so that the rest of the
// checker can treat them as ordinary Go.

import (
	"github.com/matjam/plutonium/internal/syntax/ast"
	. "github.com/matjam/plutonium/internal/types2/errors"
)

// forbidden reports a construct that Plutonium does not support. reason
// says why, in a few words, and is appended to the message.
func (check *Checker) forbidden(at positioner, what, reason string) {
	check.errorf(at, UnsupportedFeature, "%s not permitted in Plutonium (%s)", what, reason)
}

// unwrapTransparent gives a variable or value of type volatile[T] or
// aligned[T, N] the type T. The storage keeps its wrapper type; lowering finds
// it from the variable, field or pointer the expression reads.
func (check *Checker) unwrapTransparent(x *operand) {
	if x.mode() != variable && x.mode() != value {
		return
	}
	for {
		w, ok := x.typ().Underlying().(*Wrapper)
		if !ok || !w.isTransparent() {
			return
		}
		x.typ_ = w.elem
	}
}

// Subst returns t with each type parameter in tparams replaced by the
// matching type in targs. The compiler uses it to instantiate the types in a
// generic function's body. ctxt de-duplicates instantiated named types and may
// be shared across calls.
func Subst(ctxt *Context, t Type, tparams []*TypeParam, targs []Type) Type {
	if len(tparams) == 0 {
		return t
	}
	if ctxt == nil {
		ctxt = NewContext()
	}
	return (*Checker)(nil).subst(nopos, t, makeSubstMap(tparams, targs), nil, ctxt)
}

// allocatorType returns pluto/rt.Allocator, the type of the optional final
// argument of new and make (language spec section 4.2), or nil if package
// pluto/rt cannot be found.
func (check *Checker) allocatorType() Type {
	rt := check.pkg
	if rt.path != "pluto/rt" {
		if check.conf.Importer == nil {
			return nil
		}
		var err error
		if rt, err = check.conf.Importer.Import("pluto/rt"); err != nil || rt == nil {
			return nil
		}
	}
	if tn, ok := rt.scope.Lookup("Allocator").(*TypeName); ok {
		return tn.typ
	}
	return nil
}

// allocatorArg checks x, the allocator argument of the built-in what (new
// or make): it must be assignable to pluto/rt.Allocator. It returns that
// type, or nil after reporting an error.
func (check *Checker) allocatorArg(x *operand, what string) Type {
	if !x.isValid() {
		return nil
	}
	T := check.allocatorType()
	if T == nil {
		check.errorf(x, InvalidCall, invalidOp+"%s with an allocator argument needs package pluto/rt", what)
		return nil
	}
	check.assignment(x, T, "allocator argument to "+what)
	if !x.isValid() {
		return nil
	}
	return T
}

// isCapacity reports whether x, a final argument of make, is a capacity
// rather than an allocator: an integer or an untyped numeric constant.
func isCapacity(x *operand) bool {
	return !x.isValid() || allInteger(x.typ()) || isUntypedNumeric(x.typ())
}

// isMainStatus reports whether a result list is a single unnamed int64,
// the exit status main may return.
func isMainStatus(results *ast.FieldList) bool {
	if results.NumFields() != 1 || len(results.List[0].Names) > 0 {
		return false
	}
	id, ok := results.List[0].Type.(*ast.Ident)
	return ok && id.Name == "int64"
}

// forbiddenImports lists import paths Plutonium rejects, with the reason.
var forbiddenImports = map[string]string{
	"reflect": "requires runtime type metadata",
}

// checkImportPath reports a forbidden import and returns false.
func (check *Checker) checkImportPath(spec *ast.ImportSpec, path string) bool {
	if reason, bad := forbiddenImports[path]; bad {
		check.forbidden(spec.Path, "import of "+path+" is", reason)
		return false
	}
	return true
}
