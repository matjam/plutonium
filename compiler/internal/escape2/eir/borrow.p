// Translated by go2pu from internal/escape2/eir/borrow.go at the go-cutover tag; edited by hand since.

package eir

import (
	"compiler/compat"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
)

// Views of counted errors (sections 16.6 and 18.3; the borrowError of
// internal/escape). A counted error holds a reference of its own to a
// shared or synced box, which is released when its holder is: a local
// variable when its scope ends, a call's result at the end of its
// statement. A pointer taken out of it (an assertion to a pointer type, a
// conversion to an interface that does not count) is a view of the box,
// which only the holder keeps alive. OpBorrow X, Y gives such a value the
// holder's view, Y (the ErrHolder object of the variable, or of a statement
// temporary), when X holds a counted reference; memory a parameter lends,
// or a static error, keeps its own provenance, and the In tokens it holds
// are lent, so that the summary's keys of them are Borrowed. A counted
// error passed to a callee is OpLend X, Y: the callee's Borrowed keys of it
// see the holder, and its other keys (the error itself, stored or returned,
// which takes a reference of its own) do not.

// borrowError returns r, a value taken out of the counted error x, as a
// view of x's holder as well.
func (fb *fnb) borrowError(x ast.Expr, r Reg) Reg {
	if r == 0 || fb.cur == nil {
		return r
	}
	h := fb.errorHolder(x)
	if h == 0 {
		return r
	}
	return fb.emitDst(Instr{Op: OpBorrow, X: r, Y: h, Pos: fb.pos})
}

// recount returns r, copied as a counted error of type T (returned,
// stored, asserted back from an interface that does not count): a
// reference of its own, no view (OpRecount). A callee's counted copy is a
// Counts key, which its caller applies without the holder: fmt's %w takes
// the error back from an any its caller converted.
//
// For T a type parameter the instance decides: the OpRecount carries T, and
// is a reference of its own only when every type the program instantiates
// T with holds one, a counted error or a shared or synced box
// (solve.RecountsInst), as one body serves them all
// (errors.As storing a synced[E] target, section 16.6); otherwise it is the
// identity, and a view stays a view.
func (fb *fnb) recount(r Reg, T types2.Type) Reg {
	if r == 0 || fb.cur == nil || T == nil {
		return r
	}
	if isTypeParam(T) {
		return fb.emitDst(Instr{Op: OpRecount, X: r, Type: T, Pos: fb.pos})
	}
	if !types2.IsCountedError(T) || types2.IsOwner(T) {
		return r
	}
	return fb.emitDst(Instr{Op: OpRecount, X: r, Pos: fb.pos})
}

// assertCounted returns r, the value of an interface of type from asserted
// to the type parameter T, as recount gives it: a counted copy when every
// instance of T holds a reference of its own.
func (fb *fnb) assertCounted(from, T types2.Type, r Reg) Reg {
	if !isTypeParam(T) || !isInterface(from) {
		return r
	}
	return fb.recount(r, T)
}

// lendView returns r, a value of a counted error converted to an
// interface that does not count, as a variadic argument: its In tokens
// lent (OpBorrow with no holder). The holder's view is not added: fmt's %w
// takes the error back through a generic container (vec.Fixed[error]),
// whose store of a type parameter makes no counted copy the summary can
// show, and fmt's walk keeps every operand (D51). Other conversions add it
// (viewError).
func (fb *fnb) lendView(r Reg) Reg {
	if r == 0 || fb.cur == nil {
		return r
	}
	return fb.emitDst(Instr{Op: OpBorrow, X: r, Pos: fb.pos})
}

// viewError returns r, a value of the counted error x converted to an
// interface that does not count: a view of the box x holds, which x's
// holder bounds (borrowError), or with no holder its In tokens lent
// (lendView). return any(e) of a local e points into what e keeps alive.
func (fb *fnb) viewError(x ast.Expr, r Reg) Reg {
	if r == 0 || fb.cur == nil {
		return r
	}
	if h := fb.errorHolder(x); h != 0 {
		return fb.emitDst(Instr{Op: OpBorrow, X: r, Y: h, Pos: fb.pos})
	}
	return fb.lendView(r)
}

// countHeap returns r with its heap pointers counted references
// (OpCountHeap); direct for a shared or synced box converted to a counted
// error, whose In tokens become counted copies too.
func (fb *fnb) countHeap(r Reg, direct bool) Reg {
	if r == 0 || fb.cur == nil {
		return r
	}
	in := Instr{Op: OpCountHeap, X: r, Pos: fb.pos}
	if direct {
		in.Index = 1
	}
	return fb.emitDst(in)
}

