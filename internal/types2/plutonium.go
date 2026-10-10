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
