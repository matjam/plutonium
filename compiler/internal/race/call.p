// Translated by go2pu from internal/race/call.go at the go-cutover tag; edited by hand since.

package race

import (
	"compiler/compat"
	"compiler/internal/scoped"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
	"pluto/strings"
)

// maxDepth bounds how deeply func-valued arguments are followed into the
// functions they are called by.
const maxDepth = 4

// A callTarget is what a call expression calls.
type callTarget struct {
	static   *function
	name     string
	sig      *types2.Signature
	recv     origins
	recvExpr ast.Expr
	hasRecv  bool
	method   string // calls through an interface or type parameter
	fv       origins
	dyn      bool
	syncRecv bool // a method of a synchronised type
	ring     *types2.Selection
	ringX    ast.Expr
}

// call evaluates a call and returns the origins of its results.
func (w *walker) call(e *ast.CallExpr) []origins {
	tv := w.a.types[e.Fun]
	if tv.IsType() {
		return compat.Clone([]origins{w.conversion(e, tv.Type)})
	}
	w.cpuCall(e)
	if t := w.a.scoped.Tasks[e]; t != nil {
		w.scopedTaskCall(e, t)
		return nil
	}
	if name, recv := scoped.ScopeMethod(w.a.selections, e); name == "Wait" || name == "Drop" {
		if v := w.scopeVar(recv); v != nil {
			defer w.scopeWait(v)
		}
	}
	if w.frame != nil {
		// x.Drop() or a method with a value receiver moves x. A deferred
		// x.Drop() drops x where it runs (section 19.4, Deferred calls).
		if se, ok := ast.Unparen(e.Fun).(*ast.SelectorExpr); ok {
			if id, ok := ast.Unparen(se.X).(*ast.Ident); ok && w.a.moves != nil && (w.a.moves.Moves[id] || w.a.moves.DeferredDrops[e]) {
				if v, ok := w.a.objOf(id).(*types2.Var); ok && !isGlobal(v) {
					w.frame.note(w, pu.New(frameAcc{place: vpath{root: root{kind: rLocal, v: v}}, write: true, move: true, drop: se.Sel.Name == "Drop", pos: id.Pos(), typ: v.Type(), locks: w.st.locks}))
					if w.a.moves.DeferredDrops[e] {
						if w.frame.dropCalls == nil {
							w.frame.dropCalls = map[token.Pos]*types2.Var{}
						}
						w.frame.dropCalls[e.Pos()] = v
					}
				}
			}
		}
	}
	if b := w.builtin(e); b != nil {
		return b
	}
	if k, acquire, ok := w.regionOp(e); ok {
		for _, a := range e.Args {
			w.value(a)
		}
		if k == regAll {
			w.parkPoint()
		} else {
			w.region(k, acquire, e.Pos())
		}
		return pad(nil, w.typeOfCallResults(e))
	}
	if op, recv := w.lockOp(e); op != "" {
		ps := w.lockPlaces(recv)
		w.syncUse(ps.ps, e.Pos())
		if w.isRTLock(recv) {
			switch op {
			case "lock":
				w.region(regLock, true, e.Pos())
			case "unlock":
				w.region(regLock, false, e.Pos())
			}
		}
		if ps.sync {
			return nil // a lock inside a synchronised type: its own business
		}
		switch op {
		case "lock", "rlock":
			if len(ps.ps) == 1 {
				w.st.locks = w.st.locks.with(ps.ps[0], op == "lock")
				w.noteLocked(ps.ps[0])
			}
		case "unlock":
			w.unlock(recv)
		}
		return nil
	}
	if _, ok := w.a.condWait(e); ok && !w.inRuntime() {
		// c.Wait(l) counts as l.Unlock() followed by l.Lock() (spec
		// section 19.3, Held sets), after the call itself.
		defer w.condWait(e)
	}
	if fn := w.calledFunc(e); fn != nil && fn.Pkg() != nil {
		if fn.Pkg().Path() == "pluto/sync/atomic" && fn.Type().(*types2.Signature).Recv() == nil {
			return w.atomicCall(e, fn)
		}
		w.atomicPointerLoad(e, fn)
		if h, trap := w.a.handlerOf(e); h != nil {
			// Installing an interrupt handler starts a party that runs at
			// any point where interrupts are enabled, on any processor and
			// on several at once (spec section 19.3, Interrupt handlers).
			w.value(e.Args[1])
			w.value(e.Args[2])
			w.spawn(pu.New(spawn{pos: e.Pos(), root: compat.Clone(origins{h.asValue()}), multi: true, handler: true, trap: trap, desc: compat.Concat("handler ", h.name)}))
			return pad(nil, 1)
		}
	}
	t := w.target(e)
	if t.ring != nil {
		return w.ringCall(e, t)
	}
	args := w.args(e, t.sig)
	if fn := w.calledFunc(e); fn != nil {
		w.freeCall(e, fn)
		w.rtIntrinsic(e, fn, args)
	}
	w.cpuCallStore(e)
	if fn := w.calledFunc(e); fn != nil && w.a.isBlocking(fn) {
		w.parkPoint() // a call of the function that every wait ends in
	}
	if fn := w.calledFunc(e); scoped.StartsTaskWithFunc(fn) {
		// A runtime or library function that starts a task with its func
		// arguments (language spec section 12.12): the task runs them.
		var root origins
		for i, a := range e.Args {
			if _, ok := w.typeOf(a).Underlying().(*types2.Signature); ok && i < len(args) {
				root.addAll(funcOrigins(args[i]))
			}
		}
		w.spawn(pu.New(spawn{pos: e.Pos(), root: root, multi: true, desc: compat.Concat("a task started by ", fn.Pkg().Name(), ".", fn.Name())}))
	}
	if t.hasRecv {
		args = compat.Append(compat.Clone([]origins{t.recv}), args...)
	}
	if t.syncRecv {
		ps := t.recv.derefOr("*", w.unknownFor(t.recvExpr))
		if pointerElem(w.typeOf(t.recvExpr)) == nil {
			if p, ok := w.place(t.recvExpr); ok {
				ps = p.ps
				if p.sync {
					ps = nil
				}
			}
		}
		w.syncUse(ps, e.Pos())
	}
	nres := t.sig.Results().Len()
	switch {
	case t.static != nil && isOnceDo(t.static) && len(args) == 2 && len(e.Args) == 1:
		w.onceDo(e, t, args[1])
		return nil
	case t.static != nil && w.frame != nil && t.static.lit != nil && t.static.unit == w.fn.unit && w.a.scoped.Lits[t.static.lit] == nil:
		// A literal of the frame being checked, called where it is
		// written: its uses of the frame are not in its summary.
		return pad(w.inlineLit(t.static.lit, args, false, e.Pos()), nres)
	case t.static != nil:
		opts := &applyOpts{noLockEffect: t.syncRecv, argViews: w.argViews(e, t)}
		res, after := w.apply(t.static, args, e.Pos(), t.name, w.st, opts)
		w.st = after
		w.noteCallView(e, opts.held)
		res = pad(res, nres)
		for i := range res {
			if t.static.sum.freshResult(i) || i == 0 && isAllocFunc(t.static) {
				// Memory the callee allocated and handed over: this
				// function's own until it publishes it.
				site := w.alloc(w.a.resultNode(e, i))
				if wr, ok := underlying(t.sig.Results().At(i).Type()).(*types2.Wrapper); ok && (wr.Kind() == types2.WrapSynced || wr.Kind() == types2.WrapShared) {
					// A counted box: its object is reached through the
					// reference, which the synced rule follows.
					res[i] = compat.Clone(origins{{vp: site}})
				} else {
					res[i] = compat.Clone(origins{{vp: site.addr()}})
				}
			}
		}
		return res
	case t.method != "":
		var res []origins
		var out *state
		var held *lockset
		for _, f := range w.a.methodCandidates(t.method, t.sig) {
			opts := &applyOpts{noLockEffect: isSyncMethod(f), argViews: w.argViews(e, t)}
			r, after := w.apply(f, args, e.Pos(), t.name, w.st, opts)
			if opts.held != nil {
				if held != nil {
					m := held.meet(*opts.held)
					opts.held = pu.New(m)
				}
				held = opts.held
			}
			res = unionResults(res, r)
			out = join(out, after)
		}
		if out != nil {
			w.st = out
		}
		w.noteCallView(e, held)
		return pad(res, nres)
	}
	// What a func value returns is not followed: a view it returns is
	// valid under no lock.
	w.noteCallView(e, pu.New(lockset(nil)))
	return pad(w.callValue(t.fv, args, e.Pos(), t.sig, exprText(e.Fun)), nres)
}