// loadsCounted reports whether e, of type T, is a counted error loaded
// from memory the analysis does not follow into (a field, an element, a
// pointer's target, a channel's value, a package variable): what it holds
// in the heap may be a reference of its own, not a view of a holder's.
// (What a parameter's path holds keeps its key.)
func (fb *fnb) loadsCounted(e ast.Expr, T types2.Type) bool {
	if T == nil || !types2.IsCountedError(T) || types2.IsOwner(T) {
		return false
	}
	switch x := ast.Unparen(e).(type) {
	case *ast.SelectorExpr, *ast.IndexExpr, *ast.IndexListExpr, *ast.StarExpr:
		return true
	case *ast.UnaryExpr:
		return x.Op == token.ARROW
	case *ast.Ident:
		v, ok := fb.b.uses[x].(*types2.Var)
		return ok && fb.isGlobal(v)
	}
	return false
}

// lendError returns r, the counted error x passed to a callee, lent with
// x's holder (OpLend).
func (fb *fnb) lendError(x ast.Expr, r Reg) Reg {
	if r == 0 || fb.cur == nil {
		return r
	}
	h := fb.errorHolder(x)
	if h == 0 {
		return r
	}
	return fb.emitDst(Instr{Op: OpLend, X: r, Y: h, Pos: fb.pos})
}

// errorHolder returns the address of the view object of what holds the
// counted error x: a local variable (x, x.f, x[i] on an array), or the
// statement, for a call's result; 0 for one reached through a pointer or in
// a package variable, which generation checks guard at run time (section
// 18.11).
func (fb *fnb) errorHolder(x ast.Expr) Reg {
	T := fb.typeOf(x)
	if v := fb.boxRoot(x); v != nil {
		if owner := fb.ownerOf(v); owner != nil {
			_, whole := ast.Unparen(x).(*ast.Ident)
			o := owner.boxViewObj(v, T, whole)
			owner.f.Objects[o].ErrHolder = true
			return fb.objAddr(owner, o)
		}
	}
	if call, ok := ast.Unparen(x).(*ast.CallExpr); ok {
		// An error a call returns, which the statement holds.
		o := fb.newObj(Object{Kind: ObjBoxView, Type: T, Life: LifeStmt, Name: "the error a call returns", Pos: call.Pos(), ErrHolder: true})
		return fb.addr(o)
	}
	return 0
}

// lendsError reports whether a value of type to taken out of a counted
// error of type from is a view of it: from is a counted error, and to
// neither counts (a counted error, or a shared or synced box, takes a
// reference of its own) nor owns.
func lendsError(from, to types2.Type) bool {
	return countsTo(from, to) == lendView
}

// A conversion's effect on a counted error's references.
const (
	lendNone = iota
	lendView // a view of the holder (OpBorrow)
	lendCopy // a counted copy (OpRecount)
)

// countsTo returns what taking a value of type to out of one of type from
// does: a view of a counted error, a counted copy of one (from an interface
// that does not count back to a counted error), or nothing.
func countsTo(from, to types2.Type) int {
	if from == nil || to == nil {
		return lendNone
	}
	if types2.IsCountedError(to) && !types2.IsOwner(to) && isInterface(from) && !types2.IsCountedError(from) {
		return lendCopy
	}
	if lendsErrorOld(from, to) {
		return lendView
	}
	return lendNone
}

func lendsErrorOld(from, to types2.Type) bool {
	return from != nil && to != nil && types2.IsCountedError(from) && !types2.IsCountedError(to) && !isCountedBox(to) && !types2.IsOwner(to)
}

// freshCounted returns the results of a call, res of types, with a counted
// reference in each that is a shared or synced box: a reference of its
// own the caller holds, whatever made it (alloc.Synced makes it with
// unsafe code, as heap memory).
func (fb *fnb) freshCounted(res []Reg, types []types2.Type) []Reg {
	out := res
	for i, r := range res {
		if r == 0 || i >= len(types) || !isCountedBox(types[i]) || fb.cur == nil {
			continue
		}
		if &out[0] == &res[0] {
			out = compat.AppendPlain([]Reg(nil), res...)
		}
		out[i] = fb.union(r, []Reg{fb.heap(HeapCounted)}, types[i])
	}
	return out
}

// Boxes lent through a type parameter (section 18.3, "A local owner's
// memory"; section 18.7). A generic body is analysed once for all its
// instances, so a box passed by value to a parameter of generic type is a
// value of a type parameter there. A view the body takes through it (a
// pointer method of the box's object called on the value, or the value
// converted to an interface, whose data word lends the box) is a view of
// the box, which what holds the box in the caller bounds, as it bounds a
// view the caller takes itself. The body lends such a value (lendView), so
// the summary's keys of what it reaches are Borrowed; the caller passes the
// box with OpLend X, Y, Index 1, Y the view object of its holder: the local
// variable, or the statement for a temporary. The callee's other keys (the
// box itself, copied, stored or returned, which keeps a reference of its
// own) do not see the holder.

