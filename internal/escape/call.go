package escape

import (
	"fmt"
	"iter"
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
	recvKey  ast.Expr // the receiver as written (recvExpr may add &)
	hasRecv  bool
	method   string // dynamic method calls: the method name
	byName   bool   // a method call through a type parameter
	fv       tset   // calls through a func value: the value
	dynFunc  bool
	ring     *types2.Selection // a built-in ring method
	ringX    ast.Expr
	dropOp   bool // x.Drop() on an owner whose type declares no Drop
}

// call evaluates a call expression and returns what each result may point
// to.
func (w *walker) call(e *ast.CallExpr) []tset {
	tv := w.a.types[e.Fun]
	if tv.IsType() {
		return []tset{w.explicitConv(e, tv.Type)}
	}
	if v := w.variantOf(e.Fun); v != nil {
		// U.V(payload): the union holds what the payload does.
		var out tbuf
		for _, a := range e.Args {
			out.addAll(w.conv(w.value(a), w.typeOf(a), v.Payload(), a))
		}
		return []tset{out.set}
	}
	if tv.IsBuiltin() {
		return w.builtin(e)
	}
	if st := w.a.scoped.Tasks[e]; st != nil {
		w.scopedTask(e, st)
		return nil
	}
	t := w.target(e)
	if t.ring != nil {
		return w.ringCall(e, t)
	}
	if t.dropOp {
		return nil // the early drop keeps nothing
	}
	args := w.args(e, t.sig)
	if len(w.arenas) > 0 && len(args) > 0 && w.isUse(e) {
		w.noteArena(e, args[0])
	}
	cs := &callSite{site: e, pos: e.Pos(), exprs: argExprs(e, t.sig), sig: t.sig}
	cs.keys = cs.exprs
	if n := t.sig.Params().Len(); t.sig.Variadic() && !e.Ellipsis.IsValid() && len(e.Args) >= n {
		cs.packed, cs.packedAt = e.Args[n-1:], n-1
		if t.hasRecv {
			cs.packedAt++
		}
	}
	if t.hasRecv {
		args = append([]tset{t.recv}, args...)
		cs.exprs = append([]ast.Expr{t.recvExpr}, cs.exprs...)
		cs.keys = append([]ast.Expr{t.recvKey}, cs.keys...)
	}
	res := w.applyTarget(e, t, args, cs, t.sig.Results().Len())
	return w.explicitArenaResult(e, t, args, res)
}

