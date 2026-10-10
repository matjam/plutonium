package lower

// Closure records that reach a function's results.
//
// A function literal that captures variables, a method value, and the
// closure a closure constructor builds where it is called are closure
// records in the frame of the function that makes them (language spec
// sections 4.5 and 6.3). This file finds the records that may reach a
// function's results: returned directly, or through variables, fields,
// elements, interfaces, pointers and calls. Returning one is a compile error
// (E3100), since the record is gone once the function returns.
//
// The analysis is flow-insensitive and field-insensitive, and when it
// cannot tell it assumes the worst:
//
//   - A value is a set of origins: the closure records made in the
//     function, the function's parameters (for the summaries below), and
//     "other", for func values the analysis knows nothing about. Only
//     expressions of types that can hold a func value carry origins.
//   - Assigning to x.f, x[i] or *x adds the origins to x's root variable;
//     storing through a pointer, slice or map also adds them to a pool that
//     every read through a pointer, and every variable whose address is
//     taken, includes.
//   - A call of a declared function uses its summary: which parameters may
//     reach its results, which may be stored through which of its pointer
//     parameters or through other pointers, and which func parameters it
//     calls. Summaries are computed per instance of a generic function, by
//     the same analysis. Any other call (through a func value or an
//     interface) may return, and store through its arguments, anything
//     passed to it and anything the callee captured.
//   - Nested function literals are analysed as part of the function: their
//     assignments to its variables count, a literal's parameters may receive
//     whatever is passed to a call it is passed to or called by, and what
//     it returns comes back from such a call.

import (
	"fmt"
	"math/bits"
	"strings"

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// A bitset is a set of small integers.
type bitset []uint64

func (b bitset) has(i int) bool {
	return i/64 < len(b) && b[i/64]&(1<<(i%64)) != 0
}

func (b bitset) empty() bool {
	for _, w := range b {
		if w != 0 {
			return false
		}
	}
	return true
}

// add adds i to b and reports whether b changed.
func (b *bitset) add(i int) bool {
	for len(*b) <= i/64 {
		*b = append(*b, 0)
	}
	if (*b)[i/64]&(1<<(i%64)) != 0 {
		return false
	}
	(*b)[i/64] |= 1 << (i % 64)
	return true
}

// union adds o to b and reports whether b changed.
func (b *bitset) union(o bitset) bool {
	changed := false
	for len(*b) < len(o) {
		*b = append(*b, 0)
	}
	for i, w := range o {
		if (*b)[i]|w != (*b)[i] {
			(*b)[i] |= w
			changed = true
		}
	}
	return changed
}

// or returns the union of b and o as a new set.
func (b bitset) or(o bitset) bitset {
	var out bitset
	out.union(b)
	out.union(o)
	return out
}

// from returns the members of b that are at least lo, and below hi if hi
// is not negative.
func (b bitset) from(lo, hi int) bitset {
	var out bitset
	for i, w := range b {
		for w != 0 {
			j := bits.TrailingZeros64(w)
			w &^= 1 << j
			n := i*64 + j
			if n >= lo && (hi < 0 || n < hi) {
				out.add(n)
			}
		}
	}
	return out
}

// members lists the members of b in increasing order.
func (b bitset) members() []int {
	var out []int
	for i, w := range b {
		for w != 0 {
			j := bits.TrailingZeros64(w)
			w &^= 1 << j
			out = append(out, i*64+j)
		}
	}
	return out
}

// first returns the smallest member of b, or -1.
func (b bitset) first() int {
	for i, w := range b {
		if w != 0 {
			return i*64 + bits.TrailingZeros64(w)
		}
	}
	return -1
}

// A closureFlow is the analysis of one function body.
type closureFlow struct {
	l    *Lowerer
	info *types2.Info
	tmap *typeMap
	ts   *fnState // for the type argument helpers (info, tmap and l only)
	body *ast.BlockStmt

	params  []*types2.Var // the receiver, then the parameters: origins 0 to len-1
	results *types2.Tuple
	src     []ast.Node // closure records made in the frame: origin firstSrc()+i
	srcIdx  map[ast.Node]int

	vars      map[*types2.Var]bitset
	ident     map[*types2.Var]bitset // what is assigned to each variable itself, not stored through it
	addressed map[*types2.Var]bool
	pool      bitset                  // values stored through pointers
	litArgs   bitset                  // values passed to calls, which an iterator may yield
	litIn     map[*ast.FuncLit]bitset // what each literal's parameters may receive
	called    bitset                  // parameters called as unknown code, whose state may be stored
	storeTo   map[int]bitset          // values stored through each parameter
	callArgs  map[int]bitset          // the parameters passed to calls of each func parameter
	storeAny  bitset                  // values stored through pointers from elsewhere
	started   bitset                  // values a puroutine may be started with
	changed   bool
}

// A flowSummary says what a function does with its parameters (the
// receiver first): which may reach its results, and which may be stored
// through pointers it can reach.
type flowSummary struct {
	ret      bitset
	store    map[int]bitset // the parameters stored through each parameter
	storeAny bitset         // the parameters stored through other pointers
	calls    bitset         // func parameters it calls, whose captured state may be stored
	callArgs map[int]bitset // the parameters it passes to calls of each func parameter
	starts   bitset         // func parameters it may start puroutines with (gofunc)
	all      bool           // unknown: every parameter does everything

	// -1 once final; otherwise the depth of the outermost summary being
	// computed that it depends on (closureSummary).
	depth int
}

func newClosureFlow(l *Lowerer, info *types2.Info, tmap *typeMap, sig *types2.Signature, body *ast.BlockStmt) *closureFlow {
	f := &closureFlow{
		l: l, info: info, tmap: tmap, body: body,
		ts:        &fnState{l: l, info: info, tmap: tmap},
		results:   sig.Results(),
		srcIdx:    map[ast.Node]int{},
		vars:      map[*types2.Var]bitset{},
		addressed: map[*types2.Var]bool{},
		litIn:     map[*ast.FuncLit]bitset{},
		storeTo:   map[int]bitset{},
		callArgs:  map[int]bitset{},
		ident:     map[*types2.Var]bitset{},
	}
	if r := sig.Recv(); r != nil {
		f.params = append(f.params, r)
	}
	for i := 0; i < sig.Params().Len(); i++ {
		f.params = append(f.params, sig.Params().At(i))
	}
	for i, p := range f.params {
		var b bitset
		b.add(i)
		f.vars[p] = b
		f.ident[p] = b
	}
	return f
}

// run computes the origins of every variable.
func (f *closureFlow) run() {
	for {
		f.changed = false
		ast.Inspect(f.body, f.visit)
		if !f.changed {
			return
		}
	}
}

func (f *closureFlow) typeOf(e ast.Expr) types2.Type {
	T := f.info.TypeOf(e)
	if T == nil {
		return nil
	}
	return f.tmap.subst(T)
}

// carries reports whether a value of type T can hold a func value.
func carries(T types2.Type) bool { return mayHoldFunc(T, map[types2.Type]bool{}) }

func mayHoldFunc(T types2.Type, seen map[types2.Type]bool) bool {
	if T == nil {
		return true
	}
	if seen[T] {
		return false
	}
	seen[T] = true
	switch t := types2.Unalias(T).Underlying().(type) {
	case *types2.Basic:
		return t.Kind() == types2.UnsafePointer
	case *types2.Signature, *types2.Interface, *types2.TypeParam:
		return true
	case *types2.Pointer:
		return mayHoldFunc(t.Elem(), seen)
	case *types2.Slice:
		return mayHoldFunc(t.Elem(), seen)
	case *types2.Array:
		return mayHoldFunc(t.Elem(), seen)
	case *types2.Chan:
		return mayHoldFunc(t.Elem(), seen)
	case *types2.Map:
		return mayHoldFunc(t.Key(), seen) || mayHoldFunc(t.Elem(), seen)
	case *types2.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if mayHoldFunc(t.Field(i).Type(), seen) {
				return true
			}
		}
		return false
	case *types2.Tuple:
		for i := 0; i < t.Len(); i++ {
			if mayHoldFunc(t.At(i).Type(), seen) {
				return true
			}
		}
		return false
	case *types2.Wrapper:
		return mayHoldFunc(t.Elem(), seen)
	}
	return true
}

