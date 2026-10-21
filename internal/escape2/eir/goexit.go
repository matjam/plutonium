package eir

import (
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// rt.ExitTask (section 17.3). A call that may end with rt.ExitTask is
// followed, in the generated code, by a check: if the task is exiting, the
// function leaves through its ordinary return path, running its deferred
// calls and dropping what it holds (lower/goexit.go). EIR gives such a call
// the same way out, so the drops and deferred calls on it are calls of the
// function. Which calls may end that way is worked out from the source of
// the whole program by the lowerer's rule, ported here: a function may if
// it calls rt.ExitTask, a function that may, or makes a call through a func
// value or an interface that may reach one (by signature and method name;
// type parameters match anything). A program that never names
// rt.ExitTask has no such calls.

type gxNode struct {
	sig    *types2.Signature
	method string
	edges  []gxEdge
	may    bool
	value  bool
	vsigs  []*types2.Signature
}

type gxEdge struct {
	fn   *types2.Func
	fsig *types2.Signature
	name string
	msig *types2.Signature
}

type goexitInfo struct {
	active   bool
	boundary map[*types2.Func]bool // functions at which an exit stops
	funcs    map[*types2.Func]*gxNode
	members  []*gxNode
}

// analyzeGoexit works out which functions may end with rt.ExitTask
// (lower/goexit.go analyzeGoexit).
func (b *builder) analyzeGoexit() {
	gx := &goexitInfo{funcs: map[*types2.Func]*gxNode{}, boundary: map[*types2.Func]bool{}}
	b.gx = gx
	goexit := b.rtFunc("ExitTask")
	for _, name := range []string{"CatchExitTask", "CatchAllocFree", "DispatchTimerFunc", "taskMain"} {
		if fn := b.rtFunc(name); fn != nil {
			gx.boundary[fn] = true
		}
	}
	if goexit == nil {
		return
	}
	for id, obj := range b.uses {
		if obj == goexit && id.Pos().IsValid() {
			gx.active = true
			break
		}
	}
	if !gx.active {
		return
	}
	var nodes []*gxNode
	newNode := func(sig *types2.Signature) *gxNode {
		n := &gxNode{sig: plainSigOf(sig)}
		nodes = append(nodes, n)
		return n
	}
	newValue := func(sig *types2.Signature) *gxNode {
		n := newNode(sig)
		n.value = true
		n.vsigs = []*types2.Signature{n.sig}
		return n
	}
	called := map[*ast.Ident]bool{}
	markCalled := func(fun ast.Expr) {
		fun = ast.Unparen(fun)
		switch ix := fun.(type) {
		case *ast.IndexExpr:
			fun = ast.Unparen(ix.X)
		case *ast.IndexListExpr:
			fun = ast.Unparen(ix.X)
		}
		switch f := fun.(type) {
		case *ast.Ident:
			called[f] = true
		case *ast.SelectorExpr:
			if sel := b.selections[f]; sel == nil || sel.Kind() == types2.MethodVal {
				called[f.Sel] = true
			}
		}
	}
	var walk func(root ast.Node, cur *gxNode)
	walk = func(root ast.Node, cur *gxNode) {
		ast.Inspect(root, func(x ast.Node) bool {
			switch x := x.(type) {
			case *ast.FuncLit:
				if sig, ok := b.types[x].Type.(*types2.Signature); ok {
					walk(x.Body, newValue(sig))
				}
				return false
			case *ast.TaskStmt:
				markCalled(x.Call.Fun)
				walk(x.Call.Fun, cur)
				for _, a := range x.Call.Args {
					walk(a, cur)
				}
				return false
			case *ast.RangeStmt:
				iter, yield, edges := b.gxRangeIterator(x.X)
				if iter == nil {
					return true
				}
				walk(x.X, cur)
				cur.edges = append(cur.edges, edges...)
				walk(x.Body, newValue(yield))
				return false
			case *ast.CallExpr:
				markCalled(x.Fun)
				if e, ok := b.gxClassify(x); ok {
					cur.edges = append(cur.edges, e)
				}
			}
			return true
		})
	}
	for _, p := range b.prog.Packages {
		for _, f := range p.Files {
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				fn, ok := p.Info.Defs[fd.Name].(*types2.Func)
				if !ok {
					continue
				}
				n := newNode(fn.Type().(*types2.Signature))
				if fn.Type().(*types2.Signature).Recv() != nil {
					n.method = fn.Name()
				}
				gx.funcs[fn] = n
				if gx.boundary[fn] {
					continue
				}
				if fn == goexit {
					n.may = true
					gx.members = append(gx.members, n)
					continue
				}
				walk(fd.Body, n)
			}
		}
	}
	// The body-less hooks call their bound implementations.
	sched := b.scheduler()
	for fn, d := range b.decls {
		if d.decl.Body != nil || fn.Pkg() == nil || fn.Pkg().Path() != "pluto/rt" {
			continue
		}
		if method, ok := schedHooks[fn.Name()]; ok && sched != nil {
			obj, _, _ := types2.LookupFieldOrMethod(sched.Type(), true, sched.Pkg(), method)
			if target, ok := obj.(*types2.Func); ok {
				n := newNode(fn.Type().(*types2.Signature))
				n.edges = []gxEdge{{fn: target.Origin()}}
				gx.funcs[fn] = n
			}
		} else if late, ok := lateHooks[fn.Name()]; ok {
			n := newNode(fn.Type().(*types2.Signature))
			for _, name := range late {
				if target := b.rtFunc(name); target != nil {
					n.edges = append(n.edges, gxEdge{fn: target.Origin()})
				}
			}
			gx.funcs[fn] = n
		}
	}
	for id, obj := range b.uses {
		fn, ok := obj.(*types2.Func)
		if !ok || called[id] {
			continue
		}
		n := gx.funcs[fn.Origin()]
		if n == nil || n.value {
			continue
		}
		n.value = true
		n.vsigs = append(n.vsigs, n.sig, plainSigWithRecv(fn.Origin().Type().(*types2.Signature)))
	}
	for changed := true; changed; {
		changed = false
		for _, n := range nodes {
			if n.may {
				continue
			}
			for _, e := range n.edges {
				if gx.mayEdge(e) {
					n.may = true
					gx.members = append(gx.members, n)
					changed = true
					break
				}
			}
		}
	}
}