// lendBox returns r, argument x of type T (or, x nil, a result of call)
// passed to a parameter whose declared type mentions a type parameter, lent
// with the view object of what holds the boxes T holds.
func (fb *fnb) lendBox(x, call ast.Expr, T types2.Type, r Reg) Reg {
	if r == 0 || fb.cur == nil || !holdsBox(T, 0) {
		return r
	}
	h := fb.boxHolder(x, call, T)
	if h == 0 {
		return r
	}
	return fb.emitDst(Instr{Op: OpLend, X: r, Y: h, Index: 1, Pos: fb.pos})
}

// boxHolder returns the address of the view object of what holds x, a
// value of type T that holds a box: a local variable (x, x.f, x[i] on an
// array), or the statement, for a temporary (or, x nil, for a result of
// call); 0 for a box reached through a pointer, in a package variable or in
// the caller's region, which generation checks guard (section 18.11).
func (fb *fnb) boxHolder(x, call ast.Expr, T types2.Type) Reg {
	objT := T
	if isBox(T) {
		objT = pointerElem(T)
	}
	if x != nil {
		if v := fb.boxRoot(x); v != nil {
			if owner := fb.ownerOf(v); owner != nil {
				if owner != fb && owner.regionVars[v] {
					return 0
				}
				o := owner.boxViewObj(v, objT, false)
				owner.f.Objects[o].BoxHolder = true
				return fb.objAddr(owner, o)
			}
		}
		call = fb.tempRoot(x)
	}
	if call == nil {
		return 0
	}
	o := fb.newObj(Object{Kind: ObjBoxView, Type: objT, Life: LifeStmt, Name: "the object of a temporary box", Pos: call.Pos(), BoxHolder: true})
	return fb.addr(o)
}

// holdsBox reports whether a value of T holds a box in place: T is one, or
// a struct or array holding one.
func holdsBox(T types2.Type, depth int) bool {
	if T == nil || depth > 16 || isTypeParam(T) {
		return false
	}
	if isBox(T) {
		return true
	}
	switch u := unwrap(T).Underlying().(type) {
	case *types2.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if holdsBox(u.Field(i).Type(), depth+1) {
				return true
			}
		}
	case *types2.Array:
		return u.Len() > 0 && holdsBox(u.Elem(), depth+1)
	}
	return false
}

// genericParam reports whether parameter i of fn is declared with a type
// that mentions a type parameter.
func genericParam(fn *types2.Func, i int) bool {
	if fn == nil {
		return false
	}
	sig, ok := fn.Origin().Type().(*types2.Signature)
	if !ok || i >= sig.Params().Len() {
		return false
	}
	return mentionsTypeParam(sig.Params().At(i).Type())
}

// genericRecv reports whether method fn's receiver is declared with a type
// that mentions a type parameter.
func genericRecv(fn *types2.Func) bool {
	if fn == nil {
		return false
	}
	sig, ok := fn.Origin().Type().(*types2.Signature)
	return ok && sig.Recv() != nil && mentionsTypeParam(sig.Recv().Type())
}

// mentionsTypeParam reports whether t mentions a type parameter.
func mentionsTypeParam(t types2.Type) bool {
	switch t := t.(type) {
	case nil:
		return false
	case *types2.TypeParam:
		return true
	case *types2.Alias:
		return mentionsTypeParam(types2.Unalias(t))
	case *types2.Named:
		if args := t.TypeArgs(); args != nil {
			for i := 0; i < args.Len(); i++ {
				if mentionsTypeParam(args.At(i)) {
					return true
				}
			}
		}
	case *types2.Pointer:
		return mentionsTypeParam(t.Elem())
	case *types2.Slice:
		return mentionsTypeParam(t.Elem())
	case *types2.Array:
		return mentionsTypeParam(t.Elem())
	case *types2.Map:
		return mentionsTypeParam(t.Key()) || mentionsTypeParam(t.Elem())
	case *types2.Chan:
		return mentionsTypeParam(t.Elem())
	case *types2.Wrapper:
		return mentionsTypeParam(t.Elem())
	case *types2.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if mentionsTypeParam(t.Field(i).Type()) {
				return true
			}
		}
	case *types2.Tuple:
		for i := 0; i < t.Len(); i++ {
			if mentionsTypeParam(t.At(i).Type()) {
				return true
			}
		}
	case *types2.Signature:
		return mentionsTypeParam(t.Params()) || mentionsTypeParam(t.Results())
	case *types2.Interface:
		for i := 0; i < t.NumExplicitMethods(); i++ {
			if mentionsTypeParam(t.ExplicitMethod(i).Type()) {
				return true
			}
		}
	case *types2.TaggedUnion:
		for i := 0; i < t.NumVariants(); i++ {
			if v := t.Variant(i); v.HasPayload() && mentionsTypeParam(v.Payload()) {
				return true
			}
		}
	}
	return false
}
