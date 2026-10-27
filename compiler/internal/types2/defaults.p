// Translated by go2pu from internal/types2/defaults.go at the go-cutover tag; edited by hand since.

package types2

// Plutonium default parameter values (language spec section 10.13).
//
// The trailing parameters of a function or method declaration may have
// default values. A call that names the function directly (f(x), pkg.F(x),
// F[T](x), v.M(x)) may omit any suffix of the defaulted parameters; the
// compiler evaluates the omitted defaults at the call site, after the
// explicit arguments, as if they were written there but resolved in the
// scope of the function's declaration. Defaults are not part of the
// function's type: a function value, method value, method expression or
// interface method is called with all of its arguments.

import (
	"pluto/strconv"
	"compiler/compat"
	"compiler/internal/syntax/ast"
	. "compiler/internal/types2/errors"
	"compiler/pu"
)

// Defaults returns the default values of the trailing parameters of a
// declared function or method: for a function with n parameters, the last
// len(Defaults()) of them have the defaults in order. It is nil for a
// function without defaults. The expressions belong to the declaring
// package; their types are recorded in that package's Info, in terms of the
// function's own type parameters.
func (obj *Func) Defaults() []ast.Expr { return obj.Origin().defaults }

// noteFunc records that id names the declared function fn.
func (check *Checker) noteFunc(id *ast.Ident, fn *Func) {
	if check.funcRefs == nil {
		check.funcRefs = make(map[*ast.Ident]*Func)
	}
	check.funcRefs[id] = fn
}

// calleeFunc returns the declared function or method that a call names
// directly, or nil for a call through a func value, a method expression or
// anything else.
func (check *Checker) calleeFunc(call *ast.CallExpr) *Func {
	fun := ast.Unparen(call.Fun)
	if ix := unpackIndexedExpr(fun); ix != nil {
		fun = ast.Unparen(ix.x)
	}
	switch f := fun.(type) {
	case *ast.Ident:
		return check.funcRefs[f]
	case *ast.SelectorExpr:
		return check.funcRefs[f.Sel]
	}
	return nil
}

// calleeDefaults returns the default values the callee of call offers.
func (check *Checker) calleeDefaults(call *ast.CallExpr) []ast.Expr {
	if fn := check.calleeFunc(call); fn != nil {
		return fn.Defaults()
	}
	return nil
}

// paramDefaults checks where a function type declares default values and
// returns the defaults of its trailing parameters, or nil. Receivers and
// results cannot have defaults, a parameter without a default cannot
// follow one with a default, and a variadic function cannot have any: an
// argument after the variadic ones would be indistinguishable from one of
// them.
func (check *Checker) paramDefaults(recvPar *ast.FieldList, ftyp *ast.FuncType, variadic bool) []ast.Expr {
	if recvPar != nil {
		for _, f := range recvPar.List {
			if f.Default != nil {
				check.error(f.Default, MisplacedDefault, "a receiver cannot have a default value")
			}
		}
	}
	if ftyp.Results != nil {
		for _, f := range ftyp.Results.List {
			if f.Default != nil {
				check.error(f.Default, MisplacedDefault, "result parameters cannot have default values")
			}
		}
	}
	if ftyp.Params == nil {
		return nil
	}
	if variadic {
		for _, f := range ftyp.Params.List {
			if f.Default != nil {
				check.error(f.Default, VariadicDefault, "a variadic function cannot have default parameter values (an argument after the variadic ones could not be told apart from them)")
				return nil
			}
		}
		return nil
	}
	var defs []ast.Expr
	var first *ast.Field // the first parameter with a default
	index := 0           // parameter number of the current field's first name
	for _, f := range ftyp.Params.List {
		n := max(1, len(f.Names))
		switch {
		case f.Default == nil && first != nil:
			check.errorf(f, MissingDefault, "parameter %s has no default value but follows parameter %s, which has one",
				pu.Box(paramLabel(f, index)), pu.Box(paramLabel(first, index-len(defs))))
			return nil
		case f.Default == nil:
		case n > 1:
			// The parser rejects "a, b T = x"; a constructed tree may not.
			check.error(f.Default, SharedDefault, "a default value must follow a single parameter name and its type")
			return nil
		default:
			if first == nil {
				first = f
			}
			defs = compat.Append(defs, f.Default)
		}
		index += n
	}
	return defs
}

// paramLabel names a parameter in an error message: by name, or by
// position when it has none.
func paramLabel(f *ast.Field, index int) string {
	if len(f.Names) > 0 {
		return f.Names[0].Name
	}
	return compat.Concat("#", strconv.Itoa(index+1))
}

// defaultsNotAllowed reports a default value outside a function or method
// declaration.
func (check *Checker) defaultsNotAllowed(def ast.Expr) {
	check.error(def, MisplacedDefault, "default parameter values are allowed only in function and method declarations, not in function types, function literals or interface methods")
}

// defaultValues type-checks the default values of a declared function's
// trailing parameters. A default is checked in the scope of the declaration
// with the function's type parameters (and its receiver type's) in scope,
// but not its parameters: a reference to one is an error, since a default
// is evaluated by the caller. References to package-level objects count as
// dependencies of the function, as references in its body do.
func (check *Checker) defaultValues(decl *declInfo, sig *Signature, defs []ast.Expr) {
	defer func(env environment) { check.environment = env }(check.environment)

	// The scope sees what the declaration sees, plus the type parameters.
	// Each parameter name is bound to a placeholder that reports a use.
	scope := NewScope(nil, defs[0].Pos(), defs[len(defs)-1].End(), "default values")
	scope.parent = sig.scope.parent
	for _, list := range []*TypeParamList{sig.RecvTypeParams(), sig.TypeParams()} {
		for i := range list.Len() {
			scope.Insert(list.At(i).Obj())
		}
	}
	vars := sig.params.vars
	if sig.recv != nil {
		vars = compat.Append([]*Var{sig.recv}, vars...)
	}
	if check.paramPlaceholders == nil {
		check.paramPlaceholders = make(map[*Var]bool)
	}
	for _, v := range vars {
		if v.name != "" && v.name != "_" {
			ph := newVar(ParamVar, v.pos, v.pkg, v.name, v.typ)
			check.paramPlaceholders[ph] = true
			scope.Insert(ph)
		}
	}

	params := sig.params.vars[len(sig.params.vars)-len(defs):]
	for i, def := range defs {
		check.environment = environment{decl: decl, scope: scope, version: check.version}
		par := params[i]
		x := pu.Alloc[operand]()
		check.expr(newTarget(par.typ, "default value"), x, def)
		context := "default value"
		if par.name != "" {
			context = compat.Concat(context, compat.Concat(" of ", par.name))
		}
		check.assignment(x, par.typ, context)
	}
}

// paramPlaceholderUse reports a default value's reference to a parameter of
// its own function, which the caller does not have when it evaluates the
// default.
func (check *Checker) paramPlaceholderUse(e *ast.Ident, v *Var) bool {
	if !check.paramPlaceholders[v] {
		return false
	}
	check.errorf(e, DefaultRefersToParam, "a default value cannot refer to parameter %s (defaults are evaluated by the caller)", pu.Box(v.name))
	return true
}