// typeOfCallResults returns the number of results of a call.
func (w *walker) typeOfCallResults(e *ast.CallExpr) int {
	if sig, ok := w.typeOf(e.Fun).Underlying().(*types2.Signature); ok {
		return sig.Results().Len()
	}
	return 0
}

func pad(res []origins, n int) []origins {
	for len(res) < n {
		res = compat.Append(res, nil)
	}
	return res
}

func unionResults(a, b []origins) []origins {
	for len(a) < len(b) {
		a = compat.Append(a, nil)
	}
	for i, r := range b {
		a[i] = unionOrigins(a[i], r)
	}
	return a
}

// isAllocFunc reports whether f is one of pluto/alloc's functions that
// return new memory through the unchecked layer: the compiler knows them by
// name, as it knows new.
func isAllocFunc(f *function) bool {
	if f.obj == nil || f.obj.Pkg() == nil || f.obj.Pkg().Path() != "pluto/alloc" || f.sig.Recv() != nil {
		return false
	}
	switch f.obj.Name() {
	case "New", "Owned", "Shared", "Synced":
		return true
	}
	return false
}

// A resultKey names result i of a call.
type resultKey struct {
	call ast.Node
	i    int
}

// resultNode returns a node that stands for the memory result i of call
// returns.
func (a *analyzer) resultNode(call ast.Node, i int) ast.Node {
	if i == 0 {
		return call
	}
	k := resultKey{call, i}
	if n := a.resultNodes[k]; n != nil {
		return n
	}
	n := pu.New(ast.BasicLit{ValuePos: call.Pos() + token.Pos(i)})
	a.resultNodes[k] = n
	return n
}

// funcOrigins keeps the origins that are functions or parameters.
func funcOrigins(s origins) origins {
	var out origins
	for _, o := range s {
		switch o.vp.root.kind {
		case rFunc, rLit, rParam:
			if o.vp.path == "" {
				out = origins(compat.Append(out, origin{vp: o.vp}))
			}
		}
	}
	return out.uniq()
}

// calledFunc returns the declared function a call names, if any.
func (w *walker) calledFunc(e *ast.CallExpr) *types2.Func {
	switch f := ast.Unparen(stripIndex(e.Fun)).(type) {
	case *ast.Ident:
		fn, _ := w.a.uses[f].(*types2.Func)
		return fn
	case *ast.SelectorExpr:
		if sel := w.a.selections[f]; sel != nil {
			fn, _ := sel.Obj().(*types2.Func)
			return fn
		}
		fn, _ := w.a.uses[f.Sel].(*types2.Func)
		return fn
	}
	return nil
}

// atomicCall is a call of one of pluto/sync/atomic's functions: an atomic
// use of what its first argument points to.
func (w *walker) atomicCall(e *ast.CallExpr, fn *types2.Func) []origins {
	var args []origins
	for _, a := range e.Args {
		args = compat.Append(args, w.value(a))
	}
	if len(args) > 0 {
		write := !strings.HasPrefix(fn.Name(), "Load")
		for _, p := range args[0].derefOr("*", w.unknownFor(e.Args[0])) {
			w.access(p, write, true, e.Pos(), nil, false, "")
		}
	}
	return pad(nil, fn.Type().(*types2.Signature).Results().Len())
}

// atomicPointerLoad records the load that a call of Load or Swap on an
// atomic.Pointer[T] makes: an atomic read of the view it holds, which is a
// crossing once tasks run (section 19.3, Crossings case 5). The method's
// body loads an unsafe.Pointer; the call site knows the view's type.
func (w *walker) atomicPointerLoad(e *ast.CallExpr, fn *types2.Func) {
	if fn.Pkg().Path() != "pluto/sync/atomic" || (fn.Name() != "Load" && fn.Name() != "Swap") {
		return
	}
	n := recvNamed(fn)
	se, ok := ast.Unparen(e.Fun).(*ast.SelectorExpr)
	if n == nil || n.Obj().Name() != "Pointer" || !ok {
		return
	}
	var ps []vpath
	if pointerElem(w.typeOf(se.X)) != nil {
		ps = w.quietValue(se.X).derefOr("*", w.unknownFor(se.X))
	} else if pl, ok := w.place(se.X); ok {
		ps = pl.ps
	}
	w.viewLoad = true
	for _, p := range ps {
		w.access(p.extend(".v"), false, true, e.Pos(), w.typeOf(e), false, "")
	}
	w.viewLoad = false
}

// quietValue returns the origins of e without recording its uses again.
func (w *walker) quietValue(e ast.Expr) origins {
	w.quiet++
	v := w.value(e)
	w.quiet--
	return v
}

// conversion evaluates T(x).
func (w *walker) conversion(e *ast.CallExpr, to types2.Type) origins {
	if len(e.Args) != 1 {
		return nil
	}
	v := w.value(e.Args[0])
	from := w.typeOf(e.Args[0])
	if isString(to) != isString(from) && from != nil {
		// string(runes) and string(r) copy into new memory.
		return compat.Clone(origins{{vp: w.alloc(e).addr()}})
	}
	if isUnsafePointer(from) || isUnsafePointer(to) {
		// Memory reinterpreted through unsafe.Pointer is unsafe code's
		// business (memory safety design, section 2): not followed.
		return nil
	}
	w.conv(e.Args[0], to)
	if isBox(from) && !isBox(to) {
		// (*T)(p) of a box: a pointer to its object.
		return addrsOf(v.derefOr(w.derefStep(from), w.unknownFor(e.Args[0])))
	}
	return v
}