// variantOf returns the union variant that fun, as in U.V(payload), names, or
// nil.
func (w *walker) variantOf(fun ast.Expr) *types2.Variant {
	se, ok := ast.Unparen(fun).(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	v, _ := w.a.uses[se.Sel].(*types2.Variant)
	if v == nil || !v.HasPayload() {
		return nil
	}
	return v
}

// A callSite is a call being applied, for messages: its position and the
// expression passed for each parameter, receiver first (nil where unknown).
type callSite struct {
	site  ast.Node // the call expression
	pos   token.Pos
	exprs []ast.Expr
	sig   *types2.Signature
	// keys are the argument expressions as written, receiver first, for
	// the facts recorded about them (facts.go); nil if not recorded.
	// packed are the arguments packed into the variadic parameter at
	// index packedAt of keys.
	keys     []ast.Expr
	packed   []ast.Expr
	packedAt int
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
				if fn.Pkg() == nil && fn.Name() == "Drop" && types2.BoxMethodOf(fn) == nil {
					// The early drop of an owner that declares no Drop
					// (language spec section 18.6): it has no body.
					return callTarget{sig: sig, dropOp: true, recv: w.receiver(se.X, sel), hasRecv: true}
				}
				recvVar := fn.Type().(*types2.Signature).Recv()
				if recvVar != nil && isInterface(recvVar.Type()) {
					_, byName := types2.Unalias(w.typeOf(se.X)).(*types2.TypeParam)
					if p := pointerElem(w.typeOf(se.X)); p != nil {
						_, byName = types2.Unalias(p).(*types2.TypeParam)
					}
					return callTarget{sig: sig, recv: w.receiver(se.X, sel), hasRecv: true, recvExpr: se.X, recvKey: se.X,
						method: fn.Name(), byName: byName, name: types2.ExprString(se)}
				}
				f := w.a.funcs[fn.Origin()]
				recvExpr := se.X
				if recvVar != nil && pointerElem(recvVar.Type()) != nil && pointerElem(w.typeOf(se.X)) == nil {
					recvExpr = &ast.UnaryExpr{OpPos: se.X.Pos(), Op: token.AND, X: se.X}
				}
				return callTarget{static: f, sig: sig, recv: w.receiver(se.X, sel), hasRecv: true, recvExpr: recvExpr, recvKey: se.X, name: w.funcLabel(f, fn)}
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
					v := w.argumentHolders(vals[i], e.Args[0], types[i], fmt.Sprintf("result%d/", i))
					out[i] = w.conv(v, types[i], params.At(i).Type(), e.Args[0])
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
		v := w.argumentHolders(w.value(a), a, w.typeOf(a), "")
		v = w.conv(v, w.typeOf(a), pt, a)
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

// An argEval says what a key of a callee's summary leads to from the pointers
// a call passes (the arguments, receiver first, to the function f; mode
// says how the first relates to the receiver f sees).
type argEval struct {
	w    *walker
	f    *function
	cs   *callSite
	mode recvMode
}

// of is what key k leads to from raw, the pointers passed for parameter k.idx
// (all of them, or some: it takes them one by one).
func (e *argEval) of(k pkey, raw tset) tset {
	w, f, cs, mode := e.w, e.f, e.cs, e.mode
	actual := raw
	if k.borrowed {
		actual = borrowHolders(actual)
	}
	v := w.walkParam(f, actual, k)
	if k.idx == 0 {
		switch mode {
		case recvBoxed:
			v = w.walkParam(f, w.load(raw), k)
		case recvReach:
			v = w.reach(raw)
		}
	}
	if k.counts {
		T := w.parameterValueType(f, cs, k)
		if k.directCount || k.ge || T == nil || isCountedBox(T) {
			v = countedHeap(v)
		}
		generic := T == nil
		if T != nil {
			_, generic = types2.Unalias(T).(*types2.TypeParam)
		}
		if generic || k.directCount || k.ge {
			var out tbuf
			for _, t := range v {
				if t.kind == tParam {
					t.counted = true
					t.directCount = t.directCount || k.directCount
				}
				out.add(t)
			}
			v = out.set
		}
	}
	return v
}

// apply applies f's summary to a call with the given arguments (receiver
// first) and returns what the results may point to. mode says how args[0]
// relates to the receiver f sees, for calls through an interface.
func (w *walker) apply(f *function, args []tset, cs *callSite, label string, mode recvMode, nres int) []tset {
	res := make([]tset, nres)
	pos := cs.pos
	passErr := func(i int, why *reason) *viewErr {
		return &viewErr{sink: sinkCall, pos: pos, expr: cs.expr(i), fn: w.scopeName(w.sc), callee: label, calleeWhy: why}
	}
	w.a.noteCall(f, cs, mode)
	w.a.dep(f)
	if f == nil {
		// Not a function the analysis has seen: assume the worst.
		const why = "is a function the compiler cannot see into, so it may keep it"
		for i, a := range args {
			w.sink(w.reach(a), passErr(i, &reason{text: why}), "passes %s to "+label, ", which the escape analysis cannot see")
		}
		for i := range res {
			res[i] = tset{heapTok}
			if cs.sig != nil && i < cs.sig.Results().Len() && types2.IsCountedError(cs.sig.Results().At(i).Type()) {
				res[i] = countedHeap(res[i])
			}
		}
		return res
	}
	argOf := &argEval{w: w, f: f, cs: cs, mode: mode}
	arg := func(k pkey) tset {
		if k.idx >= len(args) {
			return nil
		}
		return argOf.of(k, args[k.idx])
	}
	borrowArg := func(k pkey) tset {
		v := borrowHolders(arg(k))
		if k.idx < len(f.params) && types2.IsCountedError(f.params[k.idx].Type()) {
			// A pointer or lending interface derived from an error borrows
			// the caller's reference, even through helper or indirect calls.
			if x := cs.expr(k.idx); x != nil {
				v = w.borrowError(x, v)
			}
		}
		return v
	}
	// What the callee's parameters hold, for heap-only fields.
	passed := args
	if len(args) > 0 && mode != recvExact {
		passed = append([]tset{w.reach(args[0])}, args[1:]...)
	}
	w.a.passArgs(f, passed)
	w.noteBodyArgs(f, passed)
	if w.a.report {
		w.reportEscapes(f, args, passErr, label, argOf)
	} else {
		w.applyEscapes(f, args, cs, pos, label, mode, argOf)
	}
	for _, e := range f.sum.sortedStores() {
		r := f.sum.stores[e]
		d := dest{callee: label, calleeWhy: r, src: cs.expr(e.src.idx)}
		v := arg(e.src)
		if T := w.storedType(f, cs, e.dst); T != nil {
			if !w.genericIdentity(f, cs, e.src, T) && !w.a.countedErrorResult(T) && !isCounted(T) {
				v = borrowArg(e.src)
			}
		}
		w.store(arg(e.dst), v, pos, d)
	}
	for _, k := range f.sum.sortedHeapStores() {
		w.store(arg(k), tset{heapTok}, pos, dest{callee: label})
	}
	for i := 0; i < nres && i < len(f.sum.results); i++ {
		rs := &f.sum.results[i]
		var out tbuf
		for _, k := range rs.sortedFrom() {
			v := arg(k.pkey)
			T := f.sig.Results().At(i).Type()
			if cs.sig != nil && i < cs.sig.Results().Len() {
				T = cs.sig.Results().At(i).Type()
			}
			for path := k.tag; path != ""; {
				field, rest, ok := cutFieldStep(path)
				if !ok {
					break
				}
				if selected := fieldType(T, field); selected != nil {
					T = selected
				} else {
					break
				}
				path = rest
			}
			// A generic identity transfer keeps the selected value itself.
			// Its concrete caller decides whether that value owns a reference;
			// it has not borrowed a payload merely by forwarding it.
			if !w.genericIdentity(f, cs, k.pkey, T) && !w.a.countedErrorResult(T) && !isCounted(T) {
				v = borrowArg(k.pkey)
			}
			out.addAll(retag(v, k.tag))
		}
		keys := make([]heapResult, 0, len(rs.heap))
		for key := range rs.heap {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i].tag != keys[j].tag {
				return keys[i].tag < keys[j].tag
			}
			return !keys[i].counted && keys[j].counted
		})
		for _, key := range keys {
			out.add(tok{kind: tHeap, tag: key.tag, counted: key.counted})
		}
		res[i] = out.set
	}
	return res
}

