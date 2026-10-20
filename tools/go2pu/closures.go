package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strings"
)

// Closure conversion (self-hosting plan, sections 3.6 and 5.4).
//
// Plutonium builds a closure in the frame of the function that evaluates the
// literal, and rejects storing it where it can outlive that frame: a field
// of an object in an arena, a queue, a package variable (language spec
// section 11.5, E3002 and E3003). Go code stores closures there freely (the
// type checker's deferred actions, the lowering's function jobs), because
// the collector keeps what they capture alive.
//
// go2pu converts such a closure into what the plan describes: a struct that
// holds the captured variables, with a method that is the body of the
// literal, and an interface that stands for the func type:
//
//	type job struct{ build func(s *state) }      type job struct{ build goFn1 }
//	q = append(q, &job{build: func(s *state) {   type goFn1 interface{ Call(*state) }
//		s.emit(n)                         =>     type goClosure_add_1 struct{ n int }
//	}})                                          func (goSelf *goClosure_add_1) Call(s *state) { s.emit(goSelf.n) }
//	j.build(s)                                   q = append(q, &job{build: pu.New(goClosure_add_1{n: n})})
//	                                             j.build.Call(s)
//
// A pointer is direct in an interface, so the action can be queued and run
// after the frame that made it has returned. The struct is allocated in the
// phase arena like every other object Go moves to the heap.
//
// Which closures: the conversion works on "slots", the places a func value
// can be held: variables, fields, parameters and results of func type. Two
// slots are joined when one value can flow from one to the other (an
// assignment, a call's argument and its parameter, a returned value and the
// result, a composite literal's element and its field). A group of joined
// slots is converted when it contains a place that keeps a value after the
// frame is gone (a struct field, a package variable, or a parameter Go's
// escape analysis reports as leaking) and at least one capturing function
// literal flows into it. Every slot of the group changes its type to the
// interface, every literal that flows into it becomes a struct, every call
// through a slot becomes a call of Call, and a value that is not a literal
// (a named function) is wrapped by an adapter. A value read out of a
// converted slot and handed to a place the analysis cannot see (a parameter
// of another package's function) becomes the method value x.Call.
//
// What the analysis cannot convert is reported as a note, as before: the
// slot is part of the package's API, the function whose parameter is
// converted is used as a value, the func type is a named type.
//
// A captured variable is copied into the struct when the closure and its
// creator could not tell the difference: it is never assigned after its
// declaration and its address is not taken. Otherwise it is shared by
// reference, so it is a variable allocated in the arena (heapvars.go), which
// the struct holds a pointer to. Go makes the same choice.

// A slotKey names a slot: a variable, field, or parameter, or an unnamed
// result (the result idx of the function type ft).
type slotKey struct {
	v   *types.Var
	ft  *ast.FuncType
	idx int
}

type litFlow struct {
	lit *ast.FuncLit
	dst slotKey
}

type otherFlow struct {
	e   ast.Expr
	dst slotKey
}

// A closureLit is a function literal that becomes a struct.
type closureLit struct {
	name     string
	sig      *types.Signature
	captured []*types.Var
	field    map[*types.Var]string
	ptr      map[*types.Var]bool // held by pointer: the variable is shared
	declared bool
}

// A closureCtx is the literal whose body is being translated.
type closureCtx struct {
	cl *closureLit
}

type closurePlan struct {
	tc         *translator
	uf         map[slotKey]slotKey
	declOf     map[*types.Func]*ast.FuncDecl
	used       map[ast.Node]bool
	valueFuncs map[*types.Func]bool
	litFlows   []litFlow
	others     []otherFlow
	calls      []*ast.CallExpr
	sinks      []ast.Expr

	// The plan.
	lits     map[*ast.FuncLit]*closureLit
	callConv map[*ast.CallExpr]bool
	sinkConv map[ast.Expr]bool
	otherCnv map[ast.Expr]bool
	typeConv map[*ast.FuncType]*types.Signature
	slotConv map[slotKey]bool // by slot, after planning

	// Generated declarations, shared by the files of the package.
	ifaces   map[string]string
	adapters map[string]string
	busy     map[ast.Node]bool
	ctx      []*closureCtx
}

func (p *closurePlan) find(k slotKey) slotKey {
	for {
		par, ok := p.uf[k]
		if !ok || par == k {
			return k
		}
		if gp, ok := p.uf[par]; ok {
			p.uf[k] = gp
		}
		k = par
	}
}

func (p *closurePlan) union(a, b slotKey) {
	ra, rb := p.find(a), p.find(b)
	if ra == rb {
		return
	}
	// Roots are chosen by position so that the result does not depend on
	// the order the flows were found in.
	if p.slotPos(rb) < p.slotPos(ra) {
		ra, rb = rb, ra
	}
	p.uf[rb] = ra
	if _, ok := p.uf[ra]; !ok {
		p.uf[ra] = ra
	}
}

func (p *closurePlan) slotPos(k slotKey) token.Pos {
	if k.v != nil {
		return k.v.Pos()
	}
	return k.ft.Pos() + token.Pos(k.idx)
}

func (p *closurePlan) touch(k slotKey) {
	if _, ok := p.uf[k]; !ok {
		p.uf[k] = k
	}
}

func isSig(T types.Type) bool {
	if T == nil {
		return false
	}
	_, ok := T.Underlying().(*types.Signature)
	return ok
}