func isUnsafePointer(T types2.Type) bool {
	if T == nil {
		return false
	}
	b, ok := underlying(T).(*types2.Basic)
	return ok && b.Kind() == types2.UnsafePointer
}

func isString(T types2.Type) bool {
	if T == nil {
		return false
	}
	b, ok := underlying(T).(*types2.Basic)
	return ok && (b.Kind() == types2.String || b.Kind() == types2.UntypedString)
}

// builtin evaluates a call of a built-in function; it returns nil if e is
// not one.
func (w *walker) builtin(e *ast.CallExpr) []origins {
	var id *ast.Ident
	switch f := ast.Unparen(e.Fun).(type) {
	case *ast.Ident:
		id = f
	case *ast.SelectorExpr:
		id = f.Sel
	}
	if id == nil {
		return nil
	}
	b, ok := w.a.uses[id].(*types2.Builtin)
	if !ok {
		return nil
	}
	vals := func() []origins {
		var out []origins
		for _, a := range e.Args {
			out = compat.Append(out, w.value(a))
		}
		return out
	}
	one := func(o origins) []origins { return compat.Clone([]origins{o}) }
	switch b.Name() {
	case "len", "cap":
		if tv := w.a.types[e]; !tv.Value.IsNil() {
			return one(nil) // the length of an array: a constant
		}
		if len(e.Args) == 1 {
			if T := w.typeOf(e.Args[0]); T != nil && b.Name() == "len" && types2.LenByMethod(T) {
				w.methodCall(e.Args[0], types2.LenMethodName, nil, e.Pos())
				return one(nil)
			}
		}
		vs := vals()
		if len(e.Args) == 1 {
			if T := w.typeOf(e.Args[0]); types2.MapOf(T) != nil {
				// len(m) reads the map's count, which writes change.
				for _, p := range vs[0].derefOr(w.derefStep(T), w.unknownFor(e.Args[0])) {
					w.access(p, false, false, e.Pos(), T, false, "")
				}
			}
		}
		return one(nil)
	case "delete":
		vs := vals()
		if len(vs) == 2 {
			T := w.typeOf(e.Args[0])
			for _, p := range vs[0].derefOr(w.derefStep(T), w.unknownFor(e.Args[0])) {
				w.access(p, true, false, e.Pos(), T, false, "")
			}
		}
		return nil
	case "new":
		for _, a := range e.Args[1:] {
			w.value(a)
		}
		if len(e.Args) > 0 && !w.a.types[e.Args[0]].IsType() {
			w.value(e.Args[0])
		}
		return one(compat.Clone(origins{{vp: w.alloc(e).addr()}}))
	case "make":
		for _, a := range e.Args[1:] {
			w.value(a)
		}
		if _, ok := underlying(w.typeOf(e)).(*types2.Slice); ok {
			return one(compat.Clone(origins{{vp: w.alloc(e).addr()}}))
		}
		if types2.MapOf(w.typeOf(e)) != nil {
			return one(compat.Clone(origins{{vp: w.alloc(e)}})) // a new map (checkMaps)
		}
		return one(nil)
	case "append":
		vs := vals()
		if len(vs) == 0 {
			return one(nil)
		}
		T := w.typeOf(e.Args[0])
		for _, p := range vs[0].derefOr("%", w.unknownFor(e.Args[0])) {
			w.access(p, true, false, e.Pos(), T, false, "")
		}
		for i, v := range vs[1:] {
			w.publishAt(v, e.Args[i+1].Pos(), "appended to a slice")
		}
		return one(unionOrigins(vs[0], origins{{vp: w.alloc(e).addr()}}))
	case "copy":
		vs := vals()
		if len(vs) == 2 {
			for _, p := range vs[0].derefOr("%", w.unknownFor(e.Args[0])) {
				w.access(p, true, false, e.Pos(), w.typeOf(e.Args[0]), false, "")
			}
			if !isString(w.typeOf(e.Args[1])) {
				for _, p := range vs[1].derefOr("%", w.unknownFor(e.Args[1])) {
					w.access(p, false, false, e.Pos(), w.typeOf(e.Args[1]), false, "")
				}
			}
		}
		return one(nil)
	case "clear":
		vs := vals()
		if len(vs) == 1 {
			ps := vs[0].derefOr(w.derefStep(w.typeOf(e.Args[0])), w.unknownFor(e.Args[0]))
			for _, p := range ps {
				w.access(p, true, false, e.Pos(), w.typeOf(e.Args[0]), false, "")
			}
			if sl, ok := underlying(w.typeOf(e.Args[0])).(*types2.Slice); ok {
				// Clearing a slice drops its elements.
				w.dropWrite(ps, sl.Elem(), e.Pos())
			}
		}
		return nil
	case "take":
		if len(e.Args) == 1 {
			if u, ok := ast.Unparen(e.Args[0]).(*ast.UnaryExpr); ok && u.Op == token.AND {
				if ps, ok := w.place(u.X); ok {
					T := w.typeOf(u.X)
					for _, p := range ps.ps {
						w.access(p, false, false, e.Pos(), T, ps.sync, ps.key)
						w.access(p, true, false, e.Pos(), T, ps.sync, ps.key)
					}
					return one(w.placeValue(ps.ps))
				}
			}
			// take(p) moves the object p points to out: a write of all of
			// it that frees it as an object (section 19.3, Memory and data).
			v := w.value(e.Args[0])
			if T := pointerElem(w.typeOf(e.Args[0])); T != nil {
				ps := v.derefOr("*", w.unknownFor(e.Args[0]))
				for _, p := range ps {
					w.access(p, false, false, e.Pos(), T, false, "")
				}
				w.freeing++
				for _, p := range ps {
					w.access(p, true, false, e.Pos(), T, false, "")
				}
				w.freeing--
			}
			return one(v)
		}
		return one(nil)
	case "panic":
		vals()
		w.st = pu.New(state{dead: true})
		return nil
	case "Add", "Slice", "String", "StringData", "SliceData":
		vs := vals()
		if len(vs) > 0 {
			return one(vs[0])
		}
		return one(nil)
	case "Drop":
		// unsafe.Drop destroys what its argument points to, freeing what
		// that value owns: a write of all of it (free.go). The unsafe code
		// that calls it answers for its own uses; the write is what a safe
		// caller of that code is checked against.
		vs := vals()
		if len(vs) == 1 {
			T := pointerElem(w.typeOf(e.Args[0]))
			w.dropWrite(vs[0].derefOr("*", w.unknownFor(e.Args[0])), T, e.Pos())
		}
		return nil
	}
	vals()
	return one(nil)
}

