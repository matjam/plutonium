package lower

import (
	"go/constant"

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
	typerr "github.com/matjam/plutonium/internal/types2/errors"
)

// An errorSite is the error type and the counter that belong to one
// errors.New call site (language spec section 16.3).
type errorSite struct {
	typ     types2.Type // the site's type: a uint64 with an Error method
	counter *ssa.Sym    // the number of evaluations so far, in static storage
	text    string      // the constant message
}

// errorStringName is the name of the type of an errors.New result; %T prints
// it with a star, as Go does.
const errorStringName = "errorString"

// isErrorString reports whether obj is the type of an errors.New call site.
func isErrorString(obj *types2.TypeName) bool {
	return obj.Name() == errorStringName && obj.Pkg() != nil && obj.Pkg().Path() == "pluto/errors" &&
		obj.Parent() != nil && obj.Parent() != obj.Pkg().Scope()
}

// errorsNew lowers a call errors.New(c), whose message the checker requires
// to be a constant, to an error whose dynamic type belongs to the call site
// and whose data word is the site's count after adding one for this
// evaluation (language spec section 16.3). Nothing is allocated. Two results
// of one site differ in the count, a copy of a result is equal to it, and
// different sites differ in the type. pluto/rt.errorsNewCount adds one
// atomically and panics when the count is exhausted.
func (s *fnState) errorsNew(e *ast.CallExpr, fn *types2.Func) []value {
	T := s.typeOf(e)
	tv := s.info.Types[e.Args[0]]
	if tv.Value == nil || tv.Value.Kind() != constant.String {
		s.l.errorf(e.Pos(), diag.CheckerCode(typerr.ErrorsNewNotConstant), "errors.New needs a constant message; use fmt.Errorf")
		return []value{s.zeroValue(T)}
	}
	site := s.l.errorSite(e, fn.Pkg(), constant.StringVal(tv.Value))
	n := s.callRuntime("errorsNewCount", []*ssa.Type{ssa.TI64}, []*ssa.Value{s.symAddr(site.counter)})[0]
	return []value{s.assignConvert(scalarValue(n, site.typ), T, e)}
}

// errorSite returns the type and counter of the errors.New call e, made the
// first time the call is lowered. pkg is the package of errors.New.
func (l *Lowerer) errorSite(e *ast.CallExpr, pkg *types2.Package, msg string) *errorSite {
	if site := l.errorSites[e]; site != nil {
		return site
	}
	n := len(l.errorSites) + 1
	// The type is declared in a scope of its own, so that it counts as a
	// local type and its position tells it from the types of the other
	// sites.
	tn := types2.NewTypeName(token.Pos(n), pkg, errorStringName, nil)
	types2.NewScope(pkg.Scope(), token.NoPos, token.NoPos, "errors.New").Insert(tn)
	typ := types2.NewNamed(tn, types2.Typ[types2.Uint64], nil)
	recv := types2.NewVar(token.NoPos, pkg, "e", typ)
	res := types2.NewVar(token.NoPos, pkg, "", types2.Typ[types2.String])
	sig := types2.NewSignatureType(recv, nil, nil, nil, types2.NewTuple(res), false)
	method := types2.NewFunc(token.NoPos, pkg, "Error", sig)
	typ.AddMethod(method)
	site := &errorSite{typ: typ, counter: l.staticBytes(8, 8), text: msg}
	l.errorSites[e] = site
	l.errorMethods[method] = site
	return site
}

// errorMethod returns the symbol of the Error method of an errors.New call
// site's type, queueing its body the first time: it returns the constant
// message.
func (l *Lowerer) errorMethod(fn *types2.Func, site *errorSite) *ssa.Sym {
	name := funcSymName(fn, nil)
	if sym := l.funcs[name]; sym != nil {
		return sym
	}
	sym := l.out.Sym(name, ssa.SymFunc)
	l.funcs[name] = sym
	sig := fn.Type().(*types2.Signature)
	strT := types2.Typ[types2.String]
	l.queue = append(l.queue, &funcJob{sym: sym, sig: sig, build: func(s *fnState) {
		s.incomingArgs(sig)
		s.returnValues([]value{s.stringConst(site.text, strT)})
	}})
	return sym
}

// errorsNewValue reports a use of errors.New other than a direct call: the
// type it makes belongs to a call site, so there is no function to take the
// value of or defer.
func (l *Lowerer) errorsNewValue(fn *types2.Func, pos token.Pos, use string) bool {
	if !types2.IsErrorsNew(fn) {
		return false
	}
	l.errorf(pos, diag.ErrorsNewValue, "errors.New cannot be %s: each call site has its own error type, so call it directly with a constant message", use)
	return true
}