func (p *closurePlan) isFunc(e ast.Expr) bool {
	tv, ok := p.tc.info.Types[e]
	return ok && tv.Type != nil && !tv.IsType() && isSig(tv.Type)
}

func (p *closurePlan) isNil(e ast.Expr) bool {
	tv, ok := p.tc.info.Types[e]
	return ok && tv.IsNil()
}

// resultKey returns the slot of result i of ft.
func (p *closurePlan) resultKey(ft *ast.FuncType, i int) slotKey {
	if ft.Results != nil {
		n := 0
		for _, f := range ft.Results.List {
			if len(f.Names) == 0 {
				if n == i {
					return slotKey{ft: ft, idx: i}
				}
				n++
				continue
			}
			for _, id := range f.Names {
				if n == i {
					if v, ok := p.tc.info.Defs[id].(*types.Var); ok {
						return slotKey{v: v}
					}
				}
				n++
			}
		}
	}
	return slotKey{ft: ft, idx: i}
}

// slotOf returns the slot an expression of func type reads.
func (p *closurePlan) slotOf(e ast.Expr) (slotKey, bool) {
	tc := p.tc
	switch e := ast.Unparen(e).(type) {
	case *ast.Ident:
		if v, ok := tc.info.Uses[e].(*types.Var); ok && !v.IsField() && v.Pkg() == tc.pkg {
			return slotKey{v: v}, true
		}
		if v, ok := tc.info.Defs[e].(*types.Var); ok && !v.IsField() {
			return slotKey{v: v}, true
		}
	case *ast.SelectorExpr:
		if sel := tc.info.Selections[e]; sel != nil {
			if sel.Kind() == types.FieldVal {
				if v, ok := sel.Obj().(*types.Var); ok && v.Pkg() == tc.pkg {
					return slotKey{v: v.Origin()}, true
				}
			}
			return slotKey{}, false
		}
		if v, ok := tc.info.Uses[e.Sel].(*types.Var); ok && v.Pkg() == tc.pkg {
			return slotKey{v: v}, true
		}
	case *ast.CallExpr:
		if fn, ok := p.staticCallee(e); ok {
			if d := p.declOf[fn]; d != nil && fn.Signature().Results().Len() == 1 {
				return p.resultKey(d.Type, 0), true
			}
		}
	}
	return slotKey{}, false
}

// staticCallee returns the function a call calls, if it is a function or
// method declared in this package that is not an interface method.
func (p *closurePlan) staticCallee(call *ast.CallExpr) (*types.Func, bool) {
	tc := p.tc
	fun := ast.Unparen(call.Fun)
	if ix, ok := fun.(*ast.IndexExpr); ok {
		fun = ast.Unparen(ix.X)
	}
	var fn *types.Func
	switch f := fun.(type) {
	case *ast.Ident:
		fn, _ = tc.info.Uses[f].(*types.Func)
	case *ast.SelectorExpr:
		if sel := tc.info.Selections[f]; sel != nil {
			if sel.Kind() == types.MethodVal {
				fn, _ = sel.Obj().(*types.Func)
			}
		} else {
			fn, _ = tc.info.Uses[f.Sel].(*types.Func)
		}
	}
	if fn == nil {
		return nil, false
	}
	fn = fn.Origin()
	if fn.Pkg() != tc.pkg {
		return nil, false
	}
	if sig := fn.Signature(); sig.Recv() != nil && types.IsInterface(sig.Recv().Type()) {
		return nil, false
	}
	return fn, true
}

func (tc *translator) planClosures(files []*ast.File, res *Result) {
	p := &closurePlan{
		tc:         tc,
		uf:         map[slotKey]slotKey{},
		declOf:     map[*types.Func]*ast.FuncDecl{},
		used:       map[ast.Node]bool{},
		valueFuncs: map[*types.Func]bool{},
		lits:       map[*ast.FuncLit]*closureLit{},
		callConv:   map[*ast.CallExpr]bool{},
		sinkConv:   map[ast.Expr]bool{},
		otherCnv:   map[ast.Expr]bool{},
		typeConv:   map[*ast.FuncType]*types.Signature{},
		slotConv:   map[slotKey]bool{},
		ifaces:     map[string]string{},
		adapters:   map[string]string{},
		busy:       map[ast.Node]bool{},
	}
	tc.cl = p
	for _, f := range files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok {
				if fn, ok := tc.info.Defs[fd.Name].(*types.Func); ok {
					p.declOf[fn] = fd
				}
			}
		}
	}
	for _, f := range files {
		p.findValueFuncs(f)
		p.collect(f)
	}
	p.sweepSinks(files)
	p.decide(res)
}

// findValueFuncs records the functions of the package used other than by
// calling them.
func (p *closurePlan) findValueFuncs(f *ast.File) {
	tc := p.tc
	ast.Inspect(f, func(n ast.Node) bool {
		var obj types.Object
		var at ast.Node
		switch n := n.(type) {
		case *ast.Ident:
			if sel, ok := tc.parent[n].(*ast.SelectorExpr); ok && sel.Sel == n {
				return true // handled with the selector
			}
			obj, at = tc.info.Uses[n], n
		case *ast.SelectorExpr:
			if s := tc.info.Selections[n]; s != nil {
				obj, at = s.Obj(), n
			} else {
				obj, at = tc.info.Uses[n.Sel], n
			}
		default:
			return true
		}
		fn, ok := obj.(*types.Func)
		if !ok || fn.Pkg() != tc.pkg {
			return true
		}
		cur := at
		for {
			if pe, ok := tc.parent[cur].(*ast.ParenExpr); ok {
				cur = pe
				continue
			}
			if ix, ok := tc.parent[cur].(*ast.IndexExpr); ok && ix.X == cur {
				cur = ix
				continue
			}
			break
		}
		if call, ok := tc.parent[cur].(*ast.CallExpr); !ok || call.Fun != cur {
			p.valueFuncs[fn.Origin()] = true
		}
		return true
	})
}