// onceDo follows o.Do(f) (spec section 19.3, Lazy initialization). f runs
// holding o's own lock, so the writes it makes have o in their guard set.
// Once Do has returned, o counts as held in read mode for the rest of the
// function and in the functions it calls, as a held lock does: a read there
// needs no other lock, and a write there does not hold o, so data written
// both inside f and outside has an empty guard set (E3502). An entry of a
// party holds nothing, so a read with no returned Do on some path is E3503.
// A Once the walk cannot name gives f's writes and later reads nothing.
func (w *walker) onceDo(e *ast.CallExpr, t callTarget, f origins) {
	ps := t.recv.derefOr("*", w.unknownFor(t.recvExpr))
	var o *vpath
	if len(ps) == 1 && ps[0].root.kind != rUnknown && !strings.ContainsAny(ps[0].path, "#%") {
		o = pu.New(ps[0])
	}
	w.parkPoint() // Do may wait for another task's f
	if o != nil {
		w.st.locks = w.st.locks.with(*o, true)
	}
	sig, _ := w.typeOf(e.Args[0]).Underlying().(*types2.Signature)
	if sig != nil {
		w.callValue(f, nil, e.Pos(), sig, exprText(e.Args[0]))
	}
	if o != nil {
		w.st.locks = w.st.locks.without(*o).with(*o, false)
	}
}

// isOnceDo reports whether f is sync.Once.Do.
func isOnceDo(f *function) bool {
	if f == nil || f.obj == nil || f.obj.Name() != "Do" {
		return false
	}
	n := recvNamed(f.obj)
	return n != nil && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == "pluto/sync" && n.Obj().Name() == "Once"
}

// lockOp reports whether a call locks or unlocks a mutex, and returns the
// selector of the method.
func (w *walker) lockOp(e *ast.CallExpr) (string, *ast.SelectorExpr) {
	se, ok := ast.Unparen(e.Fun).(*ast.SelectorExpr)
	if !ok {
		return "", nil
	}
	sel := w.a.selections[se]
	if sel == nil || sel.Kind() != types2.MethodVal {
		return "", nil
	}
	fn, ok := sel.Obj().(*types2.Func)
	if !ok || !isLockType(recvNamed(fn)) || w.inRuntime() && recvNamed(fn).Obj().Pkg().Path() == "pluto/rt" {
		// The runtime's own spin locks guard its own variables, which are
		// outside these rules: inside the runtime they are plain calls.
		return "", nil
	}
	switch fn.Name() {
	case "Lock":
		return "lock", se
	case "RLock":
		return "rlock", se
	case "Unlock", "RUnlock":
		return "unlock", se
	case "TryLock":
		return "trylock", se
	case "TryRLock":
		return "tryrlock", se
	}
	return "", nil
}

// recvNamed returns the named type of a method's receiver.
func recvNamed(fn *types2.Func) *types2.Named {
	r := fn.Type().(*types2.Signature).Recv()
	if r == nil {
		return nil
	}
	T := r.Type()
	if p, ok := T.(*types2.Pointer); ok {
		T = p.Elem()
	}
	n, _ := types2.Unalias(T).(*types2.Named)
	return n
}

// noteLocked records that this function locks p.
func (w *walker) noteLocked(p vpath) {
	if w.lockedHere == nil {
		w.lockedHere = map[vpath]bool{}
	}
	w.lockedHere[p] = true
}

// inRuntime reports whether the function being walked is the runtime's.
func (w *walker) inRuntime() bool {
	return w.fn.pkg != nil && (w.fn.pkg.Path == "pluto/rt" || strings.HasPrefix(w.fn.pkg.Path, "pluto/rt/"))
}

// isLockType reports whether n is a mutex: sync.Mutex, sync.RWMutex, or
// the runtime's spin lock rt.Lock (which the standard library's time
// package uses).
func isLockType(n *types2.Named) bool {
	if n == nil || n.Obj().Pkg() == nil {
		return false
	}
	switch compat.Concat(n.Obj().Pkg().Path(), ".", n.Obj().Name()) {
	case "pluto/sync.Mutex", "pluto/sync.RWMutex", "pluto/rt.Lock":
		return true
	}
	return false
}

// isSyncType reports whether T is synchronised by its own methods: a type
// of pluto/sync or pluto/sync/atomic, or the runtime's lock.
func isSyncType(T types2.Type) bool {
	if T == nil {
		return false
	}
	if el := pointerElem(T); el != nil {
		T = el
	}
	n, ok := types2.Unalias(T).(*types2.Named)
	if !ok || n.Obj().Pkg() == nil {
		return false
	}
	switch n.Obj().Pkg().Path() {
	case "pluto/sync", "pluto/sync/atomic", "pluto/rt":
		// The runtime's own records (rt.Task, rt.Lock, rt.Timer) are
		// used only through the runtime, which synchronises them.
		_, isStruct := n.Underlying().(*types2.Struct)
		return isStruct
	}
	return false
}

func isSyncMethod(f *function) bool {
	return f != nil && f.obj != nil && isSyncType(recvType(f.obj))
}

func recvType(fn *types2.Func) types2.Type {
	if r := fn.Type().(*types2.Signature).Recv(); r != nil {
		return r.Type()
	}
	return nil
}

// lockPlaces returns where the mutex of a lock method call is.
func (w *walker) lockPlaces(se *ast.SelectorExpr) places {
	sel := w.a.selections[se]
	path := sel.Index()
	if len(path) > 1 {
		ps, ok := w.fieldPlace(se.X, path[:len(path)-1])
		if !ok {
			return places{}
		}
		return ps
	}
	if T := w.typeOf(se.X); pointerElem(T) != nil {
		return places{ps: w.value(se.X).derefOr(w.derefStep(T), w.unknownFor(se.X))}
	}
	ps, _ := w.place(se.X)
	return ps
}

// unlock releases the mutex of a lock method call: when it is not known
// which mutex, every lock this function took and every lock its caller
// held may have been released.
func (w *walker) unlock(se *ast.SelectorExpr) {
	ps := w.lockPlaces(se)
	if ps.sync {
		return
	}
	if len(ps.ps) == 0 {
		w.st.locks = nil
		w.st.rel = released{all: true}
		w.sum.mayRel = released{all: true}
		w.st.endViews(nil)
		return
	}
	for _, p := range ps.ps {
		w.st.locks = w.st.locks.without(p)
		w.st.endViews(pu.New(p))
		// A mutex this function locks is not one its caller holds (locking
		// it again would never return): releasing it releases nothing of
		// the caller's.
		if !w.lockedHere[p] {
			w.st.rel = w.st.rel.with(p)
			w.sum.mayRel = w.sum.mayRel.with(p)
		}
	}
}