func (gx *goexitInfo) mayEdge(e gxEdge) bool {
	switch {
	case e.fn != nil:
		return gx.mayFunc(e.fn)
	case e.fsig != nil:
		return gx.maySig(e.fsig)
	}
	return gx.mayMethod(e.name, e.msig)
}

func (gx *goexitInfo) mayFunc(fn *types2.Func) bool {
	if !gx.active {
		return false
	}
	n := gx.funcs[fn.Origin()]
	return n != nil && n.may
}

func (gx *goexitInfo) maySig(sig *types2.Signature) bool {
	if !gx.active || sig == nil {
		return false
	}
	for _, m := range gx.members {
		for _, vs := range m.vsigs {
			if gxSigMatch(vs, sig) {
				return true
			}
		}
	}
	return false
}

func (gx *goexitInfo) mayMethod(name string, sig *types2.Signature) bool {
	if !gx.active {
		return false
	}
	for _, m := range gx.members {
		if m.method == name && gxSigMatch(m.sig, sig) {
			return true
		}
	}
	return false
}

// gxClassify describes a call for the analysis (lower/goexit.go).
func (b *builder) gxClassify(e *ast.CallExpr) (gxEdge, bool) {
	tv, ok := b.types[e.Fun]
	if !ok || tv.IsType() || tv.IsBuiltin() {
		return gxEdge{}, false
	}
	fun := ast.Unparen(e.Fun)
	switch ix := fun.(type) {
	case *ast.IndexExpr:
		if _, ok := b.types[ix.X].Type.(*types2.Signature); ok {
			fun = ast.Unparen(ix.X)
		}
	case *ast.IndexListExpr:
		fun = ast.Unparen(ix.X)
	}
	if se, ok := fun.(*ast.SelectorExpr); ok {
		if _, ok := b.uses[se.Sel].(*types2.Variant); ok {
			return gxEdge{}, false
		}
		if sel := b.selections[se]; sel != nil {
			if fn, ok := sel.Obj().(*types2.Func); ok && sel.Kind() != types2.FieldVal {
				if isIfaceMethod(fn) {
					return gxEdge{name: fn.Name(), msig: fn.Type().(*types2.Signature)}, true
				}
				return gxEdge{fn: fn.Origin()}, true
			}
		}
	}
	if id := staticFuncIdent(b, fun); id != nil {
		if fn, ok := b.uses[id].(*types2.Func); ok {
			return gxEdge{fn: fn.Origin()}, true
		}
	}
	if sig, ok := tv.Type.Underlying().(*types2.Signature); ok {
		return gxEdge{fsig: sig}, true
	}
	return gxEdge{}, false
}