func (p *closurePlan) markUsed(e ast.Expr) {
	for {
		p.used[e] = true
		pe, ok := e.(*ast.ParenExpr)
		if !ok {
			return
		}
		e = pe.X
	}
}

// flow records that the value of src is stored in the slot dst (ok false:
// in a place the analysis does not track).
func (p *closurePlan) flow(dst slotKey, ok bool, src ast.Expr) {
	if !p.isFunc(src) {
		return
	}
	p.markUsed(src)
	e := ast.Unparen(src)
	if p.isNil(e) {
		return
	}
	if !ok {
		if _, isLit := e.(*ast.FuncLit); isLit {
			return
		}
		if _, has := p.slotOf(e); has {
			p.sinks = append(p.sinks, e)
		}
		return
	}
	p.touch(dst)
	if lit, isLit := e.(*ast.FuncLit); isLit {
		p.litFlows = append(p.litFlows, litFlow{lit, dst})
		return
	}
	if s, has := p.slotOf(e); has {
		p.touch(s)
		p.union(dst, s)
		return
	}
	p.others = append(p.others, otherFlow{e, dst})
}

// lhsSlot returns the slot an assignment target denotes.
func (p *closurePlan) lhsSlot(l ast.Expr) (slotKey, bool) {
	p.markUsed(l)
	if id, ok := ast.Unparen(l).(*ast.Ident); ok && id.Name == "_" {
		return slotKey{}, false
	}
	if !p.isFunc(l) {
		return slotKey{}, false
	}
	return p.slotOf(l)
}

func (p *closurePlan) collect(f *ast.File) {
	tc := p.tc
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			if n.Tok != token.ASSIGN && n.Tok != token.DEFINE {
				return true
			}
			if len(n.Lhs) == len(n.Rhs) {
				for i, l := range n.Lhs {
					k, ok := p.lhsSlot(l)
					p.flow(k, ok, n.Rhs[i])
				}
			} else if len(n.Rhs) == 1 {
				if call, ok := ast.Unparen(n.Rhs[0]).(*ast.CallExpr); ok {
					p.tupleFlow(n.Lhs, call)
				}
			}
		case *ast.ValueSpec:
			for i, id := range n.Names {
				v, ok := tc.info.Defs[id].(*types.Var)
				if !ok || !isSig(v.Type()) {
					continue
				}
				p.touch(slotKey{v: v})
				if len(n.Values) == len(n.Names) {
					p.flow(slotKey{v: v}, true, n.Values[i])
				}
			}
			if len(n.Values) == 1 && len(n.Names) > 1 {
				if call, ok := ast.Unparen(n.Values[0]).(*ast.CallExpr); ok {
					lhs := make([]ast.Expr, len(n.Names))
					for i, id := range n.Names {
						lhs[i] = id
					}
					p.tupleFlow(lhs, call)
				}
			}
		case *ast.CompositeLit:
			p.compositeLit(n)
		case *ast.CallExpr:
			p.call(n)
		case *ast.ReturnStmt:
			p.ret(n)
		case *ast.BinaryExpr:
			if n.Op == token.EQL || n.Op == token.NEQ {
				if p.isNil(n.Y) {
					p.markUsed(n.X)
				}
				if p.isNil(n.X) {
					p.markUsed(n.Y)
				}
			}
		case *ast.ExprStmt:
			p.markUsed(n.X)
		case *ast.KeyValueExpr:
			p.markUsed(n.Key)
		}
		return true
	})
}

// tupleFlow joins the results of call with the targets lhs of a
// multi-value assignment.
func (p *closurePlan) tupleFlow(lhs []ast.Expr, call *ast.CallExpr) {
	fn, ok := p.staticCallee(call)
	if !ok {
		return
	}
	d := p.declOf[fn]
	if d == nil || fn.Signature().Results().Len() != len(lhs) {
		return
	}
	for i, l := range lhs {
		if k, ok := p.lhsSlot(l); ok {
			rk := p.resultKey(d.Type, i)
			p.touch(rk)
			p.touch(k)
			p.union(k, rk)
		}
	}
}

func (p *closurePlan) compositeLit(n *ast.CompositeLit) {
	tc := p.tc
	T := tc.typeOf(n)
	if T == nil {
		return
	}
	if pt, ok := T.Underlying().(*types.Pointer); ok {
		T = pt.Elem()
	}
	st, ok := T.Underlying().(*types.Struct)
	if !ok {
		return
	}
	for i, e := range n.Elts {
		var field *types.Var
		val := e
		if kv, ok := e.(*ast.KeyValueExpr); ok {
			id, _ := kv.Key.(*ast.Ident)
			if id == nil {
				continue
			}
			field, _ = tc.info.Uses[id].(*types.Var)
			val = kv.Value
		} else if i < st.NumFields() {
			field = st.Field(i)
		}
		if field == nil {
			continue
		}
		p.flow(slotKey{v: field.Origin()}, true, val)
	}
}

