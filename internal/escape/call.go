package escape

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// A callTarget is what a call expression calls.
type callTarget struct {
	static   *function // a declared function or method, or nil
	name     string    // for messages
	sig      *types2.Signature
	recv     tset // the receiver, for method calls
	recvExpr ast.Expr
	hasRecv  bool
	method   string // dynamic method calls: the method name
	byName   bool   // a method call through a type parameter
	fv       tset   // calls through a func value: the value
	dynFunc  bool
	ring     *types2.Selection // a built-in ring method
	ringX    ast.Expr
}

// call evaluates a call expression and returns what each result may point
// to.
func (w *walker) call(e *ast.CallExpr) []tset {
	tv := w.a.types[e.Fun]
	if tv.IsType() {
		return []tset{w.explicitConv(e, tv.Type)}
	}
	if tv.IsBuiltin() {
		return w.builtin(e)
	}
	t := w.target(e)
	if t.ring != nil {
		return w.ringCall(e, t)
	}
	args := w.args(e, t.sig)
	if len(w.arenas) > 0 && len(args) > 0 && w.isUse(e) {
		w.noteArena(e, args[0])
	}
	cs := &callSite{pos: e.Pos(), exprs: argExprs(e, t.sig)}
	if t.hasRecv {
		args = append([]tset{t.recv}, args...)
		cs.exprs = append([]ast.Expr{t.recvExpr}, cs.exprs...)
	}
	nres := t.sig.Results().Len()
	switch {
	case t.static != nil:
		return w.regionResults(e, t, args, w.apply(t.static, args, cs, t.name, recvExact, nres))
	case t.method != "":
		return w.regionResults(e, t, args, w.dynIface(t, args, cs, nres))
	case t.dynFunc:
		return w.regionResults(e, t, args, w.dynFuncCall(t, args, cs, e.Fun, nres))
	}
	return make([]tset, nres)
}

// A callSite is a call being applied, for messages: its position and the
// expression passed for each parameter, receiver first (nil where unknown).
type callSite struct {
	pos   token.Pos
	exprs []ast.Expr
}

func (cs *callSite) expr(i int) ast.Expr {
	if i < len(cs.exprs) {
		return cs.exprs[i]
	}
	return nil
}

// target works out what e calls, evaluating the receiver or func value.
func (w *walker) target(e *ast.CallExpr) callTarget {
	fun := ast.Unparen(e.Fun)
	if ix, ok := fun.(*ast.IndexExpr); ok {
		if _, isFn := w.typeOf(ix.X).Underlying().(*types2.Signature); isFn {
			fun = ast.Unparen(ix.X)
		}
	} else if ix, ok := fun.(*ast.IndexListExpr); ok {
		fun = ast.Unparen(ix.X)
	}
	sig, _ := w.typeOf(e.Fun).Underlying().(*types2.Signature)
	if sig == nil {
		sig = types2.NewSignatureType(nil, nil, nil, nil, nil, false)
	}

	if se, ok := fun.(*ast.SelectorExpr); ok {
		if sel := w.a.selections[se]; sel != nil {
			fn, _ := sel.Obj().(*types2.Func)
			switch {
			case fn == nil:
			case sel.Kind() == types2.MethodVal:
				if fn.Pkg() == nil && isRing(w.typeOf(se.X)) {
					return callTarget{sig: sig, ring: sel, ringX: se.X}
				}
				recvVar := fn.Type().(*types2.Signature).Recv()
				if recvVar != nil && isInterface(recvVar.Type()) {
					_, byName := types2.Unalias(w.typeOf(se.X)).(*types2.TypeParam)
					if p := pointerElem(w.typeOf(se.X)); p != nil {
						_, byName = types2.Unalias(p).(*types2.TypeParam)
					}
					return callTarget{sig: sig, recv: w.receiver(se.X, sel), hasRecv: true, recvExpr: se.X,
						method: fn.Name(), byName: byName, name: types2.ExprString(se)}
				}
				f := w.a.funcs[fn.Origin()]
				recvExpr := se.X
				if recvVar != nil && pointerElem(recvVar.Type()) != nil && pointerElem(w.typeOf(se.X)) == nil {
					recvExpr = &ast.UnaryExpr{OpPos: se.X.Pos(), Op: token.AND, X: se.X}
				}
				return callTarget{static: f, sig: sig, recv: w.receiver(se.X, sel), hasRecv: true, recvExpr: recvExpr, name: w.funcLabel(f, fn)}
			case sel.Kind() == types2.MethodExpr:
				// T.M(recv, args...): the receiver is the first argument.
				f := w.a.funcs[fn.Origin()]
				return callTarget{static: f, sig: sig, name: w.funcLabel(f, fn)}
			}
		} else if fn, ok := w.objOf(se.Sel).(*types2.Func); ok {
			f := w.a.funcs[fn.Origin()]
			return callTarget{static: f, sig: sig, name: w.funcLabel(f, fn)}
		}
	}
	if id, ok := fun.(*ast.Ident); ok {
		if fn, ok := w.objOf(id).(*types2.Func); ok {
			f := w.a.funcs[fn.Origin()]
			return callTarget{static: f, sig: sig, name: w.funcLabel(f, fn)}
		}
	}
	return callTarget{sig: sig, fv: w.value(e.Fun), dynFunc: true, name: types2.ExprString(e.Fun)}
}

