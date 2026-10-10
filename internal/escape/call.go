package escape

import (
	"fmt"
	"sort"

	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// A callTarget is what a call expression calls.
type callTarget struct {
	static  *function // a declared function or method, or nil
	name    string    // for messages
	sig     *types2.Signature
	recv    tset // the receiver, for method calls
	hasRecv bool
	method  string // dynamic method calls: the method name
	byName  bool   // a method call through a type parameter
	fv      tset   // calls through a func value: the value
	dynFunc bool
	ring    *types2.Selection // a built-in ring method
	ringX   ast.Expr
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
	if t.hasRecv {
		args = append([]tset{t.recv}, args...)
	}
	nres := t.sig.Results().Len()
	switch {
	case t.static != nil:
		return w.apply(t.static, args, e.Pos(), t.name, recvExact, nres)
	case t.method != "":
		return w.dynIface(t, args, e.Pos(), nres)
	case t.dynFunc:
		return w.dynFuncCall(t, args, e.Pos(), nres)
	}
	return make([]tset, nres)
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
					return callTarget{sig: sig, recv: w.receiver(se.X, sel), hasRecv: true,
						method: fn.Name(), byName: byName, name: types2.ExprString(se)}
				}
				f := w.a.funcs[fn.Origin()]
				return callTarget{static: f, sig: sig, recv: w.receiver(se.X, sel), hasRecv: true, name: w.funcLabel(f, fn)}
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
			if i == len(e.Args)-1 || len(rest) > 0 {
				l := w.tmp(e, "varargs", "the variadic argument array of a call to "+types2.ExprString(e.Fun), hintLocal)
				w.addContents(l, rest)
			}
			continue
		}
		if i < n {
			out[i] = v
		}
	}
	if variadic && len(e.Args) >= n {
		l := w.tmp(e, "varargs", "the variadic argument array of a call to "+types2.ExprString(e.Fun), hintLocal)
		out[n-1] = tset{frameTok(l)}
	}
	return out
}

// callOperands evaluates everything a call hands over: the func value or
// receiver and the arguments. A go statement uses it.
func (w *walker) callOperands(e *ast.CallExpr) tset {
	if tv := w.a.types[e.Fun]; tv.IsBuiltin() || tv.IsType() {
		return union(w.call(e)...)
	}
	t := w.target(e)
	if t.ring != nil {
		w.ringCall(e, t)
		return nil
	}
	out := union(w.args(e, t.sig)...)
	out.addAll(t.recv)
	out.addAll(t.fv)
	return out
}

// apply applies f's summary to a call with the given arguments (receiver
// first) and returns what the results may point to. mode says how args[0]
// relates to the receiver f sees, for calls through an interface.
func (w *walker) apply(f *function, args []tset, pos token.Pos, label string, mode recvMode, nres int) []tset {
	res := make([]tset, nres)
	if f == nil {
		// Not a function the analysis has seen: assume the worst.
		for _, a := range args {
			w.sink(w.reach(a), pos, "is passed to "+label+", which the escape analysis cannot see", "passes %s to "+label, ", which the escape analysis cannot see")
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
				return w.walkPath(w.load(args[0]), k.path, k.ge)
			case recvEither:
				return w.walkPath(union(args[0], w.load(args[0])), k.path, k.ge)
			case recvReach:
				return w.reach(args[0])
			}
		}
		return w.walkPath(args[k.idx], k.path, k.ge)
	}
	for _, k := range sortedKeys(f.sum.escapes) {
		r := f.sum.escapes[k]
		v := arg(k)
		w.sink(v, pos, "is passed to "+label+", which "+r.text, "passes %s to "+label, ", which "+r.text)
	}
	for _, e := range sortedEdges(f.sum.stores) {
		r := f.sum.stores[e]
		d := dest{callee: label, calleeWhy: r.text}
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

// dynIface applies a dynamic method call: the union of the summaries of
// every method it may reach.
func (w *walker) dynIface(t callTarget, args []tset, pos token.Pos, nres int) []tset {
	res := make([]tset, nres)
	for i := range res {
		res[i] = tset{heapTok}
	}
	for _, c := range w.a.ifaceCandidates(t.method, t.byName) {
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
			cargs = append([]tset{recv}, args[1:]...)
		}
		label := fmt.Sprintf("%s (which may call %s.%s)", t.name, f.pkg.Types.Name(), f.name)
		r := w.apply(f, cargs, pos, label, c.recv, nres)
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

// dynFuncCall applies a call through a func value: the union of the
// summaries of every function of that type used as a value. Results may
// point anywhere the arguments or the func value reach.
func (w *walker) dynFuncCall(t callTarget, args []tset, pos token.Pos, nres int) []tset {
	for _, fv := range w.a.funcCandidates(t.sig) {
		f := fv.f
		label := fmt.Sprintf("%s (which may call %s)", t.name, f.name)
		fargs, mode := args, recvExact
		switch fv.kind {
		case fvBound:
			// A method value: the closure record holds the receiver.
			fargs = append([]tset{w.load(t.fv)}, args...)
		case fvBoundIface:
			// The record holds an interface, whose data word leads to the
			// receiver.
			fargs, mode = append([]tset{w.load(t.fv)}, args...), recvReach
		case fvIfaceExpr:
			mode = recvReach
		}
		w.apply(f, fargs, pos, label, mode, nres)
	}
	// A closure's record itself is never visible to its body, only what it
	// holds: captured variables' addresses and a bound receiver.
	reach := w.reach(union(append(append([]tset(nil), args...), w.load(t.fv))...))
	res := make([]tset, nres)
	for i := range res {
		res[i] = union(reach, tset{heapTok})
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
	case "new", "make":
		for _, a := range e.Args[1:] {
			w.value(a)
		}
		return []tset{{heapTok}}
	case "append":
		if len(e.Args) == 0 {
			return nil
		}
		s := w.value(e.Args[0])
		var xs tset
		if e.Ellipsis.IsValid() && len(e.Args) == 2 {
			xs = w.load(w.value(e.Args[1]))
		} else {
			var et types2.Type
			if st, ok := underlying(w.typeOf(e.Args[0])).(*types2.Slice); ok {
				et = st.Elem()
			}
			for _, a := range e.Args[1:] {
				xs.addAll(w.conv(w.value(a), w.typeOf(a), et, a))
			}
		}
		if st, ok := underlying(w.typeOf(e.Args[0])).(*types2.Slice); ok && hasPointers(st.Elem()) {
			w.store(s, xs, e.Pos(), dest{text: "the backing array of " + types2.ExprString(e.Args[0]) + ", which append writes to"})
		}
		return []tset{s}
	case "copy":
		if len(e.Args) != 2 {
			return nil
		}
		dst := w.value(e.Args[0])
		src := w.value(e.Args[1])
		if st, ok := underlying(w.typeOf(e.Args[0])).(*types2.Slice); ok && hasPointers(st.Elem()) {
			w.store(dst, w.load(src), e.Pos(), dest{text: "the backing array of " + types2.ExprString(e.Args[0]) + ", which copy writes to"})
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
			w.store(r, vals[0], e.Pos(), dest{text: types2.ExprString(t.ringX)})
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
	return !a.ge && b.ge
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