// condWait applies c.Wait(l) of a sync.Cond: l is released while the call
// waits and held in write mode when it returns.
func (w *walker) condWait(e *ast.CallExpr) {
	arg := e.Args[0]
	var ps []vpath
	if T := w.typeOf(arg); pointerElem(T) != nil && !isInterface(T) {
		w.quiet++
		ps = w.value(arg).derefOr(w.derefStep(T), w.unknownFor(arg))
		w.quiet--
	}
	switch {
	case len(ps) == 0:
		w.st.rel = released{all: true}
		w.sum.mayRel = released{all: true}
		w.st.endViews(nil)
	case len(ps) == 1:
		// The Wait returns with the lock held again, so the lock stays
		// held across it, a lock the caller holds included. A view
		// loaded before it ends there (section 19.3, Views loaded under a
		// lock).
		if w.lockedHere[ps[0]] {
			w.st.locks = w.st.locks.with(ps[0], true)
		} else {
			w.sum.mayRel = w.sum.mayRel.with(ps[0])
		}
		w.st.endViews(pu.New(ps[0]))
	default:
		for _, p := range ps {
			if !w.lockedHere[p] {
				w.st.rel = w.st.rel.with(p)
				w.sum.mayRel = w.sum.mayRel.with(p)
			}
			w.st.endViews(pu.New(p))
		}
	}
}

// target works out what a call calls, evaluating its receiver or func
// value.
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
				if recvVar != nil && isInterface(recvVar.Type()) || isTypeParamRecv(sel) {
					return callTarget{sig: sig, recv: w.value(se.X), hasRecv: true, recvExpr: se.X, method: fn.Name(), name: exprText(se)}
				}
				recv, recvExpr := w.receiver(se, sel, fn)
				return callTarget{static: w.a.funcs[fn.Origin()], sig: sig, recv: recv, hasRecv: true, recvExpr: recvExpr,
					name: w.funcLabel(fn), syncRecv: isSyncType(recvType(fn))}
			case sel.Kind() == types2.MethodExpr:
				return callTarget{static: w.a.funcs[fn.Origin()], sig: sig, name: w.funcLabel(fn)}
			}
		} else if fn, ok := w.a.uses[se.Sel].(*types2.Func); ok {
			return callTarget{static: w.a.funcs[fn.Origin()], sig: sig, name: w.funcLabel(fn)}
		}
	}
	if id, ok := fun.(*ast.Ident); ok {
		if fn, ok := w.a.uses[id].(*types2.Func); ok {
			return callTarget{static: w.a.funcs[fn.Origin()], sig: sig, name: w.funcLabel(fn)}
		}
	}
	if lit, ok := fun.(*ast.FuncLit); ok {
		return callTarget{static: w.a.lits[lit], sig: sig, name: "the function literal"}
	}
	return callTarget{sig: sig, fv: w.value(e.Fun), dyn: true, name: exprText(e.Fun)}
}

// receiver evaluates the receiver of a method call x.M: a pointer to x
// for a pointer method called on an addressable x, the value otherwise,
// following embedded fields.
func (w *walker) receiver(se *ast.SelectorExpr, sel *types2.Selection, fn *types2.Func) (origins, ast.Expr) {
	recvVar := fn.Type().(*types2.Signature).Recv()
	ptrRecv := recvVar != nil && pointerElem(recvVar.Type()) != nil
	path := sel.Index()
	if len(path) > 1 {
		ps, ok := w.fieldPlace(se.X, path[:len(path)-1])
		if !ok {
			w.value(se.X)
			return nil, se.X
		}
		if ps.sync {
			return nil, se.X
		}
		F := embeddedType(w.typeOf(se.X), path[:len(path)-1])
		if pointerElem(F) != nil {
			return w.placeValue(ps.ps), se.X
		}
		if ptrRecv {
			return w.addrsOfPlaces(ps.ps), se.X
		}
		return w.placeValue(ps.ps), se.X
	}
	X := w.typeOf(se.X)
	if ptrRecv && pointerElem(X) == nil {
		return w.addrOf(se.X), pu.New(ast.UnaryExpr{OpPos: se.X.Pos(), Op: token.AND, X: se.X})
	}
	if !ptrRecv && pointerElem(X) != nil {
		ps := w.value(se.X).derefOr(w.derefStep(X), w.unknownFor(se.X))
		for _, p := range ps {
			w.access(p, false, false, se.Pos(), pointerElem(X), false, "")
		}
		return w.placeValue(ps), se.X
	}
	if isBox(X) {
		// A pointer method of a box gets a pointer to its object.
		return addrsOf(w.value(se.X).derefOr(w.derefStep(X), w.unknownFor(se.X))), se.X
	}
	return w.value(se.X), se.X
}

// isBox reports whether T is owned[T], shared[T] or synced[T].
func isBox(T types2.Type) bool {
	if T == nil {
		return false
	}
	wr, ok := underlying(T).(*types2.Wrapper)
	return ok && wr.IsPointer()
}

func embeddedType(T types2.Type, path []int) types2.Type {
	for _, i := range path {
		f := fieldOf(T, i)
		if f == nil {
			return nil
		}
		T = f.Type()
	}
	return T
}

func (w *walker) funcLabel(fn *types2.Func) string {
	if fn.Pkg() != nil && w.fn.pkg != nil && fn.Pkg() != w.fn.pkg.Types {
		return compat.Concat(fn.Pkg().Name(), ".", funcName(fn))
	}
	return funcName(fn)
}

func isRing(T types2.Type) bool {
	if el := pointerElem(T); el != nil {
		T = el
	}
	wr, ok := underlying(T).(*types2.Wrapper)
	return ok && wr.Kind() == types2.WrapRing
}

// ringCall is a built-in method of ring[T, N]: a use of the ring's memory.
func (w *walker) ringCall(e *ast.CallExpr, t callTarget) []origins {
	var args []origins
	for _, a := range e.Args {
		args = compat.Append(args, w.value(a))
	}
	var ps []vpath
	if X := w.typeOf(t.ringX); pointerElem(X) != nil {
		ps = w.value(t.ringX).derefOr("*", w.unknownFor(t.ringX))
	} else if p, ok := w.place(t.ringX); ok {
		ps = p.ps
	}
	write := false
	switch t.ring.Obj().Name() {
	case "Push", "Pop", "Reset":
		write = true
	}
	for _, p := range ps {
		w.access(p, false, false, e.Pos(), nil, false, "")
		if write {
			w.access(p, true, false, e.Pos(), nil, false, "")
		}
	}
	for i, a := range args {
		w.publishAt(a, e.Args[i].Pos(), "stored in a ring")
	}
	return pad(compat.Clone([]origins{w.placeValue(extendAll(ps, "[]"))}), t.sig.Results().Len())
}