// gxRangeIterator returns, for a range over a function or a container, the
// iterator's signature, the yield function's, and the calls the loop
// statement makes to get and call the iterator.
func (b *builder) gxRangeIterator(x ast.Expr) (*types2.Signature, *types2.Signature, []gxEdge) {
	tv, ok := b.types[x]
	if !ok || tv.Type == nil {
		return nil, nil, nil
	}
	yieldOf := func(iter *types2.Signature) *types2.Signature {
		if iter.Params().Len() != 1 {
			return nil
		}
		y, _ := iter.Params().At(0).Type().Underlying().(*types2.Signature)
		return y
	}
	if iter, ok := tv.Type.Underlying().(*types2.Signature); ok {
		y := yieldOf(iter)
		if y == nil {
			return nil, nil, nil
		}
		if id := staticFuncIdent(b, x); id != nil {
			if fn, ok := b.uses[id].(*types2.Func); ok {
				return iter, y, []gxEdge{{fn: fn.Origin()}}
			}
		}
		return iter, y, []gxEdge{{fsig: iter}}
	}
	switch tv.Type.Underlying().(type) {
	case *types2.Slice, *types2.Array, *types2.Pointer, *types2.Chan, *types2.Map, *types2.Basic:
		return nil, nil, nil
	}
	obj, _, _ := types2.LookupFieldOrMethod(tv.Type, true, nil, types2.RangeMethodName)
	all, ok := obj.(*types2.Func)
	if !ok {
		return nil, nil, nil
	}
	res := all.Type().(*types2.Signature).Results()
	if res.Len() != 1 {
		return nil, nil, nil
	}
	iter, ok := res.At(0).Type().Underlying().(*types2.Signature)
	if !ok {
		return nil, nil, nil
	}
	y := yieldOf(iter)
	if y == nil {
		return nil, nil, nil
	}
	first := gxEdge{fn: all.Origin()}
	if isIfaceMethod(all) {
		first = gxEdge{name: all.Name(), msig: all.Type().(*types2.Signature)}
	}
	return iter, y, []gxEdge{first, {fsig: iter}}
}

// plainSigOf returns sig without its receiver.
func plainSigOf(sig *types2.Signature) *types2.Signature {
	if sig.Recv() == nil && sig.TypeParams() == nil {
		return sig
	}
	return types2.NewSignatureType(nil, nil, nil, sig.Params(), sig.Results(), sig.Variadic())
}

// plainSigWithRecv returns sig with its receiver as the first parameter:
// the type of a method expression.
func plainSigWithRecv(sig *types2.Signature) *types2.Signature {
	if sig.Recv() == nil {
		return sig
	}
	params := []*types2.Var{sig.Recv()}
	for i := 0; i < sig.Params().Len(); i++ {
		params = append(params, sig.Params().At(i))
	}
	return types2.NewSignatureType(nil, nil, nil, types2.NewTuple(params...), sig.Results(), sig.Variadic())
}

func gxSigMatch(a, b *types2.Signature) bool {
	a, b = plainSigOf(a), plainSigOf(b)
	return a.Variadic() == b.Variadic() && gxTupleMatch(a.Params(), b.Params()) && gxTupleMatch(a.Results(), b.Results())
}

func gxTupleMatch(a, b *types2.Tuple) bool {
	if a.Len() != b.Len() {
		return false
	}
	for i := 0; i < a.Len(); i++ {
		if !gxTypeMatch(a.At(i).Type(), b.At(i).Type(), 0) {
			return false
		}
	}
	return true
}