// reaches reports whether a callee given a value of type T can store a func
// value where the caller can see it: through a pointer, slice, map or
// channel, or in an interface or a value of a type parameter, which may be
// a pointer.
func reaches(T types2.Type) bool { return mayReach(T, true, map[types2.Type]bool{}) }

// refers reports whether a value of type T can refer to memory that holds
// func values, so that what is stored there through another pointer can be
// read through it.
func refers(T types2.Type) bool { return mayReach(T, false, map[types2.Type]bool{}) }

func mayReach(T types2.Type, funcs bool, seen map[types2.Type]bool) bool {
	if T == nil {
		return true
	}
	if seen[T] {
		return false
	}
	seen[T] = true
	switch t := types2.Unalias(T).Underlying().(type) {
	case *types2.Basic:
		return t.Kind() == types2.UnsafePointer
	case *types2.Signature:
		return funcs // a closure's captured variables
	case *types2.Interface, *types2.TypeParam:
		return true
	case *types2.Pointer:
		return carries(t.Elem())
	case *types2.Slice:
		return carries(t.Elem())
	case *types2.Chan:
		return carries(t.Elem())
	case *types2.Map:
		return carries(t.Key()) || carries(t.Elem())
	case *types2.Array:
		return mayReach(t.Elem(), funcs, seen)
	case *types2.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if mayReach(t.Field(i).Type(), funcs, seen) {
				return true
			}
		}
		return false
	case *types2.Tuple:
		for i := 0; i < t.Len(); i++ {
			if mayReach(t.At(i).Type(), funcs, seen) {
				return true
			}
		}
		return false
	case *types2.Wrapper:
		if t.IsPointer() {
			return carries(t.Elem())
		}
		return mayReach(t.Elem(), funcs, seen)
	}
	return true
}

// source returns the origin of the closure record n makes.
func (f *closureFlow) source(n ast.Node) int {
	if i, ok := f.srcIdx[n]; ok {
		return i
	}
	i := f.firstSrc() + len(f.src)
	f.src = append(f.src, n)
	f.srcIdx[n] = i
	return i
}

// other is the origin of func values the analysis knows nothing about:
// declared functions used as values, package variables, the results of
// calls and values loaded through pointers. It is never reported; a call
// through a value that may have it is a call of unknown code.
func (f *closureFlow) other() int { return len(f.params) }

// firstSrc is the origin of the first closure record.
func (f *closureFlow) firstSrc() int { return len(f.params) + 1 }

// unknown returns the set holding only other.
func (f *closureFlow) unknown() bitset {
	var b bitset
	b.add(f.other())
	return b
}

// varOrigins returns the origins of v.
func (f *closureFlow) varOrigins(v *types2.Var) bitset {
	if v == nil {
		return nil
	}
	b := f.vars[v]
	if f.addressed[v] {
		b = b.or(f.pool)
	}
	if isGlobal(v) {
		b = b.or(f.unknown()).or(f.pool)
	}
	return b
}