// args evaluates a call's arguments, one entry per parameter of sig;
// variadic arguments packed into a slice are in memory of the call's own.
func (w *walker) args(e *ast.CallExpr, sig *types2.Signature) []origins {
	params := sig.Params()
	n := params.Len()
	out := compat.Make[origins](n)
	if len(e.Args) == 1 && n > 1 {
		if tup, ok := w.typeOf(e.Args[0]).(*types2.Tuple); ok && tup.Len() > 1 {
			vals := w.multi(e.Args[0], tup.Len())
			for i := range vals {
				if i < n {
					out[i] = vals[i]
				}
			}
			return out
		}
	}
	variadic := sig.Variadic() && !e.Ellipsis.IsValid()
	for i, a := range e.Args {
		var v origins
		if dv, ok := w.deferVals[a]; ok && e == w.deferCall {
			v = dv // evaluated at the defer statement
		} else {
			v = w.value(a)
		}
		switch {
		case variadic && i >= n-1:
			if sl, ok := params.At(n - 1).Type().Underlying().(*types2.Slice); ok {
				w.conv(a, sl.Elem())
			}
		case i < n:
			w.conv(a, params.At(i).Type())
		}
		if variadic && i >= n-1 {
			w.publishAt(v, a.Pos(), "packed into a variadic argument")
			continue
		}
		if i < n {
			out[i] = v
		}
	}
	if variadic && len(e.Args) >= n && n > 0 {
		out[n-1] = compat.Clone(origins{{vp: w.alloc(e).addr()}})
	}
	return out
}

// applyOpts adjusts how a summary is applied.
type applyOpts struct {
	noLockEffect bool // a method of a synchronised type: it leaves the caller's locks as they were
	// argViews are the locks of the view each argument passes (argViews),
	// receiver first; nil for an argument that passes none.
	argViews []*lockset
	// held is set by apply to the locks a view the call returns is
	// valid under (section 19.3, Views loaded under a lock).
	held *lockset
}

