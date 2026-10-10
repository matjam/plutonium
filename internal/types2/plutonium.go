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