func (w *walker) funcLabel(f *function, fn *types2.Func) string {
	if f != nil {
		if f.pkg.Package != w.f.pkg.Package && fn.Pkg() != nil {
			return fn.Pkg().Name() + "." + f.name
		}
		return f.name
	}
	return fn.Name()
}

func isRing(T types2.Type) bool {
	if el := pointerElem(T); el != nil {
		T = el
	}
	w, ok := underlying(T).(*types2.Wrapper)
	return ok && w.Kind() == types2.WrapRing
}

// args evaluates a call's arguments, converted to the parameter types, with
// variadic arguments packed into a frame array. The result has one entry
// per parameter of sig.
func (w *walker) args(e *ast.CallExpr, sig *types2.Signature) []tset {
	params := sig.Params()
	n := params.Len()
	out := make([]tset, n)
	if len(e.Args) == 1 && n > 1 {
		if tup, ok := w.typeOf(e.Args[0]).(*types2.Tuple); ok && tup.Len() > 1 {
			vals, types := w.multi(e.Args[0], tup.Len())
			for i := range vals {
				if i < n {
					out[i] = w.conv(vals[i], types[i], params.At(i).Type(), e.Args[0])
				}
			}
			return out
		}
	}
	variadic := sig.Variadic() && !e.Ellipsis.IsValid()
	var rest tset
	for i, a := range e.Args {
		var pt types2.Type
		switch {
		case variadic && i >= n-1:
			if s, ok := params.At(n - 1).Type().Underlying().(*types2.Slice); ok {
				pt = s.Elem()
			}
		case i < n:
			pt = params.At(i).Type()
		}
		v := w.conv(w.value(a), w.typeOf(a), pt, a)
		if variadic && i >= n-1 {
			rest.addAll(v)
			if !w.static && (i == len(e.Args)-1 || len(rest) > 0) {
				l := w.tmp(e, lkVarargs)
				w.addContents(l, rest)
			}
			continue
		}
		if i < n {
			out[i] = v
		}
	}
	if variadic && len(e.Args) >= n {
		if w.static {
			// Package initialisation packs them in a static array.
			w.store(tset{heapTok}, rest, e.Pos(), dest{text: "a static argument array"})
			out[n-1] = tset{heapTok}
			return out
		}
		l := w.tmp(e, lkVarargs)
		out[n-1] = tset{frameTok(l)}
	}
	return out
}