func (f *closureFlow) addVar(v *types2.Var, m bitset) {
	if v == nil || m.empty() {
		return
	}
	b := f.vars[v]
	if b.union(m) {
		f.vars[v] = b
		f.changed = true
	}
}

func (f *closureFlow) addPool(m bitset) {
	if f.pool.union(m) {
		f.changed = true
	}
}

// root returns the variable whose storage e is part of, if any, and
// whether reaching e from it goes through a pointer, slice or map.
func (f *closureFlow) root(e ast.Expr) (*types2.Var, bool) {
	switch e := ast.Unparen(e).(type) {
	case *ast.Ident:
		v, _ := f.info.ObjectOf(e).(*types2.Var)
		return v, false // nil for _ and a type switch's symbol
	case *ast.SelectorExpr:
		sel := f.info.Selections[e]
		if sel == nil {
			v, _ := f.info.Uses[e.Sel].(*types2.Var)
			return v, false
		}
		v, ind := f.root(e.X)
		return v, ind || sel.Indirect() || f.isPointer(e.X)
	case *ast.IndexExpr:
		v, ind := f.root(e.X)
		_, arr := underlying(f.typeOf(e.X)).(*types2.Array)
		return v, ind || !arr
	case *ast.SliceExpr:
		v, ind := f.root(e.X)
		_, arr := underlying(f.typeOf(e.X)).(*types2.Array)
		return v, ind || !arr
	case *ast.StarExpr:
		if u, ok := ast.Unparen(e.X).(*ast.UnaryExpr); ok && u.Op == token.AND {
			return f.root(u.X) // *&x is x
		}
		v, _ := f.root(e.X)
		return v, true
	case *ast.UnaryExpr:
		if e.Op == token.AND {
			v, ind := f.root(e.X)
			return v, ind
		}
	}
	return nil, true
}

func underlying(T types2.Type) types2.Type {
	if T == nil {
		return nil
	}
	return types2.Unalias(T).Underlying()
}

// isPointer reports whether e is a pointer, or of a type the analysis does
// not know.
func (f *closureFlow) isPointer(e ast.Expr) bool {
	T := f.typeOf(e)
	return T == nil || isPointerLike(T)
}

// assign records that the value with origins m is stored in lhs.
func (f *closureFlow) assign(lhs ast.Expr, m bitset) {
	if m.empty() {
		return
	}
	v, ind := f.root(lhs)
	if ind {
		// Through a pointer: record where it may point, for the summary.
		var to bitset
		if v != nil {
			to = f.ident[v]
			if f.addressed[v] {
				to = to.or(f.pool)
			}
		}
		if v == nil || isGlobal(v) || !to.from(len(f.params), -1).empty() {
			if f.storeAny.union(m) {
				f.changed = true
			}
		}
		for _, j := range to.from(0, len(f.params)).members() {
			b := f.storeTo[j]
			if b.union(m) {
				f.storeTo[j] = b
				f.changed = true
			}
		}
		f.addPool(m)
	} else if _, ok := ast.Unparen(lhs).(*ast.Ident); ok && v != nil {
		b := f.ident[v]
		if b.union(m) {
			f.ident[v] = b
			f.changed = true
		}
	}
	f.addVar(v, m)
}

// isGlobal reports whether v is a package-level variable.
func isGlobal(v *types2.Var) bool {
	return v.Pkg() != nil && v.Parent() == v.Pkg().Scope()
}

// addressOf records that e's address is taken.
func (f *closureFlow) addressOf(e ast.Expr) {
	v, ind := f.root(e)
	if v != nil && !ind && !f.addressed[v] {
		f.addressed[v] = true
		f.changed = true
	}
}

func (f *closureFlow) visit(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.AssignStmt:
		if len(n.Lhs) == len(n.Rhs) {
			for i, lhs := range n.Lhs {
				f.assign(lhs, f.expr(n.Rhs[i]))
			}
		} else if len(n.Rhs) == 1 {
			m := f.expr(n.Rhs[0])
			for _, lhs := range n.Lhs {
				f.assign(lhs, m)
			}
		}
	case *ast.ValueSpec:
		if len(n.Names) == len(n.Values) {
			for i, id := range n.Names {
				f.assign(id, f.expr(n.Values[i]))
			}
		} else if len(n.Values) == 1 {
			m := f.expr(n.Values[0])
			for _, id := range n.Names {
				f.assign(id, m)
			}
		}
	case *ast.RangeStmt:
		m := f.expr(n.X).or(f.unknown()).or(f.pool)
		if _, ok := underlying(f.typeOf(n.X)).(*types2.Signature); ok {
			m = m.or(f.litArgs) // the iterator's arguments to yield
		}
		if n.Key != nil {
			f.assign(n.Key, m)
		}
		if n.Value != nil {
			f.assign(n.Value, m)
		}
		if _, ok := underlying(f.typeOf(n.X)).(*types2.Array); ok && n.Value != nil {
			f.addressOf(n.X)
		}
	case *ast.TypeSwitchStmt:
		var x ast.Expr
		switch a := n.Assign.(type) {
		case *ast.AssignStmt:
			x = a.Rhs[0].(*ast.TypeAssertExpr).X
		case *ast.ExprStmt:
			x = a.X.(*ast.TypeAssertExpr).X
		}
		m := f.expr(x)
		for _, c := range n.Body.List {
			if v, ok := f.info.Implicits[c].(*types2.Var); ok {
				f.addVar(v, m)
			}
		}
	case *ast.SendStmt:
		f.assign(&ast.StarExpr{X: n.Chan}, f.expr(n.Value))
	case *ast.GoStmt:
		// The puroutine runs the function with the arguments, which must
		// outlive this frame.
		m := f.expr(n.Call.Fun)
		for _, a := range n.Call.Args {
			m = m.or(f.expr(a))
		}
		f.start(m)
	case *ast.CallExpr:
		f.call(n)
	case *ast.UnaryExpr:
		if n.Op == token.AND {
			f.addressOf(n.X)
		}
	case *ast.SliceExpr:
		if _, ok := underlying(f.typeOf(n.X)).(*types2.Array); ok {
			f.addressOf(n.X)
		}
	case *ast.FuncLit:
		sig, _ := f.typeOf(n).(*types2.Signature)
		if sig != nil {
			for i := 0; i < sig.Params().Len(); i++ {
				if id := paramIdent(n, i); id != nil {
					v, _ := f.info.Defs[id].(*types2.Var)
					f.addVar(v, f.litIn[n])
				}
			}
		}
	}
	return true
}