func (p *closurePlan) call(n *ast.CallExpr) {
	tc := p.tc
	if tv, ok := tc.info.Types[n.Fun]; ok && tv.IsType() {
		return // a conversion
	}
	if id, ok := ast.Unparen(n.Fun).(*ast.Ident); ok {
		if _, ok := tc.info.Uses[id].(*types.Builtin); ok {
			return
		}
	}
	if p.isFunc(n.Fun) {
		if _, has := p.slotOf(n.Fun); has {
			p.calls = append(p.calls, n)
			p.markUsed(n.Fun)
		}
	}
	fn, static := p.staticCallee(n)
	var params *types.Tuple
	if static {
		params = fn.Signature().Params()
	}
	for i, a := range n.Args {
		if static && params != nil && (!fn.Signature().Variadic() || i < params.Len()-1) && i < params.Len() {
			pv := params.At(i)
			if isSig(pv.Type()) {
				p.flow(slotKey{v: pv}, true, a)
				continue
			}
		}
		p.flow(slotKey{}, false, a)
	}
}

func (p *closurePlan) ret(n *ast.ReturnStmt) {
	tc := p.tc
	var ft *ast.FuncType
	switch f := tc.nearestFunc(n).(type) {
	case *ast.FuncDecl:
		ft = f.Type
	case *ast.FuncLit:
		ft = f.Type
	}
	if ft == nil || ft.Results == nil {
		return
	}
	count := 0
	for _, f := range ft.Results.List {
		if len(f.Names) == 0 {
			count++
		} else {
			count += len(f.Names)
		}
	}
	if len(n.Results) == count {
		for i, r := range n.Results {
			p.flow(p.resultKey(ft, i), true, r)
		}
		return
	}
	if len(n.Results) == 1 && count > 1 {
		if call, ok := ast.Unparen(n.Results[0]).(*ast.CallExpr); ok {
			if fn, ok := p.staticCallee(call); ok {
				if d := p.declOf[fn]; d != nil && fn.Signature().Results().Len() == count {
					for i := 0; i < count; i++ {
						a, b := p.resultKey(ft, i), p.resultKey(d.Type, i)
						p.touch(a)
						p.touch(b)
						p.union(a, b)
					}
				}
			}
		}
	}
}

// sweepSinks finds the reads of a slot that no flow accounts for: the value
// goes to a place the analysis cannot see.
func (p *closurePlan) sweepSinks(files []*ast.File) {
	tc := p.tc
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			e, ok := n.(ast.Expr)
			if !ok || p.used[e] {
				return true
			}
			switch e := e.(type) {
			case *ast.Ident:
				if sel, ok := tc.parent[e].(*ast.SelectorExpr); ok && sel.Sel == e {
					return true
				}
				if _, ok := tc.info.Uses[e]; !ok {
					return true
				}
			case *ast.SelectorExpr, *ast.CallExpr:
			default:
				return true
			}
			if !p.isFunc(e) {
				return true
			}
			if _, has := p.slotOf(e); has {
				p.sinks = append(p.sinks, e)
			}
			return true
		})
	}
}

// leakingParam reports whether Go's escape analysis says the parameter v
// outlives its call.
func (p *closurePlan) leakingParam(v *types.Var) bool {
	tc := p.tc
	id := tc.defOf[v]
	if id == nil {
		return false
	}
	f, ok := tc.parent[id].(*ast.Field)
	if !ok {
		return false
	}
	fl, ok := tc.parent[f].(*ast.FieldList)
	if !ok {
		return false
	}
	ft, ok := tc.parent[fl].(*ast.FuncType)
	if !ok || ft.Params != fl {
		return false
	}
	for _, m := range tc.esc.at(tc.fset, id.Pos()) {
		if m == "leaking param: "+id.Name {
			return true
		}
	}
	return false
}

// keeps reports whether a slot is a place that holds a func value after the
// frame that stored it is gone.
func (p *closurePlan) keeps(k slotKey) bool {
	v := k.v
	if v == nil {
		return false
	}
	if v.IsField() {
		return true
	}
	if v.Parent() == p.tc.pkg.Scope() {
		return true
	}
	return p.leakingParam(v)
}

// apiProblem returns why a slot cannot change its type, or "".
func (p *closurePlan) apiProblem(k slotKey) string {
	tc := p.tc
	v := k.v
	if v == nil {
		return ""
	}
	if _, named := v.Type().(*types.Named); named {
		return "its type is a named func type"
	}
	if _, alias := v.Type().(*types.Alias); alias {
		return "its type is a named func type"
	}
	if v.IsField() && v.Exported() {
		return "it is an exported field"
	}
	if v.Parent() == tc.pkg.Scope() && v.Exported() {
		return "it is an exported variable"
	}
	id := tc.defOf[v]
	if id == nil {
		return ""
	}
	// A parameter or result of a function.
	if f, ok := tc.parent[id].(*ast.Field); ok {
		if fl, ok := tc.parent[f].(*ast.FieldList); ok {
			if ft, ok := tc.parent[fl].(*ast.FuncType); ok {
				if fd, ok := tc.parent[ft].(*ast.FuncDecl); ok {
					if fn, ok := tc.info.Defs[fd.Name].(*types.Func); ok {
						if p.valueFuncs[fn.Origin()] {
							return fmt.Sprintf("%s is used as a value", fd.Name.Name)
						}
						if fd.Name.IsExported() && (fd.Recv == nil || exportedRecv(fd.Recv)) {
							return fmt.Sprintf("%s is exported", fd.Name.Name)
						}
					}
				}
			}
		}
	}
	return ""
}