// apply applies f's summary to a call with the given arguments (receiver
// first) and returns what the results may point to. mode says how args[0]
// relates to the receiver f sees, for calls through an interface.
func (w *walker) apply(f *function, args []tset, cs *callSite, label string, mode recvMode, nres int) []tset {
	res := make([]tset, nres)
	pos := cs.pos
	passErr := func(i int, why string) *viewErr {
		return &viewErr{sink: sinkCall, pos: pos, expr: cs.expr(i), fn: w.scopeName(w.sc), callee: label, calleeWhy: why}
	}
	if f == nil {
		// Not a function the analysis has seen: assume the worst.
		const why = "is a function the compiler cannot see into, so it may keep it"
		for i, a := range args {
			w.sink(w.reach(a), passErr(i, why), "passes %s to "+label, ", which the escape analysis cannot see")
		}
		for i := range res {
			res[i] = tset{heapTok}
		}
		return res
	}
	arg := func(k pkey) tset {
		if k.idx >= len(args) {
			return nil
		}
		if k.idx == 0 {
			switch mode {
			case recvBoxed:
				return w.walkParam(f, w.load(args[0]), k)
			case recvReach:
				return w.reach(args[0])
			}
		}
		return w.walkParam(f, args[k.idx], k)
	}
	// What the callee's parameters hold, for heap-only fields.
	passed := args
	if len(args) > 0 && mode != recvExact {
		passed = append([]tset{w.reach(args[0])}, args[1:]...)
	}
	w.a.passArgs(f, passed)
	for _, k := range sortedKeys(f.sum.escapes) {
		r := f.sum.escapes[k]
		v := arg(k)
		w.sink(v, passErr(k.idx, r.text), "passes %s to "+label, ", which "+r.text)
	}
	for _, e := range sortedEdges(f.sum.stores) {
		r := f.sum.stores[e]
		d := dest{callee: label, calleeWhy: r.text, src: cs.expr(e.src.idx)}
		w.store(arg(e.dst), arg(e.src), pos, d)
	}
	for i := 0; i < nres && i < len(f.sum.results); i++ {
		rs := f.sum.results[i]
		for _, k := range sortedRkeys(rs.from) {
			res[i].addAll(retag(arg(k.pkey), k.tag))
		}
		tags := make([]string, 0, len(rs.heap))
		for tag := range rs.heap {
			tags = append(tags, tag)
		}
		sort.Strings(tags)
		for _, tag := range tags {
			res[i].add(tok{kind: tHeap, tag: tag})
		}
	}
	return res
}

// walkParam follows k from v, the value passed for parameter k.idx of f. A
// struct parameter's fields are told apart (bindParams): a path that starts
// with a field step selects that field of the struct value passed.
func (w *walker) walkParam(f *function, v tset, k pkey) tset {
	if k.idx < len(f.params) && isStructValue(f.params[k.idx].Type()) {
		if i, rest, ok := cutFieldStep(k.path); ok {
			v = selectField(v, i)
			n := len(k.path) - len(rest)
			k.path = rest
			if k.filt != nil {
				if k.filtAt >= n {
					k.filtAt -= n
				} else {
					k.filt, k.filtAt = nil, 0
				}
			}
		}
	}
	return w.walkKey(v, k)
}

// isStructValue reports whether T is a struct type (not a pointer to one).
func isStructValue(T types2.Type) bool {
	_, ok := underlying(T).(*types2.Struct)
	return ok
}

// cutFieldStep splits a leading field step from a path.
func cutFieldStep(p string) (int, string, bool) {
	i := strings.IndexByte(p, '.')
	if i <= 0 || strings.IndexByte(p[:i], '*') >= 0 {
		return 0, p, false
	}
	n, err := strconv.Atoi(p[:i])
	if err != nil {
		return 0, p, false
	}
	return n, p[i+1:], true
}

// dynIface applies a dynamic method call: the union of the summaries of
// every method it may reach.
func (w *walker) dynIface(t callTarget, args []tset, cs *callSite, nres int) []tset {
	res := make([]tset, nres)
	for i := range res {
		res[i] = tset{heapTok}
	}
	for _, c := range w.a.ifaceCandidates(t.method, t.byName, t.sig) {
		f := c.f
		if len(f.params) != len(args) {
			continue
		}
		cargs := args
		if len(args) > 0 && len(args[0]) > 0 {
			// Drop receivers whose dynamic type is known not to have f.
			recv := filterRecv(args[0], f)
			if len(recv) == 0 {
				continue
			}
			if c.recv != recvReach {
				recv = markRecv(recv, f)
			}
			cargs = append([]tset{recv}, args[1:]...)
		}
		label := fmt.Sprintf("%s (which may call %s.%s)", t.name, f.pkg.Types.Name(), f.name)
		r := w.apply(f, cargs, cs, label, c.recv, nres)
		for i := range res {
			res[i].addAll(r[i])
		}
	}
	return res
}