// paramIdent returns the identifier of lit's i'th parameter, or nil.
func paramIdent(lit *ast.FuncLit, i int) *ast.Ident {
	for _, fld := range lit.Type.Params.List {
		if len(fld.Names) == 0 {
			if i == 0 {
				return nil
			}
			i--
			continue
		}
		if i < len(fld.Names) {
			return fld.Names[i]
		}
		i -= len(fld.Names)
	}
	return nil
}

// expr returns the origins of e's value. A value that refers to memory
// may see whatever was stored there through another pointer.
func (f *closureFlow) expr(e ast.Expr) bitset {
	m := f.expr1(e)
	if e != nil && !f.pool.empty() && refers(f.typeOf(e)) {
		if tv, ok := f.info.Types[e]; !ok || (!tv.IsType() && tv.Value == nil) {
			m = m.or(f.pool)
		}
	}
	return m
}

func (f *closureFlow) expr1(e ast.Expr) bitset {
	if e == nil {
		return nil
	}
	if tv, ok := f.info.Types[e]; ok && (tv.IsType() || tv.Value != nil) {
		return nil
	}
	switch e := e.(type) {
	case *ast.CallExpr:
		m := f.call(e)
		if !carries(f.typeOf(e)) {
			return nil
		}
		if tv, ok := f.info.Types[e.Fun]; !ok || !tv.IsType() {
			m = m.or(f.unknown())
		}
		return m
	case *ast.KeyValueExpr:
		return f.expr(e.Key).or(f.expr(e.Value))
	}
	if !carries(f.typeOf(e)) {
		return nil
	}
	switch e := e.(type) {
	case *ast.ParenExpr:
		return f.expr(e.X)
	case *ast.Ident:
		switch obj := f.info.ObjectOf(e).(type) {
		case *types2.Var:
			return f.varOrigins(obj)
		case *types2.Func:
			return f.unknown()
		}
	case *ast.FuncLit:
		if len(litCaptures(f.info, e)) == 0 {
			return nil
		}
		// What the literal holds is only reached by calling it, which the
		// analysis follows (litResults).
		var m bitset
		m.add(f.source(e))
		return m
	case *ast.SelectorExpr:
		sel := f.info.Selections[e]
		if sel == nil {
			switch obj := f.info.Uses[e.Sel].(type) {
			case *types2.Var:
				return f.varOrigins(obj)
			case *types2.Func:
				return f.unknown()
			}
			return nil
		}
		switch sel.Kind() {
		case types2.FieldVal:
			m := f.expr(e.X)
			if sel.Indirect() || f.isPointer(e.X) {
				m = m.or(f.pool).or(f.unknown())
			}
			return m
		case types2.MethodVal:
			var m bitset
			m.add(f.source(e))
			return m.or(f.expr(e.X))
		case types2.MethodExpr:
			return f.unknown()
		}
	case *ast.IndexExpr:
		if _, ok := f.info.ObjectOf(identOf(ast.Unparen(e.X))).(*types2.Func); ok && identOf(ast.Unparen(e.X)) != nil {
			return f.unknown() // an instance of a generic function
		}
		m := f.expr(e.X)
		if _, arr := underlying(f.typeOf(e.X)).(*types2.Array); !arr {
			m = m.or(f.pool).or(f.unknown())
		}
		return m
	case *ast.IndexListExpr:
		return f.unknown()
	case *ast.SliceExpr:
		return f.expr(e.X).or(f.pool).or(f.unknown())
	case *ast.StarExpr:
		return f.expr(e.X).or(f.pool).or(f.unknown())
	case *ast.UnaryExpr:
		switch e.Op {
		case token.AND:
			return f.expr(e.X)
		case token.ARROW:
			return f.expr(e.X).or(f.pool).or(f.unknown())
		}
	case *ast.CompositeLit:
		var m bitset
		for _, x := range e.Elts {
			m.union(f.expr(x))
		}
		return m
	case *ast.TypeAssertExpr:
		return f.expr(e.X)
	}
	return nil
}

// litCaptures lists the variables lit uses that are declared in an
// enclosing function, in first-use order.
func litCaptures(info *types2.Info, lit *ast.FuncLit) []*types2.Var {
	var out []*types2.Var
	seen := map[*types2.Var]bool{}
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		v, ok := info.Uses[id].(*types2.Var)
		if !ok || seen[v] || v.IsField() || v.Pkg() == nil || v.Parent() == v.Pkg().Scope() {
			return true
		}
		if v.Pos() >= lit.Pos() && v.Pos() < lit.End() {
			return true
		}
		seen[v] = true
		out = append(out, v)
		return true
	})
	return out
}