func exportedRecv(recv *ast.FieldList) bool {
	if recv == nil || len(recv.List) == 0 {
		return true
	}
	t := recv.List[0].Type
	if s, ok := t.(*ast.StarExpr); ok {
		t = s.X
	}
	if ix, ok := t.(*ast.IndexExpr); ok {
		t = ix.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.IsExported()
	}
	return true
}

func (p *closurePlan) slotSig(k slotKey) *types.Signature {
	tc := p.tc
	if k.v != nil {
		sig, _ := k.v.Type().Underlying().(*types.Signature)
		return sig
	}
	if k.ft != nil {
		if tv, ok := tc.info.Types[k.ft]; ok {
			_ = tv
		}
		// The result's type: find the function that owns ft.
		var sigT types.Type
		switch f := tc.parent[k.ft].(type) {
		case *ast.FuncDecl:
			if fn, ok := tc.info.Defs[f.Name].(*types.Func); ok {
				sigT = fn.Type()
			}
		case *ast.FuncLit:
			sigT = tc.info.TypeOf(f)
		}
		if sig, ok := sigT.(*types.Signature); ok && k.idx < sig.Results().Len() {
			s, _ := sig.Results().At(k.idx).Type().Underlying().(*types.Signature)
			return s
		}
	}
	return nil
}

// litOK reports whether a literal can become a struct: the enclosing
// function is not generic and the types it mentions are declared at package
// level.
func (p *closurePlan) litOK(lit *ast.FuncLit) string {
	tc := p.tc
	switch f := tc.nearestFuncOuter(lit).(type) {
	case *ast.FuncDecl:
		if f.Type.TypeParams != nil {
			return "its function is generic"
		}
		if f.Recv != nil && len(f.Recv.List) > 0 {
			if _, generic := recvTypeArgs(f.Recv.List[0].Type); generic {
				return "its method belongs to a generic type"
			}
		}
	case nil:
	}
	for _, v := range tc.capturedVars(lit) {
		if !tc.pkgLevelType(v.Type()) {
			return fmt.Sprintf("it captures %s, whose type %s is declared inside a function", v.Name(), v.Type())
		}
	}
	if sig, ok := tc.info.TypeOf(lit).(*types.Signature); ok && !tc.pkgLevelType(sig) {
		return "its signature mentions a type declared inside a function"
	}
	return ""
}

func recvTypeArgs(t ast.Expr) (ast.Expr, bool) {
	if s, ok := t.(*ast.StarExpr); ok {
		t = s.X
	}
	switch t := t.(type) {
	case *ast.IndexExpr:
		return t.X, true
	case *ast.IndexListExpr:
		return t.X, true
	}
	return t, false
}

// nearestFuncOuter returns the outermost function declaration around n.
func (tc *translator) nearestFuncOuter(n ast.Node) ast.Node {
	var out ast.Node
	for p := tc.parent[n]; p != nil; p = tc.parent[p] {
		if fd, ok := p.(*ast.FuncDecl); ok {
			out = fd
		}
	}
	return out
}

// pkgLevelType reports whether every named type in T is declared at package
// level (or is a universe or imported type), and T mentions no type
// parameter.
func (tc *translator) pkgLevelType(T types.Type) bool {
	seen := map[types.Type]bool{}
	var ok func(T types.Type) bool
	ok = func(T types.Type) bool {
		if seen[T] {
			return true
		}
		seen[T] = true
		switch T := T.(type) {
		case *types.Basic:
			return true
		case *types.Named:
			o := T.Obj()
			if o.Pkg() != nil && o.Parent() != o.Pkg().Scope() && o.Parent() != nil {
				return false
			}
			for i := 0; i < T.TypeArgs().Len(); i++ {
				if !ok(T.TypeArgs().At(i)) {
					return false
				}
			}
			return true
		case *types.Alias:
			return ok(types.Unalias(T))
		case *types.TypeParam:
			return false
		case *types.Pointer:
			return ok(T.Elem())
		case *types.Slice:
			return ok(T.Elem())
		case *types.Array:
			return ok(T.Elem())
		case *types.Chan:
			return ok(T.Elem())
		case *types.Map:
			return ok(T.Key()) && ok(T.Elem())
		case *types.Signature:
			for i := 0; i < T.Params().Len(); i++ {
				if !ok(T.Params().At(i).Type()) {
					return false
				}
			}
			for i := 0; i < T.Results().Len(); i++ {
				if !ok(T.Results().At(i).Type()) {
					return false
				}
			}
			return true
		case *types.Struct:
			for i := 0; i < T.NumFields(); i++ {
				if !ok(T.Field(i).Type()) {
					return false
				}
			}
			return true
		case *types.Interface:
			for i := 0; i < T.NumMethods(); i++ {
				if !ok(T.Method(i).Type()) {
					return false
				}
			}
			return true
		}
		return true
	}
	return ok(T)
}

// capturedVars lists the variables of enclosing functions that lit refers
// to, in the order of their first use.
func (tc *translator) capturedVars(lit *ast.FuncLit) []*types.Var {
	var out []*types.Var
	seen := map[*types.Var]bool{}
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		v, ok := tc.info.Uses[id].(*types.Var)
		if !ok || v.IsField() || seen[v] || v.Pkg() == nil || v.Parent() == v.Pkg().Scope() {
			return true
		}
		if v.Pos() < lit.Pos() || v.Pos() > lit.End() {
			seen[v] = true
			out = append(out, v)
		}
		return true
	})
	return out
}

