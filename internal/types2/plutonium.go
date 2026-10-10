package types2

// Plutonium language rules layered on the Go type checker.
//
// Plutonium is a kernel-safe language with Go syntax. Constructs that need
// a garbage collector, a scheduler or runtime type metadata are rejected
// here, at the point the checker first meets them, so that the rest of the
// checker can treat them as ordinary Go.

import (
	"slices"
	"strconv"

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

// SubstLocal is Subst for types that appear in the body of a generic
// function, where named types declared in the body may mention the
// function's type parameters. local is called for each named type that has
// no type parameters of its own; it returns the type to use instead (such
// a local type instantiated for targs), or nil to keep it. Aliases without
// type parameters are replaced by the substituted type they stand for when
// that differs.
func SubstLocal(ctxt *Context, t Type, tparams []*TypeParam, targs []Type, local func(*Named) Type) Type {
	if len(tparams) == 0 {
		return t
	}
	if ctxt == nil {
		ctxt = NewContext()
	}
	s := subster{pos: nopos, smap: makeSubstMap(tparams, targs), ctxt: ctxt, local: local}
	return s.typ(t)
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

// complexReason is why complex numbers are not part of Plutonium.
const complexReason = "they are rarely useful in systems code; use a struct of two floats"

// checkComplexValue notes a value of complex type, to be reported by
// reportComplex. Complex types may still appear in type expressions such
// as the type sets of constraints shared with Go code; what Plutonium
// rejects is any value of such a type, whether written directly, converted
// from an untyped complex constant or produced by instantiating a generic
// function.
func (check *Checker) checkComplexValue(x ast.Expr, mode operandMode, typ Type) {
	switch mode {
	case invalid, novalue, builtin, typexpr:
		return
	}
	if isComplexValueType(typ) {
		check.complexAt = append(check.complexAt, x.Pos())
	}
}

// checkComplexTypeArgs notes an instantiation with a complex type argument,
// which would make values of a complex type.
func (check *Checker) checkComplexTypeArgs(at positioner, targs []Type) {
	for _, t := range targs {
		if isComplexValueType(t) {
			check.complexAt = append(check.complexAt, at.Pos())
			return
		}
	}
}

// isComplexValueType reports whether typ is a typed complex type.
func isComplexValueType(typ Type) bool {
	b, ok := Unalias(typ).Underlying().(*Basic)
	return ok && b.info&IsComplex != 0 && b.info&IsUntyped == 0
}

// reportComplex reports the complex values noted while checking, once per
// source line (an expression and its operands are one mistake), at the
// first such position on the line. It runs after the untyped expressions
// have their final types.
func (check *Checker) reportComplex() {
	for x, info := range check.untyped {
		check.checkComplexValue(x, info.mode, info.typ)
	}
	if len(check.complexAt) == 0 {
		return
	}
	slices.Sort(check.complexAt)
	seen := map[string]bool{}
	for _, pos := range check.complexAt {
		key := strconv.Itoa(int(pos))
		if check.fset != nil {
			p := check.fset.Position(pos)
			key = p.Filename + ":" + strconv.Itoa(p.Line)
		}
		if !seen[key] {
			seen[key] = true
			check.forbidden(atPos(pos), "complex numbers are", complexReason)
		}
	}
	check.complexAt = nil
}

// IsErrorsNew reports whether fn is pluto/errors.New, which the compiler
// evaluates at each call site to a static error (language spec section
// 13.1).
func IsErrorsNew(fn *Func) bool {
	return fn != nil && fn.name == "New" && fn.pkg != nil && fn.pkg.path == "pluto/errors"
}

// errorsNewCall checks a call of errors.New: its message must be a
// constant, since the error it makes is static.
func (check *Checker) errorsNewCall(call *ast.CallExpr, args []*operand) {
	if !IsErrorsNew(check.calleeFunc(call)) || len(args) != 1 || !args[0].isValid() {
		return
	}
	if args[0].mode() != constant_ {
		check.errorf(args[0], InvalidCall, "errors.New needs a constant message, since each call site makes one static error; use fmt.Errorf to build a message at run time")
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