// call returns the origins of c's results and records what the callee may
// store.
func (f *closureFlow) call(c *ast.CallExpr) bitset {
	if tv, ok := f.info.Types[c.Fun]; ok && tv.IsType() {
		if len(c.Args) == 1 {
			return f.expr(c.Args[0])
		}
		return nil
	}
	var args []ast.Expr
	var recv ast.Expr
	fun := stripIndex(ast.Unparen(c.Fun))
	if se, ok := fun.(*ast.SelectorExpr); ok {
		if sel := f.info.Selections[se]; sel != nil && sel.Kind() == types2.MethodVal {
			recv = se.X
		}
	}
	if recv != nil {
		args = append(args, recv)
	}
	args = append(args, c.Args...)
	masks := make([]bitset, len(args))
	var all bitset
	for i, a := range args {
		masks[i] = f.expr(a)
		all.union(masks[i])
	}
	if f.litArgs.union(all) {
		f.changed = true
	}
	var cm bitset // the callee's origins, for a call through a func value
	if recv == nil {
		if tv, ok := f.info.Types[c.Fun]; !ok || tv.Value == nil {
			if _, builtin := f.info.ObjectOf(identOf(fun)).(*types2.Builtin); !builtin || identOf(fun) == nil {
				cm = f.expr(c.Fun)
			}
		}
	}
	// Built-in functions.
	if id := identOf(fun); id != nil {
		if b, ok := f.info.ObjectOf(id).(*types2.Builtin); ok {
			switch b.Name() {
			case "copy":
				if len(c.Args) == 2 {
					f.assign(&ast.StarExpr{X: c.Args[0]}, masks[1])
				}
				return nil
			case "append":
				return all
			}
			return all
		}
	}

	var ret, store bitset // store: through any argument that can reach memory
	argStore := make([]bitset, len(args))
	var anyStore bitset // through pointers the arguments do not reach
	if g, targs := f.callee(c); g != nil {
		if f.l.closureCtor(g) != nil {
			// Expanded here: the closure record is in this frame.
			var m bitset
			m.add(f.source(c))
			return m.or(all)
		}
		if isGoFuncCheck(g) {
			f.start(all)
		}
		sum := f.l.closureSummary(g, targs)
		if sum.all {
			r := f.callFuncs(all, all.or(f.unknown()))
			ret = all.or(r).or(f.state(all))
			store = ret
		} else {
			sig := g.Type().(*types2.Signature)
			nrecv := 0
			if sig.Recv() != nil {
				nrecv = 1
			}
			gp := nrecv + sig.Params().Len()
			// param maps each argument to its parameter, or -1 for the
			// results of f(g()), which fill every parameter.
			param := func(i int) int {
				if len(c.Args) == 1 && sig.Params().Len() > 1 && i == len(args)-1 {
					return -1
				}
				p := i
				if recv == nil {
					p += nrecv
				}
				return min(p, gp-1)
			}
			of := func(p int) bitset {
				var m bitset
				for i := range args {
					if q := param(i); q == p || q < 0 {
						m.union(masks[i])
					}
				}
				return m
			}
			for i := range args {
				p := param(i)
				if p < 0 && !sum.starts.empty() || p >= 0 && sum.starts.has(p) {
					f.start(masks[i])
				}
				if p < 0 {
					r := f.callFuncs(masks[i], all.or(f.unknown()))
					ret.union(masks[i].or(r))
					store.union(masks[i].or(r).or(f.state(masks[i])))
					continue
				}
				if sum.ret.has(p) {
					ret.union(masks[i])
				}
				if sum.storeAny.has(p) {
					anyStore.union(masks[i])
				}
				for _, q := range sum.store[p].members() {
					argStore[i].union(of(q))
				}
				if sum.calls.has(p) {
					in := f.unknown()
					for _, q := range sum.callArgs[p].members() {
						in.union(of(q))
					}
					r := f.callFuncs(masks[i], in)
					ret.union(r)
					store.union(r.or(f.state(masks[i])))
				}
			}
		}
	} else if lits := f.knownLits(c.Fun, recv); lits != nil {
		// Literals of this function: the analysis sees what their bodies
		// store, and their parameters receive these arguments.
		for _, lit := range lits {
			f.addLitIn(lit, all)
			ret.union(f.litResults(lit))
		}
	} else {
		// Unknown code: it may return or store anything passed to it, and
		// what the callee captured, and call any func value passed to it.
		if recv != nil {
			cm = masks[0]
		}
		st := f.state(cm.or(all))
		r := f.callFuncs(all.or(cm).or(st), all.or(f.unknown()))
		ret = all.or(st).or(r)
		store = ret
	}
	f.addPool(anyStore)
	for i, a := range args {
		m := argStore[i].or(store)
		if m.empty() {
			continue
		}
		if i == 0 && recv != nil && !f.isPointer(recv) {
			f.assign(recv, m) // a pointer method changes the receiver itself
		} else if reaches(f.typeOf(a)) {
			f.assign(&ast.StarExpr{X: a}, m)
		}
	}
	if !store.empty() {
		f.addPool(store)
	}
	return ret
}

// callFuncs records that the func values with origins m may be called with
// arguments with origins in, and returns the origins of what they may
// return: what the function's own literals among them return (the others
// are covered by state).
func (f *closureFlow) callFuncs(m, in bitset) bitset {
	var r bitset
	for _, lit := range f.lits(m) {
		f.addLitIn(lit, in)
		r.union(f.litResults(lit))
	}
	for _, j := range m.from(0, len(f.params)).members() {
		b := f.callArgs[j]
		if b.union(in.from(0, len(f.params))) {
			f.callArgs[j] = b
			f.changed = true
		}
	}
	return r
}