// shared reports whether a closure must hold v by reference: its value
// could change, or something could point to it.
func (tc *translator) shared(v *types.Var) bool {
	if holdsOwner(v.Type(), map[types.Type]bool{}) {
		return true
	}
	if tc.isResult(v) {
		return true
	}
	for _, id := range tc.usesOf[v] {
		if tc.writtenUse(id) {
			return true
		}
	}
	return false
}

// writtenUse reports whether the use id assigns the variable or a part of
// it (a field of a struct value, an element of an array), or lets a
// pointer to it escape.
func (tc *translator) writtenUse(id *ast.Ident) bool {
	if !tc.plainUse(id) {
		return true
	}
	var cur ast.Node = id
	for {
		switch p := tc.parent[cur].(type) {
		case *ast.ParenExpr:
			cur = p
			continue
		case *ast.SelectorExpr:
			if sel := tc.info.Selections[p]; p.X == cur && sel != nil && sel.Kind() == types.FieldVal && !sel.Indirect() {
				cur = p
				continue
			}
		case *ast.IndexExpr:
			if _, isArray := tc.typeOf(p.X).Underlying().(*types.Array); isArray && p.X == cur {
				cur = p
				continue
			}
		}
		break
	}
	if cur == ast.Node(id) {
		return tc.assigns(id)
	}
	switch p := tc.parent[cur].(type) {
	case *ast.AssignStmt:
		for _, l := range p.Lhs {
			if l == cur {
				return true
			}
		}
	case *ast.IncDecStmt:
		return true
	case *ast.RangeStmt:
		return p.Tok == token.ASSIGN && (p.Key == cur || p.Value == cur)
	}
	return false
}

func (p *closurePlan) decide(res *Result) {
	tc := p.tc
	// Group the slots.
	groups := map[slotKey][]slotKey{}
	for k := range p.uf {
		r := p.find(k)
		groups[r] = append(groups[r], k)
	}
	litsOf := map[slotKey][]*ast.FuncLit{}
	for _, lf := range p.litFlows {
		r := p.find(lf.dst)
		litsOf[r] = append(litsOf[r], lf.lit)
	}
	var roots []slotKey
	for r := range groups {
		roots = append(roots, r)
	}
	sort.Slice(roots, func(i, j int) bool { return p.slotPos(roots[i]) < p.slotPos(roots[j]) })
	converted := map[slotKey]bool{}
	for _, r := range roots {
		members := groups[r]
		keeps := false
		for _, m := range members {
			if p.keeps(m) {
				keeps = true
			}
		}
		var capturing []*ast.FuncLit
		for _, lit := range litsOf[r] {
			if len(tc.capturedVars(lit)) > 0 {
				capturing = append(capturing, lit)
			}
		}
		if !keeps || len(capturing) == 0 {
			continue
		}
		sort.Slice(capturing, func(i, j int) bool { return capturing[i].Pos() < capturing[j].Pos() })
		if why, at := p.problem(members, litsOf[r]); why != "" {
			if at == nil {
				at = capturing[0]
			}
			tc.noteRes(res, at, "closure conversion refused for the func values stored here (%s); the closures stay and Plutonium reports them: hand-fix", why)
			continue
		}
		converted[r] = true
	}
	for _, r := range roots {
		if !converted[r] {
			continue
		}
		for _, m := range groups[r] {
			p.slotConv[m] = true
		}
	}

	// Choose the names of the literals, in source order.
	var all []*ast.FuncLit
	for _, r := range roots {
		if converted[r] {
			all = append(all, litsOf[r]...)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Pos() < all[j].Pos() })
	count := map[string]int{}
	for _, lit := range all {
		if p.lits[lit] != nil {
			continue
		}
		fn := "init"
		if fd, ok := tc.nearestFuncOuter(lit).(*ast.FuncDecl); ok {
			fn = fd.Name.Name
		}
		count[fn]++
		sig, _ := tc.info.TypeOf(lit).(*types.Signature)
		cl := &closureLit{
			name:     fmt.Sprintf("goClosure_%s_%d", fn, count[fn]),
			sig:      sig,
			captured: tc.capturedVars(lit),
			field:    map[*types.Var]string{},
			ptr:      map[*types.Var]bool{},
		}
		used := map[string]bool{"Call": true}
		for _, v := range cl.captured {
			name := goName(v.Name())
			for used[name] {
				name += "_"
			}
			used[name] = true
			cl.field[v] = name
			if tc.shared(v) {
				cl.ptr[v] = true
				tc.heap[v] = true
			}
		}
		p.lits[lit] = cl
	}

	for _, lf := range p.litFlows {
		_ = lf
	}
	for _, c := range p.calls {
		if k, ok := p.slotOf(c.Fun); ok && p.slotConv[k] {
			p.callConv[c] = true
		}
	}
	for _, e := range p.sinks {
		if k, ok := p.slotOf(e); ok && p.slotConv[k] {
			p.sinkConv[e] = true
		}
	}
	for _, o := range p.others {
		if p.slotConv[o.dst] {
			p.otherCnv[o.e] = true
		}
	}
	// The declared types of converted slots.
	var keys []slotKey
	for k := range p.slotConv {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return p.slotPos(keys[i]) < p.slotPos(keys[j]) })
	for _, k := range keys {
		sig := p.slotSig(k)
		if sig == nil {
			continue
		}
		if te := p.declType(k); te != nil {
			if ft, ok := ast.Unparen(te).(*ast.FuncType); ok {
				p.typeConv[ft] = sig
			}
		}
	}
}