// filterRecv drops from an interface data word the pointers to frame
// locations whose dynamic type does not have method f.
func filterRecv(ts tset, f *function) tset {
	var out tset
	for _, t := range ts {
		if t.kind == tFrame && t.path == "" && t.loc.typ != nil && f.obj != nil && !containsTypeParam(t.loc.typ) {
			dyn := t.loc.typ
			if !t.loc.box {
				dyn = types2.NewPointer(dyn)
			}
			sel := types2.NewMethodSet(dyn).Lookup(f.obj.Pkg(), f.obj.Name())
			if sel == nil {
				continue
			}
			if m, _ := sel.Obj().(*types2.Func); m == nil || m.Origin() != f.obj {
				continue
			}
		}
		out.add(t)
	}
	return out
}

// markRecv records on the caller's pointers in an interface data word that
// they reach method f only if the interface holds f's receiver type: a
// caller that knows the dynamic type then drops what f's summary says
// about other types (filterDyn).
func markRecv(ts tset, f *function) tset {
	if len(f.params) == 0 {
		return ts
	}
	T := f.params[0].Type()
	if el := pointerElem(T); el != nil {
		T = el
	}
	if containsTypeParam(T) {
		return ts
	}
	var out tset
	for _, t := range ts {
		if t.kind == tParam && t.filt == nil && !t.ge {
			t.filt, t.filtAt = T, len(t.path)
		}
		out.add(t)
	}
	return out
}

// dynFuncCall applies a call through a func value: the union of the
// summaries of every function of that type used as a value. Results may
// point anywhere the arguments or the func value reach.
func (w *walker) dynFuncCall(t callTarget, args []tset, cs *callSite, fun ast.Expr, nres int) []tset {
	res := make([]tset, nres)
	for i := range res {
		res[i] = tset{heapTok}
	}
	var captured tset // computed once, if needed
	// A func value that can only be closure records made here can only
	// call those literals.
	var only map[*ast.FuncLit]bool
	for _, tk := range t.fv {
		lit := w.recLit[tk.loc]
		if tk.kind != tFrame || tk.path != "" || lit == nil {
			only = nil
			break
		}
		if only == nil {
			only = map[*ast.FuncLit]bool{}
		}
		only[lit] = true
	}
	for _, fv := range w.a.funcCandidates(t.sig) {
		f := fv.f
		if only != nil && !only[f.lit] {
			continue
		}
		label := fmt.Sprintf("%s (which may call %s)", t.name, f.name)
		fargs, mode, fcs := args, recvExact, cs
		switch fv.kind {
		case fvBound:
			// A method value: the closure record holds the receiver.
			fargs = append([]tset{w.load(t.fv)}, args...)
			fcs = &callSite{pos: cs.pos, exprs: append([]ast.Expr{fun}, cs.exprs...)}
		case fvBoundIface:
			// The record holds an interface, whose data word leads to the
			// receiver.
			fargs, mode = append([]tset{w.load(t.fv)}, args...), recvReach
			fcs = &callSite{pos: cs.pos, exprs: append([]ast.Expr{fun}, cs.exprs...)}
		case fvIfaceExpr:
			mode = recvReach
		}
		r := w.apply(f, fargs, fcs, label, mode, nres)
		var outer tset
		if f.lit != nil {
			// What a literal returns of the frames around it: known if it
			// was walked here, else anything its record leads to. (The
			// record itself is never visible to its body.)
			if sc := w.scopes[f.lit]; sc != nil {
				outer = sc.outerRet
			} else {
				if captured == nil {
					captured = append(w.reach(w.load(t.fv)), heapTok)
				}
				outer = captured
				if f.regionLeaks {
					// Built in a region, it may return memory of the
					// region itself, which the func value points to.
					outer = union(outer, t.fv)
				}
			}
		}
		for i := range res {
			res[i].addAll(r[i])
			res[i].addAll(outer)
		}
	}
	return res
}

// ----------------------------------------------------------------------------
// Built-in functions and methods