// start records that a puroutine may be started with the func values with
// origins m.
func (f *closureFlow) start(m bitset) {
	if f.started.union(m) {
		f.changed = true
	}
}

// isGoFuncCheck reports whether g is the runtime's check that a func value
// may start a puroutine (pluto/rt's checkGoFunc, which rt.Go and
// rt.CheckGoFunc call): a function that passes a parameter to it starts
// puroutines with that parameter, as one that passes it to a go statement
// does.
func isGoFuncCheck(g *types2.Func) bool {
	return g.Name() == "checkGoFunc" && g.Pkg() != nil && g.Pkg().Path() == "pluto/rt"
}

// addLitIn records that lit's parameters may receive values with origins m.
func (f *closureFlow) addLitIn(lit *ast.FuncLit, m bitset) {
	b := f.litIn[lit]
	if b.union(m) {
		f.litIn[lit] = b
		f.changed = true
	}
}

// state returns the origins of what calling a func value with origins m
// may store or return: what it captured. The function's own literals are left
// out, since the analysis sees their bodies, and so are its parameters and
// unknown functions, whose captured variables are in other frames; the
// parameters are recorded as called, for the summary.
func (f *closureFlow) state(m bitset) bitset {
	if f.called.union(m.from(0, len(f.params))) {
		f.changed = true
	}
	var out bitset
	for _, i := range m.members() {
		if i < f.firstSrc() {
			continue
		}
		switch n := f.src[i-f.firstSrc()].(type) {
		case *ast.SelectorExpr:
			// A method value's copy of its receiver.
			out.union(f.expr(n.X))
		case *ast.CallExpr:
			// A constructor's closure captures its arguments.
			for _, a := range n.Args {
				out.union(f.expr(a))
			}
			if se, ok := stripIndex(ast.Unparen(n.Fun)).(*ast.SelectorExpr); ok && f.info.Selections[se] != nil {
				out.union(f.expr(se.X))
			}
		}
	}
	return out
}

// lits returns the function literals of this function among the origins m.
func (f *closureFlow) lits(m bitset) []*ast.FuncLit {
	var out []*ast.FuncLit
	for _, i := range m.members() {
		if i >= f.firstSrc() {
			if lit, ok := f.src[i-f.firstSrc()].(*ast.FuncLit); ok {
				out = append(out, lit)
			}
		}
	}
	return out
}

// knownLits returns the function literals of this function that fun, the
// callee of a call with receiver recv, may be, or nil if it may be anything
// else.
func (f *closureFlow) knownLits(fun, recv ast.Expr) []*ast.FuncLit {
	if recv != nil {
		return nil
	}
	if lit, ok := ast.Unparen(fun).(*ast.FuncLit); ok {
		return []*ast.FuncLit{lit}
	}
	m := f.expr(fun)
	if m.empty() {
		return nil
	}
	var lits []*ast.FuncLit
	for _, i := range m.members() {
		if i < f.firstSrc() {
			return nil
		}
		lit, ok := f.src[i-f.firstSrc()].(*ast.FuncLit)
		if !ok {
			return nil
		}
		lits = append(lits, lit)
	}
	return lits
}

// litResults returns the origins of what lit, a literal of this function,
// returns.
func (f *closureFlow) litResults(lit *ast.FuncLit) bitset {
	var m bitset
	sig, _ := f.typeOf(lit).(*types2.Signature)
	if sig == nil || sig.Results().Len() == 0 {
		return nil
	}
	named := hasDefer(lit.Body)
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			for _, e := range n.Results {
				m.union(f.expr(e))
			}
			named = named || len(n.Results) == 0
		}
		return true
	})
	if named {
		for i := 0; i < sig.Results().Len(); i++ {
			m.union(f.varOrigins(f.litResultVar(lit, i)))
		}
	}
	return m
}

// litResultVar returns lit's i'th named result, or nil.
func (f *closureFlow) litResultVar(lit *ast.FuncLit, i int) *types2.Var {
	if lit.Type.Results == nil {
		return nil
	}
	for _, fld := range lit.Type.Results.List {
		if i < len(fld.Names) {
			v, _ := f.info.Defs[fld.Names[i]].(*types2.Var)
			return v
		}
		i -= len(fld.Names)
	}
	return nil
}

// stripIndex returns the function that fun instantiates, or fun.
func stripIndex(fun ast.Expr) ast.Expr {
	switch ix := fun.(type) {
	case *ast.IndexExpr:
		return ast.Unparen(ix.X)
	case *ast.IndexListExpr:
		return ast.Unparen(ix.X)
	}
	return fun
}

// callee returns the declared function or concrete method c calls, with
// its type arguments in this instance, or nil.
func (f *closureFlow) callee(c *ast.CallExpr) (*types2.Func, []types2.Type) {
	g := calledFunc(f.info, c)
	if g == nil {
		return nil, nil
	}
	fun := stripIndex(ast.Unparen(c.Fun))
	if se, ok := fun.(*ast.SelectorExpr); ok && f.info.Selections[se] != nil {
		return g, f.ts.methodTypeArgs(g, se, f.info.TypeOf(se.X))
	}
	id := identOf(fun)
	if id == nil {
		return g, nil
	}
	return g, f.ts.instanceArgs(id)
}