// problem returns why the slots of a group cannot be converted, or "".
func (p *closurePlan) problem(members []slotKey, lits []*ast.FuncLit) (string, ast.Node) {
	for _, m := range members {
		var at ast.Node
		if id := p.tc.defOf[m.v]; m.v != nil && id != nil {
			at = id
		} else if m.v == nil {
			at = m.ft
		} else if len(lits) > 0 {
			at = lits[0]
		}
		if why := p.apiProblem(m); why != "" {
			return why, at
		}
		if m.v != nil {
			if te := p.declType(m); te != nil {
				if _, ok := ast.Unparen(te).(*ast.FuncType); !ok {
					return "its declared type is not a func type literal", at
				}
			}
			if f, ok := p.tc.parent[p.tc.defOf[m.v]].(*ast.Field); ok && len(f.Names) > 1 {
				return "several names are declared with one func type", at
			}
		}
	}
	for _, lit := range lits {
		if len(p.tc.capturedVars(lit)) == 0 {
			continue
		}
		if why := p.litOK(lit); why != "" {
			return why, lit
		}
	}
	return "", nil
}

// declType returns the type expression that declares the slot, if any.
func (p *closurePlan) declType(k slotKey) ast.Expr {
	tc := p.tc
	if k.v != nil {
		id := tc.defOf[k.v]
		if id == nil {
			return nil
		}
		switch d := tc.parent[id].(type) {
		case *ast.Field:
			return d.Type
		case *ast.ValueSpec:
			return d.Type
		}
		return nil
	}
	if k.ft != nil && k.ft.Results != nil {
		n := 0
		for _, f := range k.ft.Results.List {
			if len(f.Names) == 0 {
				if n == k.idx {
					return f.Type
				}
				n++
			} else {
				n += len(f.Names)
			}
		}
	}
	return nil
}

func (tc *translator) noteRes(res *Result, n ast.Node, format string, args ...any) {
	pos := tc.fset.Position(n.Pos())
	msg := fmt.Sprintf("%s:%d: %s", filepath.Base(pos.Filename), pos.Line, fmt.Sprintf(format, args...))
	for _, m := range res.Notes {
		if m == msg {
			return
		}
	}
	res.Notes = append(res.Notes, msg)
}

// ----------------------------------------------------------------------------
// Rewriting

// sigKey names the func type of sig without its parameter names.
func sigKey(sig *types.Signature) string {
	q := func(p *types.Package) string { return p.Path() }
	var b strings.Builder
	b.WriteString("func(")
	for i := 0; i < sig.Params().Len(); i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		pt := sig.Params().At(i).Type()
		if sig.Variadic() && i == sig.Params().Len()-1 {
			b.WriteString("...")
			pt = pt.(*types.Slice).Elem()
		}
		b.WriteString(types.TypeString(pt, q))
	}
	b.WriteString(") (")
	for i := 0; i < sig.Results().Len(); i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(types.TypeString(sig.Results().At(i).Type(), q))
	}
	b.WriteString(")")
	return b.String()
}

// ifaceName returns the interface that stands for the func type sig,
// declaring it in the file being translated the first time.
func (t *fileTr) ifaceName(sig *types.Signature) string {
	p := t.cl
	key := sigKey(sig)
	if n, ok := p.ifaces[key]; ok {
		return n
	}
	n := fmt.Sprintf("goFn%d", len(p.ifaces)+1)
	p.ifaces[key] = n
	t.extra = append(t.extra, fmt.Sprintf("// %s stands for %s, which a closure that outlives its frame cannot be\n// (go2pu closure conversion): the closure is a struct with this method.\ntype %s interface {\n\tCall%s\n}\n",
		n, t.typeText(sig), n, strings.TrimPrefix(t.typeText(sig), "func")))
	return n
}

// adapterName returns the struct that wraps a func value of type sig as an
// interface value of ifaceName(sig).
func (t *fileTr) adapterName(sig *types.Signature) string {
	p := t.cl
	key := sigKey(sig)
	if n, ok := p.adapters[key]; ok {
		return n
	}
	iface := t.ifaceName(sig)
	n := fmt.Sprintf("goFnFunc%d", len(p.adapters)+1)
	p.adapters[key] = n
	var params, args []string
	for i := 0; i < sig.Params().Len(); i++ {
		pt := sig.Params().At(i).Type()
		name := fmt.Sprintf("p%d", i)
		if sig.Variadic() && i == sig.Params().Len()-1 {
			params = append(params, name+" ..."+t.typeText(pt.(*types.Slice).Elem()))
			args = append(args, name+"...")
			continue
		}
		params = append(params, name+" "+t.typeText(pt))
		args = append(args, name)
	}
	results := ""
	switch sig.Results().Len() {
	case 0:
	case 1:
		results = " " + t.typeText(sig.Results().At(0).Type())
	default:
		var rs []string
		for i := 0; i < sig.Results().Len(); i++ {
			rs = append(rs, t.typeText(sig.Results().At(i).Type()))
		}
		results = " (" + strings.Join(rs, ", ") + ")"
	}
	ret := ""
	if sig.Results().Len() > 0 {
		ret = "return "
	}
	t.extra = append(t.extra, fmt.Sprintf("// %s wraps a func value as a %s.\ntype %s struct{ f %s }\n\nfunc (a *%s) Call(%s)%s {\n\t%sa.f(%s)\n}\n",
		n, iface, n, t.typeText(sig), n, strings.Join(params, ", "), results, ret, strings.Join(args, ", ")))
	return n
}