func gxTypeMatch(a, b types2.Type, depth int) bool {
	a, b = types2.Unalias(a), types2.Unalias(b)
	if _, ok := a.(*types2.TypeParam); ok {
		return true
	}
	if _, ok := b.(*types2.TypeParam); ok {
		return true
	}
	if depth > 8 {
		return true
	}
	switch a := a.(type) {
	case *types2.Pointer:
		b, ok := b.(*types2.Pointer)
		return ok && gxTypeMatch(a.Elem(), b.Elem(), depth+1)
	case *types2.Slice:
		b, ok := b.(*types2.Slice)
		return ok && gxTypeMatch(a.Elem(), b.Elem(), depth+1)
	case *types2.Array:
		b, ok := b.(*types2.Array)
		return ok && a.Len() == b.Len() && gxTypeMatch(a.Elem(), b.Elem(), depth+1)
	case *types2.Chan:
		b, ok := b.(*types2.Chan)
		return ok && a.Dir() == b.Dir() && gxTypeMatch(a.Elem(), b.Elem(), depth+1)
	case *types2.Signature:
		b, ok := b.(*types2.Signature)
		return ok && a.Variadic() == b.Variadic() && gxTupleMatch(a.Params(), b.Params()) && gxTupleMatch(a.Results(), b.Results())
	case *types2.Named:
		b, ok := b.(*types2.Named)
		if !ok || a.Origin().Obj() != b.Origin().Obj() {
			return false
		}
		aa, ba := a.TypeArgs(), b.TypeArgs()
		if aa.Len() != ba.Len() {
			return false
		}
		for i := 0; i < aa.Len(); i++ {
			if !gxTypeMatch(aa.At(i), ba.At(i), depth+1) {
				return false
			}
		}
		return true
	}
	return types2.Identical(a, b)
}

// ----------------------------------------------------------------------------
// Ways out

// callMayExit reports whether the call c may end with rt.ExitTask, so that
// the function leaves after it (lower/goexit.go callMayGoexit).
func (fb *fnb) callMayExit(c *Call) bool {
	gx := fb.b.gx
	if gx == nil || !gx.active || fb.gxCatch() {
		return false
	}
	switch c.Implicit {
	case ImplDefer, ImplDrop, ImplWrapper, ImplTask:
		return false
	}
	switch c.Kind {
	case CallStatic:
		if c.Callee == NoFunc {
			return false
		}
		f := fb.b.p.Funcs[c.Callee]
		if f.Key.Obj != nil {
			return gx.mayFunc(f.Key.Obj)
		}
		return false
	case CallIface:
		if c.Method == nil {
			return false
		}
		return gx.mayMethod(c.Method.Name(), c.Method.Type().(*types2.Signature))
	case CallTypeParam:
		return gx.mayMethod(c.Method.Name(), c.Method.Type().(*types2.Signature))
	case CallFunc:
		return gx.maySig(c.Sig)
	}
	return false
}

// gxCatch reports whether an exit stops at this function: rt.CatchExitTask,
// rt.CatchAllocFree, rt.DispatchTimerFunc, rt.taskMain, the panic handler.
func (fb *fnb) gxCatch() bool {
	root := fb.root
	if root.f.Key.Obj == nil {
		return root.f.Key.Synth != nil
	}
	fn := root.f.Key.Obj
	return fb.b.gx.boundary[fn] || fn == fb.b.panicHandler()
}

// leaveForExit gives the current block a way out for an exit: the
// function returns as it would here, running its deferred calls and
// dropping what it holds, or for a loop body stops the loop.
func (fb *fnb) leaveForExit(pos token.Pos) {
	if fb.cur == nil {
		return
	}
	exit := fb.newBlock()
	cont := fb.newBlock()
	fb.branchBoth(exit, cont)
	fb.seal(exit)
	fb.startBlock(exit)
	if fb.root.f.External && fb.root.f.Key.Obj != nil && hasExternDecl(fb.b, fb.root.f.Key.Obj, "pluto.main") {
		fb.cur = nil // pluto.main: the main task ends (rt.mainGoexit)
	} else {
		fb.emitReturnFor(pos, fb.b.moves.AtStmt[fb.curStmt], true)
	}
	fb.seal(cont)
	fb.startBlock(cont)
}

// hasExternDecl reports whether fn is declared //pu:extern name.
func hasExternDecl(b *builder, fn *types2.Func, name string) bool {
	d := b.decls[fn]
	return d != nil && hasExternName(d.decl, name)
}