// closureSummary returns what g, instantiated with targs, does with its
// parameters.
func (l *Lowerer) closureSummary(g *types2.Func, targs []types2.Type) *flowSummary {
	key := funcSymName(g, targs)
	if s := l.flowSums[key]; s != nil {
		if s.depth >= 0 {
			l.flowMin = min(l.flowMin, s.depth)
		}
		return s
	}
	d := l.decls[g.Origin()]
	if d == nil || d.decl.Body == nil {
		s := &flowSummary{all: true, depth: -1}
		l.flowSums[key] = s
		return s
	}
	// Recursive calls see the summary as it is computed, starting empty,
	// and the function is analysed again until it stops growing. A summary
	// computed from one still being computed is tentative, and is computed
	// again on each round.
	depth := len(l.flowStack)
	s := &flowSummary{depth: depth, store: map[int]bitset{}, callArgs: map[int]bitset{}}
	l.flowSums[key] = s
	l.flowStack = append(l.flowStack, key)
	outer := l.flowMin
	mark := len(l.flowTent)
	var tmap *typeMap
	if len(targs) > 0 {
		tmap = newTypeMap(l, d.obj, targs)
	}
	low := depth
	for {
		l.flowMin = depth + 1
		f := newClosureFlow(l, d.pkg.Info, tmap, d.obj.Type().(*types2.Signature), d.decl.Body)
		f.run()
		low = l.flowMin
		if !s.merge(f) || low > depth {
			break
		}
		for _, k := range l.flowTent[mark:] {
			delete(l.flowSums, k)
		}
		l.flowTent = l.flowTent[:mark]
	}
	l.flowStack = l.flowStack[:depth]
	if low < depth {
		// Depends on a summary still being computed further out.
		s.depth = low
		l.flowTent = append(l.flowTent, key)
	} else {
		s.depth = -1
		for _, k := range l.flowTent[mark:] {
			if t := l.flowSums[k]; t != nil {
				t.depth = -1
			}
		}
		l.flowTent = l.flowTent[:mark]
	}
	l.flowMin = min(outer, low)
	return s
}

// merge adds what the analysis f found to s and reports whether s grew.
func (s *flowSummary) merge(f *closureFlow) bool {
	np := len(f.params)
	grew := false
	for _, r := range f.returns() {
		for _, m := range f.returned(r) {
			grew = s.ret.union(m.from(0, np)) || grew
		}
	}
	for j, m := range f.storeTo {
		b := s.store[j]
		if b.union(m.from(0, np)) {
			s.store[j] = b
			grew = true
		}
	}
	grew = s.storeAny.union(f.storeAny.from(0, np)) || grew
	grew = s.calls.union(f.called.from(0, np)) || grew
	grew = s.starts.union(f.started.from(0, np)) || grew
	for j, m := range f.callArgs {
		b := s.callArgs[j]
		if b.union(m.from(0, np)) {
			s.callArgs[j] = b
			grew = true
		}
	}
	return grew
}

// returns lists the function's own return statements, leaving out those
// of nested function literals.
func (f *closureFlow) returns() []*ast.ReturnStmt {
	var out []*ast.ReturnStmt
	ast.Inspect(f.body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			out = append(out, n)
		}
		return true
	})
	return out
}

// returned returns the origins of each value r returns. A bare return
// returns the named results; with a deferred call, which may assign to
// them, every return does.
func (f *closureFlow) returned(r *ast.ReturnStmt) []bitset {
	var out []bitset
	if len(r.Results) == 0 || hasDefer(f.body) {
		for i := 0; i < f.results.Len(); i++ {
			out = append(out, f.varOrigins(f.results.At(i)))
		}
	}
	for _, e := range r.Results {
		out = append(out, f.expr(e))
	}
	return out
}

// ----------------------------------------------------------------------------
// Reporting

// checkClosureReturns reports the closure records that job's function may
// return.
func (l *Lowerer) checkClosureReturns(job *funcJob) {
	var body *ast.BlockStmt
	var name string
	switch {
	case job.pkg == nil:
		return
	case job.decl != nil:
		if fn, _ := job.pkg.Info.Defs[job.decl.Name].(*types2.Func); fn == nil || l.closureCtor(fn) != nil {
			return
		}
		body, name = job.decl.Body, job.decl.Name.Name
	case job.lit != nil:
		body, name = job.lit.Body, "the function literal at "+l.shortPos(job.lit.Pos())
	}
	if body == nil || job.sig.Results().Len() == 0 {
		return
	}
	var sig *types2.Signature
	if job.decl != nil {
		sig = job.pkg.Info.Defs[job.decl.Name].Type().(*types2.Signature)
	} else {
		sig = job.pkg.Info.TypeOf(job.lit).(*types2.Signature)
	}
	f := newClosureFlow(l, job.pkg.Info, job.tmap, sig, body)
	f.run()
	for _, r := range f.returns() {
		ms := f.returned(r)
		named := len(ms) - len(r.Results)
		for i, m := range ms {
			src := m.from(f.firstSrc(), -1)
			if src.empty() {
				continue
			}
			var e ast.Expr
			var what string
			pos := r.Pos()
			if i >= named {
				e = r.Results[i-named]
				what, pos = shortExpr(e), e.Pos()
			} else {
				what = f.results.At(i).Name()
			}
			if l.flowReported[pos] {
				break
			}
			l.flowReported[pos] = true
			l.report(pos, f.diagnose(e, what, f.src[src.first()-f.firstSrc()], name, job))
			break
		}
	}
}