// genericIdentity recognizes a transfer of the selected generic value itself.
func (w *walker) genericIdentity(f *function, cs *callSite, k pkey, T types2.Type) bool {
	_, generic := types2.Unalias(T).(*types2.TypeParam)
	return generic && k.filt == nil && types2.Identical(T, w.parameterValueType(f, cs, k))
}

// parameterValueType resolves the value selected from an actual argument
// by a summary path, including fields passed by value.
func (w *walker) parameterValueType(f *function, cs *callSite, k pkey) types2.Type {
	if k.idx >= len(f.params) {
		return nil
	}
	T := f.params[k.idx].Type()
	if actual := w.typeOf(cs.expr(k.idx)); actual != nil {
		T = actual
	}
	for isStructValue(T) {
		i, rest, ok := cutFieldStep(k.path)
		if !ok {
			break
		}
		T, k.path = fieldType(T, i), rest
	}
	for _, step := range splitPath(k.path) {
		if T == nil {
			return nil
		}
		if step == "*" {
			T = pointerElem(T)
		} else {
			i, _ := strconv.Atoi(strings.TrimSuffix(step, "."))
			if fld := fieldType(T, i); fld != nil {
				T = types2.NewPointer(fld)
			} else {
				return nil
			}
		}
	}
	return T
}