// closureRewrite translates the nodes closure conversion changes.
func (t *fileTr) closureRewrite(n ast.Node) (string, bool) {
	p := t.cl
	if p == nil || p.busy[n] {
		return "", false
	}
	switch n := n.(type) {
	case *ast.FuncType:
		if sig, ok := p.typeConv[n]; ok {
			return t.ifaceName(sig), true
		}
	case *ast.FuncLit:
		if cl := p.lits[n]; cl != nil {
			return t.closureLit(n, cl), true
		}
	case *ast.CallExpr:
		if p.callConv[n] {
			s := t.closureCall(n)
			if p.sinkConv[n] {
				s += ".Call"
			}
			return s, true
		}
	}
	if e, ok := n.(ast.Expr); ok {
		if p.sinkConv[e] {
			p.busy[n] = true
			s := t.render(n)
			delete(p.busy, n)
			return s + ".Call", true
		}
		if p.otherCnv[e] {
			p.busy[n] = true
			s := t.render(n)
			delete(p.busy, n)
			sig, _ := t.typeOf(e).Underlying().(*types.Signature)
			if sig == nil {
				return "", false
			}
			return t.puNew(t.adapterName(sig) + "{f: " + s + "}"), true
		}
	}
	return "", false
}

func (t *fileTr) closureCall(n *ast.CallExpr) string {
	dots := ""
	if n.Ellipsis.IsValid() {
		dots = "..."
	}
	return fmt.Sprintf("%s.Call(%s%s)", t.render(n.Fun), t.list(n.Args), dots)
}

// captureExpr is the expression for the held form of v (its value or its
// pointer) where a closure over v is created.
func (t *fileTr) captureExpr(v *types.Var, cl *closureLit) string {
	p := t.cl
	if len(p.ctx) > 0 {
		if inner := p.ctx[len(p.ctx)-1].cl; inner.field[v] != "" {
			return "goSelf." + inner.field[v]
		}
	}
	return goName(v.Name())
}

// closureLit translates a function literal that becomes a struct.
func (t *fileTr) closureLit(lit *ast.FuncLit, cl *closureLit) string {
	t.declareClosure(lit, cl)
	var fields []string
	for _, v := range cl.captured {
		fields = append(fields, cl.field[v]+": "+t.captureExpr(v, cl))
	}
	return t.puNew(cl.name + "{" + strings.Join(fields, ", ") + "}")
}

// declareClosure declares the struct and method of the closure, once.
func (t *fileTr) declareClosure(lit *ast.FuncLit, cl *closureLit) {
	if cl.declared {
		return
	}
	cl.declared = true
	p := t.cl
	pos := t.fset.Position(lit.Pos())
	var b strings.Builder
	fmt.Fprintf(&b, "// %s is the function literal at %s:%d, converted to a struct\n// that holds the variables it captures (go2pu closure conversion).\n", cl.name, filepath.Base(pos.Filename), pos.Line)
	fmt.Fprintf(&b, "type %s struct {\n", cl.name)
	for _, v := range cl.captured {
		typ := t.captureTypeText(v)
		if cl.ptr[v] {
			typ = "*" + typ
		}
		fmt.Fprintf(&b, "\t%s %s\n", cl.field[v], typ)
	}
	b.WriteString("}\n")

	// The captured variables, inside the body, are fields.
	saved := map[ast.Node]string{}
	savedPtr := map[*ast.Ident]string{}
	var set []ast.Node
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		v, ok := t.info.Uses[id].(*types.Var)
		if !ok || cl.field[v] == "" {
			return true
		}
		field := "goSelf." + cl.field[v]
		text := field
		if cl.ptr[v] {
			if sel, ok := t.parent[id].(*ast.SelectorExpr); !ok || sel.X != id {
				text = "(*" + field + ")"
			}
			if old, had := t.ptrSubst[id]; had {
				savedPtr[id] = old
			}
			t.ptrSubst[id] = field
		}
		if old, had := t.subst[id]; had {
			saved[id] = old
		}
		t.subst[id] = text
		set = append(set, id)
		return true
	})
	p.ctx = append(p.ctx, &closureCtx{cl: cl})
	text, ok := t.funcWithHeapParams(lit, lit.Type, nil, lit.Body)
	if !ok {
		text = t.splice(lit)
	}
	p.ctx = p.ctx[:len(p.ctx)-1]
	for _, n := range set {
		id := n.(*ast.Ident)
		delete(t.subst, id)
		delete(t.ptrSubst, id)
		if old, had := saved[id]; had {
			t.subst[id] = old
		}
		if old, had := savedPtr[id]; had {
			t.ptrSubst[id] = old
		}
	}
	fmt.Fprintf(&b, "\nfunc (goSelf *%s) Call%s\n", cl.name, strings.TrimPrefix(text, "func"))
	t.extra = append(t.extra, b.String())
}

// captureTypeText writes the type of a captured variable, where a variable
// of a converted slot has the interface type.
func (t *fileTr) captureTypeText(v *types.Var) string {
	if t.cl.slotConv[slotKey{v: v}] {
		if sig, ok := v.Type().Underlying().(*types.Signature); ok {
			return t.ifaceName(sig)
		}
	}
	return t.typeText(v.Type())
}