// apply applies f's summary to a call with args (receiver first), made in
// state base, and returns the results' origins and the state after it.
func (w *walker) apply(f *function, args []origins, pos token.Pos, name string, base *state, opts *applyOpts) ([]origins, *state) {
	after := base.clone()
	unknown := f == nil || f.bodyless
	for i, a := range args {
		if !unknown && !f.sum.pubParams.has(i) {
			continue
		}
		// The callee stores, passes on or keeps what this argument points
		// to: it is published.
		w.publishAt(a, pos, compat.Concat("passed to ", name, ", which may keep it"))
		for _, o := range a {
			if o.vp.root.kind == rFresh {
				after.pub[o.vp.root.node] = true
			}
		}
	}
	if unknown || base.dead {
		return nil, after
	}
	if w.u != nil && f.unit != nil && f.unit != w.u {
		// The walk reads f's summary: when it changes, w.u is walked again.
		if f.unit.users == nil {
			f.unit.users = map[*unit]bool{}
		}
		f.unit.users[w.u] = true
	}
	s := f.sum
	// An argument the analysis cannot follow points to unknown memory, one
	// place per call and parameter.
	full := compat.Make[origins](len(args))
	for i, a := range args {
		full[i] = a
		if k := (capSite{pos, i}); len(a) > maxOrigins || w.a.capSites[k] {
			// Once cut, always: else a set that shrinks by being cut
			// elsewhere would grow back, and the summaries never settle.
			if w.a.capSites == nil {
				w.a.capSites = map[capSite]bool{}
			}
			w.a.capSites[k] = true
			full[i] = a.cut()
		}
		if len(a) == 0 {
			full[i] = compat.Clone(origins{{vp: vpath{root: root{kind: rUnknown, pos: pos, idx: i}}}})
		}
	}
	args = full
	fr := frame{pos: pos, callee: name}
	if w.quiet == 0 {
		// What the callee does with what its parameters reach depends on
		// the arguments: it is instantiated here. The rest of what it
		// does (package variables, synced objects) is the same at every
		// call, so the caller keeps an edge to it instead of a copy, and
		// the edges are followed once per task in the end (check.go).
		w.sum.addEdge(pu.New(edge{f: f, args: args, locks: base.locks, rel: base.rel, pre: !base.spawned}))
		if w.frame != nil {
			w.frame.noteEdge(w, f, args, base.locks, pos)
		}
		// The callee's accesses instantiated here are the same as at the
		// last walk when its summary and the context are: they are kept
		// per call (memo) and added again instead of worked out again.
		var mk applyKey
		var me *applyMemo
		var seen map[*access]bool // what me holds, as one access may come from many
		var argViews []*lockset
		if opts != nil {
			argViews = opts.argViews
		}
		memo := w.frame == nil && !w.a.cpuOn && !hasFresh(args) && argViews == nil
		if memo {
			mk = applyKey{pos: pos, f: f}
			ctx := (&edge{args: args, locks: base.locks, rel: base.rel, pre: !base.spawned}).key()
			if m := w.a.applyMemo[mk]; m != nil && m.sum == s && m.ctx == ctx {
				for _, x := range m.accs {
					w.sum.addAccess(x)
				}
				for _, x := range m.typed {
					w.sum.addTyped(x)
				}
				goto rest
			}
			me = pu.New(applyMemo{sum: s, ctx: ctx})
			seen = map[*access]bool{}
		}
		for _, x := range s.accesses {
			if x.place.root.kind != rParam {
				continue
			}
			ps := substitute(x.place, args)
			w.a.subs += len(ps)
			if len(ps) == 0 {
				continue
			}
			locks := compose(base.locks, substRel(x.rel, args), substLocks(x.locks, args))
			rel := base.rel.union(substRel(x.rel, args))
			for _, p := range ps {
				if w.frame != nil {
					w.frame.noteSub(w, p, x, locks, pos)
					continue
				}
				if p.root.kind == rGlobal && w.a.cpuVars[p.root.v] {
					var ok bool
					inh := compose(base.locks, substRel(x.rel, args), nil)
					if x.view && !x.write && !hasDeref(p.path) {
						w.sum.addAccess(cpuViewLoad(p, x.pos, x.typ, locks, rel, !base.spawned && x.pre))
					}
					if p, ok = w.cpuPlace(p, x.pos, locks, rel, !base.spawned && x.pre, true, x.write, &inh, w.sum.addAccess); !ok {
						continue
					}
				}
				if x.tkey != "" && p.root.kind != rParam {
					init := w.initializing(p, base) && !s.pubParams.has(x.place.root.idx)
					pre := w.isMain() && !base.spawned && x.pre
					w.a.recordType(x.tkey, p, x.write, x.atomic, locks, x.pos, name, init || pre)
				}
				if !w.a.shareable(p) {
					continue
				}
				na := pu.New(access{
					tkey:   x.tkey,
					place:  p,
					write:  x.write,
					atomic: x.atomic,
					locks:  locks,
					rel:    rel,
					pos:    x.pos,
					pre:    !base.spawned && x.pre,
					typ:    x.typ,
					free:   x.free,
					view:   x.view,
					sync:   x.sync,
				})
				na.hasValid, na.vvalid = viewValid(x, locks, base.locks, args, argViews, s.mayRel)
				na.vpre = x.hasValid && x.vpre && !base.spawned
				// The memo holds the summary's own accesses, not copies.
				acc, ty := w.sum.putAccess(na)
				if me != nil {
					if !seen[acc] {
						seen[acc] = true
						me.accs = compat.Append(me.accs, acc)
					}
					if ty != nil && !seen[ty] {
						seen[ty] = true
						me.typed = compat.Append(me.typed, ty)
					}
				}
			}
		}
		for _, x := range s.typed {
			if w.frame != nil {
				break
			}
			locks := compose(base.locks, substRel(x.rel, args), substLocks(x.locks, args))
			tps := substitute(x.place, args)
			w.a.subs += len(tps)
			for _, p := range tps {
				if p.root.kind != rParam {
					// The place is cut, so it is not memory being initialised.
					w.a.recordType(x.tkey, p, x.write, x.atomic, locks, x.pos, name, false)
					continue
				}
				t := pu.New(*x)
				t.place = vpath{root: p.root, path: "#"}
				t.locks = locks
				t.rel = base.rel.union(substRel(x.rel, args))
				t.pre = !base.spawned && x.pre
				ty := w.sum.putTyped(t)
				if me != nil && !seen[ty] {
					seen[ty] = true
					me.typed = compat.Append(me.typed, ty)
				}
			}
		}
		if me != nil {
			if w.a.applyMemo == nil {
				w.a.applyMemo = map[applyKey]*applyMemo{}
			}
			w.a.applyMemo[mk] = me
		}
	rest:
		for _, c := range s.cross {
			var vals origins
			for _, o := range c.vals {
				for _, v := range substitute(o.vp, args) {
					vals = origins(compat.Append(vals, origin{vp: v, tag: o.tag}))
				}
			}
			cc := pu.New(*c)
			cc.vals = vals.uniq()
			cc.via = compat.Append(compat.Clone([]frame{fr}), c.via...)
			w.sum.addCrossing(cc)
		}
		for _, sp := range s.spawns {
			ns := pu.New(spawn{pos: sp.pos, multi: sp.multi, handler: sp.handler, trap: sp.trap, desc: sp.desc, via: compat.Append(compat.Clone([]frame{fr}), sp.via...)})
			for _, o := range sp.root {
				for _, v := range substitute(o.vp, args) {
					ns.root = origins(compat.Append(ns.root, origin{vp: v}))
				}
			}
			ns.root = ns.root.uniq()
			for _, a := range sp.args {
				var na origins
				for _, o := range a {
					for _, v := range substitute(o.vp, args) {
						na = origins(compat.Append(na, origin{vp: v, tag: o.tag}))
					}
				}
				ns.args = compat.Append(ns.args, na.uniq())
			}
			w.sum.addSpawn(ns)
		}
		if w.depth < maxDepth {
			for _, c := range s.calls {
				in := base.clone()
				in.locks = compose(base.locks, substRel(c.rel, args), substLocks(c.locks, args))
				in.rel = base.rel.union(substRel(c.rel, args))
				var fv origins
				if c.idx < len(args) {
					fv = args[c.idx]
				}
				var sig *types2.Signature
				if c.idx < len(f.params) {
					sig, _ = f.params[c.idx].Type().Underlying().(*types2.Signature)
				}
				w.depth++
				saved, savedConc := w.st, w.conc
				w.st = in
				if (c.conc || w.conc) && w.frame == nil {
					w.conc = true
					pname := "the function it is given"
					if c.idx < len(f.params) && f.params[c.idx].Name() != "" {
						pname = f.params[c.idx].Name()
					}
					w.a.noteConcCall(fv, pos, name, pname)
				}
				w.callValueIn(fv, nil, c.pos, sig, compat.Concat("a function passed to ", name))
				w.st, w.conc = saved, savedConc
				w.depth--
			}
		}
	}
	if s.maySpawn {
		after.spawned = true
		w.sum.maySpawn = true
	}
	if s.mayAlloc {
		w.sum.mayAlloc = true
	}
	held := base.locks
	if opts == nil || !opts.noLockEffect {
		acq := substLocks(s.exitLocks, args)
		for _, l := range acq {
			w.noteLocked(l.place)
		}
		// A call that may release a lock ends the views loaded under it,
		// even when it returns with the lock held (section 19.3, Views
		// loaded under a lock).
		may := substRel(s.mayRel, args).union(substRel(s.exitRel, args))
		if may.all {
			after.endViews(nil)
		}
		for _, k := range may.keys {
			after.endViews(pu.New(k))
			if !w.lockedHere[k] {
				w.sum.mayRel = w.sum.mayRel.with(k)
			}
		}
		if may.all {
			w.sum.mayRel = released{all: true}
		}
		held = compose(base.locks, may, nil)
		rel := substRel(s.exitRel, args)
		after.locks = compose(base.locks, rel, acq)
		if !rel.all {
			// Releasing a mutex this function locks releases nothing of
			// its caller's.
			var keep released
			for _, k := range rel.keys {
				if !w.lockedHere[k] {
					keep = keep.with(k)
				}
			}
			rel = keep
		}
		after.rel = base.rel.union(rel)
	}
	if opts != nil {
		// A view the call returns counts as loaded at the call, under the
		// locks the caller holds across the whole call; one it was passed
		// is valid no longer than it was.
		for _, av := range opts.argViews {
			if av != nil {
				held = held.meet(*av)
			}
		}
		opts.held = pu.New(held)
	}
	var res []origins
	for _, r := range s.results {
		var out origins
		for _, o := range r {
			for _, v := range substitute(o.vp, args) {
				out = origins(compat.Append(out, origin{vp: v, tag: o.tag}))
			}
		}
		res = compat.Append(res, out.uniq())
	}
	return res, after
}

// compose returns the locks held after releasing rel from base and taking
// acq.
func compose(base lockset, rel released, acq lockset) lockset {
	var out lockset
	if !rel.all {
		for _, l := range base {
			if !rel.has(l.place) {
				out = out.with(l.place, l.excl)
			}
		}
	}
	for _, l := range acq {
		out = out.with(l.place, l.excl)
	}
	return out
}

// viewValid works out, for an access x of a callee made through a view
// (access.vvalid), the locks held from the load of the view to it with no
// ending event, at a call in a state holding base whose arguments pass
// views with argViews: what the callee's summary records for a view it
// loaded, and, for a view an argument passes, the locks held from its
// load to the call that the callee does not release before x.
func viewValid(x *access, locks, base lockset, args []origins, argViews []*lockset, mayRel released) (bool, lockset) {
	var av *lockset
	if x.place.root.kind == rParam && x.place.root.idx < len(argViews) {
		av = argViews[x.place.root.idx]
	}
	if !x.hasValid && av == nil {
		return false, nil
	}
	vv := locks
	if x.hasValid {
		vv = compose(base, substRel(x.rel, args), substLocks(x.vvalid, args))
	}
	if av != nil {
		// A view passed to a call that may release its lock counts as
		// used after the release.
		vv = vv.meet(compose(*av, substRel(x.rel, args).union(substRel(mayRel, args)), nil))
	}
	return true, vv
}