// storedType finds the type of the memory named by a summary's destination.
// Prefer the argument's instantiated type for generic calls such as As.
func (w *walker) storedType(f *function, cs *callSite, k pkey) types2.Type {
	if k.idx >= len(f.params) {
		return nil
	}
	T := f.params[k.idx].Type()
	if x := cs.expr(k.idx); x != nil {
		if actual := w.typeOf(x); actual != nil {
			T = actual
		}
	}
	for _, step := range splitPath(k.path) {
		if step == "*" {
			T = pointerElem(T)
		} else {
			i, _ := strconv.Atoi(strings.TrimSuffix(step, "."))
			if fld := fieldType(T, i); fld != nil {
				T = types2.NewPointer(fld)
			} else {
				return nil
			}
		}
		if T == nil {
			return nil
		}
	}
	return pointerElem(T)
}

// walkParam follows k from v, the value passed for parameter k.idx of f. A
// struct parameter's fields are told apart (bindParams): a path that starts
// with a field step selects that field of the struct value passed.
func (w *walker) walkParam(f *function, v tset, k pkey) tset {
	if k.idx < len(f.params) {
		T := f.params[k.idx].Type()
		for isStructValue(T) {
			if i, rest, ok := cutFieldStep(k.path); ok {
				v = selectField(v, i)
				T = fieldType(T, i)
				n := len(k.path) - len(rest)
				k.path = rest
				if k.filt != nil {
					if k.filtAt >= n {
						k.filtAt -= n
					} else {
						k.filt, k.filtAt, k.filtShape = nil, 0, shapeWhole
					}
				}
			} else {
				break
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
	// Candidate summaries cover the whole program, including bodyless
	// methods' conservative results. Adding an unknown heap result here
	// would discard proof that a method only returns a receiver's field.
	res := make([]tbuf, nres)
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
				recv = markRecv(w.a, recv, f)
			}
			cargs = append([]tset{recv}, args[1:]...)
		}
		label := fmt.Sprintf("%s (which may call %s.%s)", t.name, f.pkg.Types.Name(), f.name)
		r := w.apply(f, cargs, cs, label, c.recv, nres)
		for i := range res {
			res[i].addAll(r[i])
		}
	}
	return bufSets(res)
}

// bufSets returns the sets the buffers built.
func bufSets(bs []tbuf) []tset {
	out := make([]tset, len(bs))
	for i := range bs {
		out[i] = bs[i].set
	}
	return out
}

// boundKeys returns the argument keys of a call through a method value
// fun, whose bound receiver comes first: facts about it are facts about
// the func value, which holds the receiver.
func boundKeys(cs *callSite, fun ast.Expr) []ast.Expr {
	if cs.keys == nil {
		return nil
	}
	return append([]ast.Expr{fun}, cs.keys...)
}