func (w *walker) builtin(e *ast.CallExpr) []tset {
	name := ""
	switch f := ast.Unparen(e.Fun).(type) {
	case *ast.Ident:
		name = f.Name
	case *ast.SelectorExpr:
		name = f.Sel.Name
	}
	switch name {
	case "take":
		// The value moved out of *p.
		if len(e.Args) != 1 {
			return nil
		}
		return []tset{w.load(w.value(e.Args[0]))}
	case "new", "make":
		if name == "new" && len(e.Args) > 0 && !w.a.types[e.Args[0]].IsType() {
			// new(v) stores v in a new heap variable.
			w.store(tset{heapTok}, w.value(e.Args[0]), e.Args[0].Pos(), dest{text: "the variable new(...) allocates", src: e.Args[0]})
		}
		for _, a := range e.Args[1:] {
			w.value(a)
		}
		return []tset{{heapTok}}
	case "copy":
		if len(e.Args) != 2 {
			return nil
		}
		dst := w.value(e.Args[0])
		src := w.value(e.Args[1])
		if st, ok := underlying(w.typeOf(e.Args[0])).(*types2.Slice); ok && hasPointers(st.Elem()) {
			w.store(dst, w.load(src), e.Pos(), dest{text: "the backing array of " + exprText(e.Args[0])})
		}
		return []tset{nil}
	case "Sizeof", "Alignof", "Offsetof":
		return []tset{nil}
	case "Add", "Slice", "SliceData", "StringData", "String":
		if len(e.Args) == 0 {
			return nil
		}
		v := w.value(e.Args[0])
		for _, a := range e.Args[1:] {
			w.value(a)
		}
		if name != "Add" && !hasPointers(elemOrPointee(w.typeOf(e))) {
			// The same address, seen as memory that holds no pointers:
			// nothing can be loaded from it as a pointer, so its path
			// stays exact.
			return []tset{v}
		}
		return []tset{collapse(v)}
	}
	// len, cap, min, max, print, println, panic, close, clear, delete:
	// nothing is kept.
	for _, a := range e.Args {
		w.conv(w.value(a), w.typeOf(a), types2.Universe.Lookup("any").Type(), a)
	}
	n := 0
	if tup, ok := w.typeOf(e).(*types2.Tuple); ok {
		n = tup.Len()
	} else if w.typeOf(e) != nil {
		n = 1
	}
	return make([]tset, n)
}

// ringCall handles the built-in methods of ring[T, N], whose elements are
// stored inline in the ring.
func (w *walker) ringCall(e *ast.CallExpr, t callTarget) []tset {
	r := w.receiver(t.ringX, t.ring)
	var vals []tset
	for _, a := range e.Args {
		vals = append(vals, w.value(a))
	}
	switch t.ring.Obj().Name() {
	case "Push":
		if len(vals) == 1 {
			// The elements are stored inside the ring's own storage.
			if f := w.fieldOf(t.ringX); f != nil {
				w.a.storeField(f, vals[0], w.enclosingFields(t.ringX))
			}
			d := w.destOf(t.ringX)
			d.src = e.Args[0]
			w.store(r, vals[0], e.Pos(), d)
		}
		return []tset{nil}
	case "Pop", "Peek":
		return []tset{w.load(r), nil}
	}
	return make([]tset, t.sig.Results().Len())
}

// ----------------------------------------------------------------------------
// Deterministic iteration over summaries

func lessKey(a, b pkey) bool {
	if a.idx != b.idx {
		return a.idx < b.idx
	}
	if a.path != b.path {
		return a.path < b.path
	}
	if a.ge != b.ge {
		return !a.ge && b.ge
	}
	if a.filtAt != b.filtAt {
		return a.filtAt < b.filtAt
	}
	return filtName(a.filt) < filtName(b.filt)
}

func filtName(t types2.Type) string {
	if t == nil {
		return ""
	}
	return types2.TypeString(t, nil)
}

func sortedKeys(m map[pkey]*reason) []pkey {
	out := make([]pkey, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return lessKey(out[i], out[j]) })
	return out
}

func sortedRkeys(m map[rkey]bool) []rkey {
	out := make([]rkey, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].pkey != out[j].pkey {
			return lessKey(out[i].pkey, out[j].pkey)
		}
		return out[i].tag < out[j].tag
	})
	return out
}

func sortedEdges(m map[edge]*reason) []edge {
	out := make([]edge, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].src != out[j].src {
			return lessKey(out[i].src, out[j].src)
		}
		return lessKey(out[i].dst, out[j].dst)
	})
	return out
}