// diagnose explains returning what (the expression e, or a named result
// when e is nil), which holds the closure record src makes in the frame of
// the function called name.
func (f *closureFlow) diagnose(e ast.Expr, what string, src ast.Node, name string, job *funcJob) *diag.Diagnostic {
	l := f.l
	direct := e != nil && ast.Unparen(e) == src
	d := &diag.Diagnostic{Code: diag.ClosureReturned}
	switch src := src.(type) {
	case *ast.FuncLit:
		caps := litCaptures(f.info, src)
		v := caps[0]
		owner := l.varOwner(v, job, name)
		if direct {
			d.Msg = fmt.Sprintf("cannot return the function literal: it uses %s, a local variable of %s", v.Name(), owner)
		} else {
			d.Msg = fmt.Sprintf("cannot return %s: it holds the function literal at %s, which uses %s, a local variable of %s", what, l.shortPos(src.Pos()), v.Name(), owner)
		}
		if owner == name {
			d.Text = fmt.Sprintf("A function literal that uses variables of the function around it refers to those variables where they live, in %s's frame (%s is declared at %s). That frame is reclaimed when %s returns.", name, v.Name(), l.shortPos(v.Pos()), name)
		} else {
			d.Text = fmt.Sprintf("A function literal that uses variables of the functions around it is a closure record, holding the code to run and where each variable is, in the frame of the function that makes it: here %s. That frame is reclaimed when %s returns, and the record with it.", name, name)
		}
		if direct && job.decl != nil && onlyParams(caps, f.params) {
			d.Text += fmt.Sprintf("\nIf %s only checks its arguments and returns the literal, make the literal its whole body, so that it is a closure constructor: each call then builds the closure in the caller's frame (spec section 4.5). Checks can move into the literal, or to the caller.", name)
		}
		d.Fix = &diag.Fix{
			Intro: "Return a value that carries its own state instead, such as a struct with a method",
			Code:  fmt.Sprintf("type state struct {\n\t%s %s\n}\n\nfunc (s *state) call() {\n\t... s.%s ...\n}", v.Name(), types2.TypeString(v.Type(), types2.RelativeTo(v.Pkg())), v.Name()),
		}
	case *ast.SelectorExpr:
		mv := types2.ExprString(src)
		if direct {
			d.Msg = fmt.Sprintf("cannot return the method value %s: its closure record is in %s's frame", mv, name)
		} else {
			d.Msg = fmt.Sprintf("cannot return %s: it holds the method value %s (%s), whose closure record is in %s's frame", what, mv, l.shortPos(src.Pos()), name)
		}
		d.Text = fmt.Sprintf("A method value such as %s is a closure record, holding the method to call and a copy of the receiver, in the frame of the function that makes it: here %s. That frame is reclaimed when %s returns, and the record with it.", mv, name, name)
		sel := f.info.Selections[src]
		recv := types2.TypeString(sel.Obj().Type().(*types2.Signature).Recv().Type(), types2.RelativeTo(sel.Obj().Pkg()))
		d.Fix = &diag.Fix{
			Intro: "Return the receiver and let the caller call the method, or return a method expression, which takes the receiver as its first argument",
			Code:  fmt.Sprintf("m := (%s).%s // m(x, ...) calls x.%s(...)", recv, sel.Obj().Name(), sel.Obj().Name()),
		}
	case *ast.CallExpr:
		call := shortExpr(src)
		ctor := types2.ExprString(src.Fun)
		if direct {
			d.Msg = fmt.Sprintf("cannot return %s: %s builds its closure in %s's frame", call, ctor, name)
		} else {
			d.Msg = fmt.Sprintf("cannot return %s: it holds the closure that %s builds in %s's frame (%s)", what, call, name, l.shortPos(src.Pos()))
		}
		d.Text = fmt.Sprintf("%s is a closure constructor: each call is compiled where it is made, and the closure it returns is built in the caller's frame, here %s's (spec section 4.5). That frame is reclaimed when %s returns.", ctor, name, name)
		if job.decl != nil {
			d.Text += fmt.Sprintf("\nIf %s's whole body is one return of a call of %s, %s is a closure constructor too, and its callers build the closure instead.", name, ctor, name)
		}
	}
	return d
}

// shortExpr returns e as source text for a message, with the arguments of
// a call and the elements of a composite literal left out when they hold a
// function literal or are long.
func shortExpr(e ast.Expr) string {
	s := types2.ExprString(e)
	if len(s) <= 40 && !strings.Contains(s, " literal)") {
		return s
	}
	switch e := ast.Unparen(e).(type) {
	case *ast.CallExpr:
		return types2.ExprString(e.Fun) + "(...)"
	case *ast.CompositeLit:
		if e.Type != nil {
			return types2.ExprString(e.Type) + "{...}"
		}
	case *ast.UnaryExpr:
		if e.Op == token.AND {
			return "&" + shortExpr(e.X)
		}
	}
	return s
}

// onlyParams reports whether every variable in vs is one of params.
func onlyParams(vs, params []*types2.Var) bool {
	for _, v := range vs {
		found := false
		for _, p := range params {
			found = found || p == v
		}
		if !found {
			return false
		}
	}
	return true
}

// varOwner names the function that declares v: name, the function job
// lowers, if v is declared there, or the innermost enclosing function that
// does.
func (l *Lowerer) varOwner(v *types2.Var, job *funcJob, name string) string {
	for j := job; j != nil; {
		var lo, hi token.Pos
		var n string
		switch {
		case j.decl != nil:
			lo, hi, n = j.decl.Pos(), j.decl.End(), j.decl.Name.Name
		case j.lit != nil:
			lo, hi, n = j.lit.Pos(), j.lit.End(), "the function literal at "+l.shortPos(j.lit.Pos())
		}
		if j == job {
			n = name
		}
		if v.Pos() >= lo && v.Pos() < hi {
			return n
		}
		if j.parent == nil {
			break
		}
		j = j.parent.job
	}
	return "the function around it"
}