// filterRecv drops from an interface data word the pointers to frame
// locations whose dynamic type does not have method f.
func filterRecv(ts tset, f *function) tset {
	var out tbuf
	for _, t := range ts {
		if t.kind == tFrame && t.path == "" && t.interior() && f.obj != nil {
			// A pointer into the location: of the type of something in it.
			if vt := t.loc.valueType(); vt != nil && !containsTypeParam(vt) {
				if R := recvBase(f.obj); R != nil && !containsTypeParam(R) && !holdsType(vt, R, 0) {
					continue
				}
			}
		} else if dyn := t.loc.dynamicType(); t.kind == tFrame && t.path == "" && dyn != nil && t.shape == shapeWhole && f.obj != nil && !containsTypeParam(dyn) {
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
	return out.set
}

// recvBase returns the type a method is declared on, without the pointer of a
// pointer receiver.
func recvBase(m *types2.Func) types2.Type {
	sig, _ := m.Type().(*types2.Signature)
	if sig == nil || sig.Recv() == nil {
		return nil
	}
	T := sig.Recv().Type()
	if el := pointerElem(T); el != nil {
		return el
	}
	return T
}

// markRecv records on the caller's pointers in an interface data word that
// they reach method f only if the interface holds f's receiver type: a
// caller that knows the dynamic type then drops what f's summary says
// about other types (filterDyn).
func markRecv(a *analyzer, ts tset, f *function) tset {
	if len(f.params) == 0 {
		return ts
	}
	T := f.params[0].Type()
	ptrRecv := false
	if el := pointerElem(T); el != nil {
		T, ptrRecv = el, true
	}
	if containsTypeParam(T) {
		return ts
	}
	var out tbuf
	for _, t := range ts {
		if t.kind == tParam && !t.ge && t.shape != shapeOther {
			switch {
			case t.filt == nil:
				t.filt, t.filtAt, t.filtShape = T, len(t.path), t.shape
			case t.filtAt == len(t.path) && t.filtShape == t.shape && ptrRecv:
				// A type assertion to an interface already constrains
				// this data word, and a token has room for one filter.
				// A pointer method is called only on a pointer, which
				// a box holding a value is not: the filter becomes the
				// interface and a pointer.
				t.filt = a.ptrFilter(t.filt)
			case t.filtAt == len(t.path) && t.filtShape == t.shape && isInterface(t.filt):
				// A value method is called on a T or a *T, which implements
				// the interface asserted (or f would not be its candidate):
				// T says more of the data word than the interface does.
				t.filt = T
			}
		}
		out.add(t)
	}
	return out.set
}

// filterBoundRecv drops from what a closure record holds the pointers to
// frame locations that cannot be the pointer receiver of type T it would
// hold if it were a method value's record: a function literal's record holds
// pointers to the variables it uses, of types of their own.
func filterBoundRecv(ts tset, T types2.Type) tset {
	if _, ptr := T.(*types2.Pointer); !ptr || containsTypeParam(T) {
		return ts
	}
	var out tbuf
	for _, t := range ts {
		if t.kind == tFrame && t.path == "" && t.interior() {
			if vt := t.loc.valueType(); vt != nil && !containsTypeParam(vt) && !holdsType(vt, T.(*types2.Pointer).Elem(), 0) {
				continue
			}
		} else if dyn := t.loc.dynamicType(); t.kind == tFrame && t.path == "" && dyn != nil && t.shape == shapeWhole && !containsTypeParam(dyn) && !types2.Identical(dyn, T) {
			continue
		}
		out.add(t)
	}
	return out.set
}

// dynFuncCall applies a call through a func value: the union of the
// summaries of every function of that type used as a value. Results may
// point anywhere the arguments or the func value reach.
func (w *walker) dynFuncCall(t callTarget, args []tset, cs *callSite, fun ast.Expr, nres int) []tset {
	res := make([]tbuf, nres)
	for i := range res {
		res[i].add(heapTok)
		if i < t.sig.Results().Len() && types2.IsCountedError(t.sig.Results().At(i).Type()) {
			res[i] = tbuf{set: countedHeap(res[i].set)}
		}
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
	body := -1 // whether the func value may be a loop body's closure, once known
	for _, fv := range w.a.funcCandidates(t.sig) {
		f := fv.f
		if only != nil && !only[f.lit] {
			continue
		}
		if f.loopBody {
			// A loop body is called only through what its closure was passed
			// to (noteBodyArgs).
			if body < 0 {
				body = 0
				if w.carriesBody(t.fv) {
					body = 1
				}
			}
			if body == 0 {
				continue
			}
		}
		label := fmt.Sprintf("%s (which may call %s)", t.name, f.name)
		fargs, mode, fcs := args, recvExact, cs
		switch fv.kind {
		case fvBound:
			// A method value: the closure record holds the receiver.
			recv := w.load(t.fv)
			if r := f.sig.Recv(); r != nil {
				if _, ptr := r.Type().(*types2.Pointer); ptr {
					// The record holds the receiver: only a pointer to a
					// T can be there (a literal's record holds pointers
					// to the variables it uses), and the callers of this
					// function know which of theirs are.
					recv = markRecv(w.a, filterBoundRecv(recv, r.Type()), f)
				}
			}
			fargs = append([]tset{recv}, args...)
			fcs = &callSite{site: cs.site, pos: cs.pos, exprs: append([]ast.Expr{fun}, cs.exprs...), keys: boundKeys(cs, fun), packed: cs.packed, packedAt: cs.packedAt + 1}
		case fvBoundIface:
			// The record holds an interface, whose data word leads to the
			// receiver.
			fargs, mode = append([]tset{w.load(t.fv)}, args...), recvReach
			fcs = &callSite{site: cs.site, pos: cs.pos, exprs: append([]ast.Expr{fun}, cs.exprs...), keys: boundKeys(cs, fun), packed: cs.packed, packedAt: cs.packedAt + 1}
		case fvIfaceExpr:
			mode = recvReach
		}
		r := w.apply(f, fargs, fcs, label, mode, nres)
		for i := range res {
			var outer tset
			if f.lit != nil {
				// What a literal returns of the frames around it: known if it
				// was walked here, else anything its record leads to. (The
				// record itself is never visible to its body.)
				if sc := w.scopes[f.lit]; sc != nil {
					outer = sc.outerRet[i]
				} else if w.a.dep(f) && len(f.outerResults[i]) != 0 {
					if captured == nil {
						captured = append(w.reach(w.load(t.fv)), heapTok)
					}
					for path := range f.outerResults[i] {
						outer.addAll(retag(captured, path))
					}
					for path := range f.outerRegionResults[i] {
						// Built in a region, it may return memory of the
						// region itself, which the func value points to.
						outer = union(outer, retag(t.fv, path))
					}
				}
			}
			res[i].addAll(r[i])
			res[i].addAll(outer)
		}
	}
	if nres == 1 && len(args) == 1 && w.a.variantCandidate(t.sig) {
		// A union variant used as a function value: the union it makes holds
		// what the payload does.
		res[0].addAll(args[0])
	}
	return bufSets(res)
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
		// The same address at an offset (unsafe.Add), or seen as the elements
		// of the slice or pointer made: of a type that matters to a filter
		// only if that type may have methods.
		shape := shapeOffset
		if name != "Add" {
			shape = shapeWhole
			if w.a.mayHaveMethods(elemOrPointee(w.typeOf(e))) {
				shape = shapeElement
			}
		}
		return []tset{intoSet(v, shape)}
	}
	// len, cap, min, max, print, println, panic, close, clear, delete:
	// nothing is kept.
	// Only panic keeps its argument in an interface; the others are
	// modelled with the conversion for what the argument reaches.
	for _, a := range e.Args {
		v := w.value(a)
		saved := w.noBox
		w.noBox = name != "panic"
		w.conv(v, w.typeOf(a), types2.Universe.Lookup("any").Type(), a)
		w.noBox = saved
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

func lessKey(a, b pkey) bool { return lessKeyNamed(a, b, filtName) }

func lessKeyNamed(a, b pkey, name func(types2.Type) string) bool {
	if a.idx != b.idx {
		return a.idx < b.idx
	}
	if a.counts != b.counts {
		return !a.counts && b.counts
	}
	if a.directCount != b.directCount {
		return !a.directCount && b.directCount
	}
	if a.borrowed != b.borrowed {
		return !a.borrowed && b.borrowed
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
	if a.filtShape != b.filtShape {
		return a.filtShape < b.filtShape
	}
	return name(a.filt) < name(b.filt)
}

func filtName(t types2.Type) string {
	if t == nil {
		return ""
	}
	return types2.TypeString(t, nil)
}

func sortedKeys(seq iter.Seq[pkey]) []pkey {
	var out []pkey
	for k := range seq {
		out = append(out, k)
	}
	// Ordering compares the text of the filter types last: make each once.
	names := map[types2.Type]string{}
	name := func(t types2.Type) string {
		if t == nil {
			return ""
		}
		s, ok := names[t]
		if !ok {
			s = filtName(t)
			names[t] = s
		}
		return s
	}
	sort.Slice(out, func(i, j int) bool { return lessKeyNamed(out[i], out[j], name) })
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
