// Translated by go2pu from internal/escape2/eir/borrow.go at the go-cutover tag; edited by hand since.

package eir

import (
	"compiler/compat"
	"compiler/internal/syntax/ast"
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
// interface that does not count: its In tokens lent (OpBorrow with no
// holder). The holder's view is not added: fmt's %w takes the error back
// through a generic container (vec.Fixed[error]), whose store of a type
// parameter makes no counted copy the summary can show, and fmt's walk
// keeps every operand (D51). A view of a holder kept past it through such
// an interface (returning any(e) of a local e) is not seen
// (docs/plans/2026-10-20-escape-redesign.md, known gaps).
func (fb *fnb) lendView(r Reg) Reg {
	if r == 0 || fb.cur == nil {
		return r
	}
	return fb.emitDst(Instr{Op: OpBorrow, X: r, Pos: fb.pos})
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