// substLocks substitutes the caller's arguments into locks named through
// parameters; a lock that is not one known mutex is dropped.
func substLocks(ls lockset, args []origins) lockset {
	var out lockset
	for _, l := range ls {
		ps := substitute(l.place, args)
		if len(ps) == 1 && !ps[0].isAddr() {
			out = out.with(ps[0], l.excl)
		}
	}
	return out
}

func substRel(r released, args []origins) released {
	if r.all {
		return r
	}
	var out released
	for _, k := range r.keys {
		ps := substitute(k, args)
		if len(ps) == 0 {
			return released{all: true}
		}
		for _, p := range ps {
			out = out.with(p)
		}
	}
	return out
}

// callValue calls a func value with origins fv.
func (w *walker) callValue(fv origins, args []origins, pos token.Pos, sig *types2.Signature, name string) []origins {
	return w.callValueIn(fv, args, pos, sig, name)
}

func (w *walker) callValueIn(fv origins, args []origins, pos token.Pos, sig *types2.Signature, name string) []origins {
	fv = funcOrigins(fv)
	var res []origins
	var out *state
	base := w.st
	applyTo := func(f *function, recvBound bool) {
		a := args
		if recvBound {
			a = compat.Append(compat.Clone([]origins{nil}), args...)
		}
		r, after := w.apply(f, a, pos, name, base, nil)
		res = unionResults(res, r)
		out = join(out, after)
	}
	if len(fv) == 0 {
		if sig != nil {
			for _, f := range w.a.valueCandidates(sig, true) {
				applyTo(f, f.obj != nil && f.sig.Recv() != nil && f.sig.Params().Len() == sig.Params().Len())
			}
		}
	}
	for _, o := range fv {
		switch o.vp.root.kind {
		case rFunc:
			if f := w.a.funcs[o.vp.root.fn.Origin()]; f != nil {
				applyTo(f, f.sig.Recv() != nil && sig != nil && f.sig.Params().Len() == sig.Params().Len())
			}
		case rLit:
			if lit, ok := o.vp.root.node.(*ast.FuncLit); ok {
				if w.frame != nil && w.a.lits[lit] != nil && w.a.lits[lit].unit == w.fn.unit && w.a.scoped.Lits[lit] == nil {
					// A literal of the frame being checked: its uses of the
					// frame are not in its summary.
					res = unionResults(res, w.inlineLit(lit, args, false, pos))
					if out == nil {
						out = w.st
					} else {
						out = join(out, w.st)
					}
					continue
				}
				applyTo(w.a.lits[lit], false)
			}
		case rParam:
			if w.quiet == 0 {
				w.sum.addCall(pu.New(paramCall{idx: o.vp.root.idx, locks: base.locks, rel: base.rel, pos: pos, pre: !base.spawned, conc: w.conc}))
			}
		}
	}
	if out != nil {
		w.st = out
	}
	return res
}

// methodCall calls method name on x, with args (nil entries for
// arguments the analysis need not follow), and returns its first result.
func (w *walker) methodCall(x ast.Expr, name string, args []origins, pos token.Pos) origins {
	T := w.typeOf(x)
	if T == nil {
		return nil
	}
	var pkg *types2.Package
	if w.fn.pkg != nil {
		pkg = w.fn.pkg.Types
	}
	obj, _, _ := types2.LookupFieldOrMethod(T, true, pkg, name)
	fn, ok := obj.(*types2.Func)
	if !ok {
		w.value(x)
		return nil
	}
	var recv origins
	if r := fn.Type().(*types2.Signature).Recv(); r != nil && isInterface(r.Type()) {
		recv = w.value(x)
		var res []origins
		var out *state
		for _, f := range w.a.methodCandidates(name, fn.Type().(*types2.Signature)) {
			r, after := w.apply(f, compat.Append(compat.Clone([]origins{recv}), args...), pos, name, w.st, nil)
			res = unionResults(res, r)
			out = join(out, after)
		}
		if out != nil {
			w.st = out
		}
		if len(res) > 0 {
			return res[0]
		}
		return nil
	}
	if r := fn.Type().(*types2.Signature).Recv(); r != nil && pointerElem(r.Type()) != nil && pointerElem(T) == nil {
		recv = w.addrOf(x)
	} else {
		recv = w.value(x)
	}
	res, after := w.apply(w.a.funcs[fn.Origin()], compat.Append(compat.Clone([]origins{recv}), args...), pos, w.funcLabel(fn), w.st, nil)
	w.st = after
	if len(res) > 0 {
		return res[0]
	}
	return nil
}

// methodCandidates returns the methods a call of method name through an
// interface or type parameter may reach: every method of that name with
// as many parameters and results.
func (a *analyzer) methodCandidates(name string, sig *types2.Signature) []*function {
	var out []*function
	for _, f := range a.byName[name] {
		if f.sig.Params().Len() == sig.Params().Len() && f.sig.Results().Len() == sig.Results().Len() {
			out = compat.Append(out, f)
		}
	}
	return out
}

// valueCandidates returns the functions a call through a func value of
// type sig may reach when the analysis does not know which it holds:
// every function and literal of that type used as a value.
func (a *analyzer) valueCandidates(sig *types2.Signature, _ bool) []*function {
	var out []*function
	for _, f := range a.values {
		if sameShape(f.sig, sig) {
			out = compat.Append(out, f)
		}
	}
	return out
}

// sameShape reports whether a function of type fs, used as a value, may
// have type sig: its parameters (without or with its receiver) and
// results are identical.
func sameShape(fs, sig *types2.Signature) bool {
	plain := types2.NewSignatureType(nil, nil, nil, fs.Params(), fs.Results(), fs.Variadic())
	target := types2.NewSignatureType(nil, nil, nil, sig.Params(), sig.Results(), sig.Variadic())
	if types2.Identical(plain, target) {
		return true
	}
	if r := fs.Recv(); r != nil && fs.Params().Len()+1 == sig.Params().Len() {
		vars := compat.Clone([]*types2.Var{r})
		for i := 0; i < fs.Params().Len(); i++ {
			vars = compat.Append(vars, fs.Params().At(i))
		}
		expr := types2.NewSignatureType(nil, nil, nil, types2.NewTuple(vars...), fs.Results(), fs.Variadic())
		return types2.Identical(expr, target)
	}
	return false
}

// An applyKey is a call of one callee at one place.
type applyKey struct {
	pos token.Pos
	f   *function
}

// An applyMemo is what apply last instantiated at a call: the accesses of
// the callee's summary sum in context ctx.
type applyMemo struct {
	sum   *summary
	ctx   string
	accs  []*access
	typed []*access
}

// hasFresh reports whether an argument may be memory allocated by the
// caller, whose initialisation depends on the walk's state.
func hasFresh(args []origins) bool {
	for _, a := range args {
		for _, o := range a {
			if o.vp.root.kind == rFresh {
				return true
			}
		}
	}
	return false
}
