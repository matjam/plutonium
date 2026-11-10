package moves

import (
	"compiler/compat"
	"compiler/internal/load"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
	"pluto/strconv"
	"pluto/strings"
)

// What a function may keep of the memory its inputs reach (language spec
// sections 8.8 and 19.3). The escape analysis says, for each argument of a
// call, whether the callee may keep the pointer it is given (ArgFacts.Self).
// That is not enough when an owner is passed by address to a helper that
// hands a view of what the owner holds to a call that keeps it:
//
//	func lend(l *List) { stash(l.head) }
//
//	lend(&l)
//	task sum(l) // stash kept l.head, which now moves to the task
//
// lend keeps nothing of &l itself, only memory loaded through it. keeps
// records, for each function with a body and each of its inputs (the
// receiver first), whether the function may keep memory loaded through the
// input: an expression that loads through the input (l.head, l.head.next,
// a local variable that holds such a load) is passed to a call that may
// keep it, or the input, or memory loaded through it, is passed to a
// function that may keep memory loaded through its own input. The second
// rule is followed through any number of calls, to a fixed point. A call
// of a function that keeps memory loaded through an argument then counts,
// for the views the argument holds, as a call that keeps them (views.p,
// callStores): the owner may not move to another task afterwards.
//
// A call through an interface, a type parameter's method or a func value,
// or of a function literal, may reach each of the functions the escape
// analysis takes as its candidates (Calls.Callees): for an interface
// method, the methods of that name on the types the program converts to an
// interface; for a func value, the functions whose values may reach the
// call. Each is followed as a static callee is, and so are function
// literals and the bodies of ranges over a function, which have summaries
// of their own. A func value call whose candidates are not known keeps, at
// its worst, memory loaded through everything it is passed (keepTargets).

// A keepSite is the call by which a function may keep memory one of its
// inputs reaches, for messages.
type keepSite struct {
	pos  token.Pos
	text string
}

// valid reports whether s names a call.
func (s keepSite) valid() bool { return s.pos.IsValid() }

// A keepEdge says that input in of from reaches input j of to: if to
// keeps memory loaded through input j, from keeps memory loaded through
// input in.
type keepEdge struct {
	from ast.Node
	in   int
	j    int
}

// A keepTarget is a function a call may reach, by its node in c.keeps (a
// *ast.FuncDecl, *ast.FuncLit or *ast.RangeStmt), whose input shift+j is
// input j of the call.
type keepTarget struct {
	node  ast.Node
	shift int
}

// maxInputs bounds the inputs a summary follows, one bit each.
const maxInputs = 64

// keepScan holds what is known while one function body is scanned: its
// inputs, and for each local variable the inputs whose own value it may
// hold (own) and those through which it may hold loaded memory (held).
type keepScan struct {
	c      *checker
	inputs map[*types2.Var]int
	own    map[*types2.Var]uint64
	held   map[*types2.Var]uint64
	change bool
	// direct: a call's result holds nothing of its arguments. The frees
	// scan uses it, where an imprecise answer would be a false error.
	direct bool
}

// findKeeps computes c.keeps for every function of prog with a body, every
// function literal and every body of a range over a function.
func (c *checker) findKeeps(prog *load.Program) {
	c.keeps = map[ast.Node][]keepSite{}
	c.keepDecls = map[*types2.Func]*ast.FuncDecl{}
	into := map[ast.Node][]keepEdge{} // the edges, by the function reached
	type pending struct {
		fn ast.Node
		j  int
	}
	type scanned struct {
		node   ast.Node
		inputs []*ast.Ident // nil: an input without a name
		body   *ast.BlockStmt
	}
	var funcs []scanned
	for _, p := range prog.Packages {
		for _, file := range p.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.FuncDecl:
					if obj, ok := c.defs[x.Name].(*types2.Func); ok && x.Body != nil {
						c.keepDecls[obj] = x
						funcs = compat.Append(funcs, scanned{node: x, inputs: paramNames(x.Recv, x.Type.Params), body: x.Body})
					}
				case *ast.FuncLit:
					funcs = compat.Append(funcs, scanned{node: x, inputs: paramNames(nil, x.Type.Params), body: x.Body})
				case *ast.RangeStmt:
					if !c.rangeBody(x) || x.Tok != token.DEFINE {
						return true
					}
					var in []*ast.Ident
					for _, e := range []ast.Expr{x.Key, x.Value} {
						id, _ := e.(*ast.Ident)
						in = compat.Append(in, id)
					}
					funcs = compat.Append(funcs, scanned{node: x, inputs: in, body: x.Body})
				}
				return true
			})
		}
	}
	var work []pending
	var scans []keepFunc
	for _, sc := range funcs {
		node := sc.node
		s := pu.New(keepScan{c: c, inputs: map[*types2.Var]int{}, own: map[*types2.Var]uint64{}, held: map[*types2.Var]uint64{}})
		for i, name := range sc.inputs {
			if name == nil || i >= maxInputs {
				continue
			}
			if v, ok := c.defs[name].(*types2.Var); ok && mayView(v.Type()) {
				s.inputs[v] = i
			}
		}
		if len(s.inputs) == 0 {
			continue
		}
		n := len(sc.inputs)
		if len(c.keeps[node]) < n {
			ks := compat.Make[keepSite](n)
			copy(ks, c.keeps[node])
			c.keeps[node] = ks
		}
		s.taint(sc.body)
		scans = compat.Append(scans, keepFunc{node: node, n: n, body: sc.body, scan: s})
		s.calls(sc.body, func(in int, site keepSite) {
			if !c.keeps[node][in].valid() {
				c.keeps[node][in] = site
				work = compat.Append(work, pending{fn: node, j: in})
			}
		}, func(to ast.Node, j, in int) {
			into[to] = compat.Append(into[to], keepEdge{from: node, in: in, j: j})
		})
	}
	for len(work) > 0 {
		w := work[len(work)-1]
		work = work[:len(work)-1]
		site := c.keeps[w.fn][w.j]
		for _, e := range into[w.fn] {
			if e.j != w.j || c.keeps[e.from][e.in].valid() {
				continue
			}
			c.keeps[e.from][e.in] = site
			work = compat.Append(work, pending{fn: e.from, j: e.in})
		}
	}
	c.findFrees(scans)
}

// paramNames returns the names of the inputs of a function, the receiver
// first: nil for one without a name.
func paramNames(recv, params *ast.FieldList) []*ast.Ident {
	var out []*ast.Ident
	for _, l := range []*ast.FieldList{recv, params} {
		if l == nil {
			continue
		}
		for _, field := range l.List {
			if len(field.Names) == 0 {
				out = compat.Append(out, nil)
				continue
			}
			out = compat.Append(out, field.Names...)
		}
	}
	return out
}

// rangeBody reports whether st ranges over a function, whose body is then
// a function of its own (section 12.11).
func (c *checker) rangeBody(st *ast.RangeStmt) bool {
	T := c.typeOf(st.X)
	if T == nil {
		return false
	}
	_, ok := T.Underlying().(*types2.Signature)
	return ok
}

// bit returns the set holding input i.
func bit(i int) uint64 { return uint64(1) << uint(i) }

// local returns the local variable (or input) id names, or nil.
func (s *keepScan) local(id *ast.Ident) *types2.Var {
	v, ok := s.c.objOf(id).(*types2.Var)
	if !ok || v.IsField() || isGlobal(v) {
		return nil
	}
	return v
}

// sets returns the inputs whose own value e may hold (a copy of the
// input, or the address of a part of what it is), and those through which
// it may hold memory loaded through them.
func (s *keepScan) sets(e ast.Expr) (own, held uint64) {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return s.sets(x.X)
	case *ast.Ident:
		v := s.local(x)
		if v == nil {
			return 0, 0
		}
		if i, ok := s.inputs[v]; ok {
			own = bit(i)
		}
		return own | s.own[v], s.held[v]
	case *ast.SelectorExpr:
		sel := s.c.selections[x]
		if sel == nil {
			return 0, 0 // a qualified identifier
		}
		o, h := s.sets(x.X)
		if sel.Kind() == types2.FieldVal && sel.Indirect() {
			return 0, o | h // loaded through a pointer
		}
		return o, h // a part of the value, or a method value holding its receiver
	case *ast.StarExpr:
		o, h := s.sets(x.X)
		return 0, o | h
	case *ast.IndexExpr:
		T := s.c.typeOf(x.X)
		if T == nil {
			return 0, 0
		}
		switch T.Underlying().(type) {
		case *types2.Array:
			return s.sets(x.X)
		case *types2.Slice, *types2.Pointer, *types2.Map:
			o, h := s.sets(x.X)
			return 0, o | h
		}
		return 0, 0
	case *ast.SliceExpr:
		return s.sets(x.X)
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			return s.sets(x.X)
		}
		return 0, 0
	case *ast.TypeAssertExpr:
		return s.sets(x.X)
	case *ast.CompositeLit:
		for _, el := range x.Elts {
			o, h := s.sets(el)
			own, held = own|o, held|h
		}
		return own, held
	case *ast.KeyValueExpr:
		return s.sets(x.Value)
	case *ast.CallExpr:
		tv := s.c.types[x.Fun]
		if tv.IsType() {
			for _, a := range x.Args {
				o, h := s.sets(a)
				own, held = own|o, held|h
			}
			return own, held
		}
		if tv.IsBuiltin() {
			if id, ok := ast.Unparen(x.Fun).(*ast.Ident); ok && id.Name == "append" {
				for _, a := range x.Args {
					o, h := s.sets(a)
					own, held = own|o, held|h
				}
			}
			return own, held
		}
		if s.direct {
			return 0, 0
		}
		// A result the escape analysis says may derive from an
		// argument: memory loaded through it, as l.First() returns
		// l.head.
		s.c.callArgs(x, func(key, val ast.Expr, j int) {
			if af := s.c.calls.Arg(key); af == nil || af.Returned {
				o, h := s.sets(val)
				held |= o | h
			}
		})
		return 0, held
	}
	return 0, 0
}

// store records that the local variable lhs is part of may hold what
// own and held say. A store anywhere else is not followed.
func (s *keepScan) store(lhs ast.Expr, own, held uint64) {
	if own|held == 0 {
		return
	}
	for {
		switch x := lhs.(type) {
		case *ast.ParenExpr:
			lhs = x.X
			continue
		case *ast.SelectorExpr:
			if sel := s.c.selections[x]; sel != nil && sel.Kind() == types2.FieldVal && !sel.Indirect() {
				lhs = x.X
				continue
			}
			return
		case *ast.IndexExpr:
			if T := s.c.typeOf(x.X); T != nil {
				if _, ok := T.Underlying().(*types2.Array); ok {
					lhs = x.X
					continue
				}
			}
			return
		case *ast.Ident:
			v := s.local(x)
			if v == nil || x.Name == "_" {
				return
			}
			if s.own[v]|own != s.own[v] || s.held[v]|held != s.held[v] {
				s.own[v] |= own
				s.held[v] |= held
				s.change = true
			}
		}
		return
	}
}

// taint finds what the local variables of body may hold, to a fixed
// point.
func (s *keepScan) taint(body *ast.BlockStmt) {
	for i := 0; i < 20; i++ {
		s.change = false
		ast.Inspect(body, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.AssignStmt:
				if n.Tok != token.ASSIGN && n.Tok != token.DEFINE {
					return true
				}
				if len(n.Lhs) == len(n.Rhs) {
					for j, r := range n.Rhs {
						o, h := s.sets(r)
						s.store(n.Lhs[j], o, h)
					}
					return true
				}
				var o, h uint64
				for _, r := range n.Rhs {
					ro, rh := s.sets(r)
					o, h = o|ro, h|rh
				}
				for _, l := range n.Lhs {
					s.store(l, o, h)
				}
			case *ast.ValueSpec:
				if len(n.Names) == len(n.Values) {
					for j, r := range n.Values {
						o, h := s.sets(r)
						s.store(n.Names[j], o, h)
					}
					return true
				}
				var o, h uint64
				for _, r := range n.Values {
					ro, rh := s.sets(r)
					o, h = o|ro, h|rh
				}
				for _, l := range n.Names {
					s.store(l, o, h)
				}
			case *ast.RangeStmt:
				o, h := s.sets(n.X)
				for _, l := range []ast.Expr{n.Key, n.Value} {
					if l != nil {
						s.store(l, 0, o|h)
					}
				}
			}
			return true
		})
		if !s.change {
			return
		}
	}
}

// calls finds, in body, the calls that may keep memory loaded through an
// input (kept), and the inputs passed to functions that may keep memory
// loaded through what they are given (edge).
func (s *keepScan) calls(body *ast.BlockStmt, kept func(in int, site keepSite), edge func(to ast.Node, j, in int)) {
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		tv := s.c.types[call.Fun]
		if tv.IsType() || tv.IsBuiltin() {
			return true
		}
		targets, known := s.c.keepTargets(call)
		site := keepSite{pos: call.Pos(), text: exprText(call)}
		s.c.callArgs(call, func(key, val ast.Expr, j int) {
			own, held := s.sets(val)
			if own|held == 0 {
				return
			}
			lost := held
			if !known {
				lost = own | held // a func value of which nothing is known
			} else if !keepsArg(s.c.calls.Arg(key)) {
				lost = 0
			}
			for i := 0; i < maxInputs; i++ {
				if lost&bit(i) != 0 {
					kept(i, site)
				}
			}
			for _, t := range targets {
				for i := 0; i < maxInputs; i++ {
					if (own|held)&bit(i) != 0 {
						edge(t.node, j+t.shift, i)
					}
				}
			}
		})
		return true
	})
}

// keepTargets returns the functions with a body that call may reach, and
// false if they are not known: a call of a func value the escape analysis
// recorded no candidates for, or one of whose candidates has no summary
// here (a method value of an interface, a range body that assigns to
// variables declared outside it).
func (c *checker) keepTargets(call *ast.CallExpr) ([]keepTarget, bool) {
	if to := c.staticCallee(call); to != nil {
		if d := c.keepDecls[to]; d != nil {
			return compat.Clone([]keepTarget{{node: d}}), true
		}
		return nil, true
	}
	cs, ok := c.calls.Callees(call)
	if !ok {
		// Not a call the escape analysis has (one in a function the
		// program does not reach): a method of an interface or a type
		// parameter keeps nothing found here, a func value the worst.
		if se := funSelector(call.Fun); se != nil {
			if sel := c.selections[se]; sel != nil && sel.Kind() == types2.MethodVal {
				return nil, true
			}
		}
		return nil, false
	}
	var out []keepTarget
	for _, cd := range cs {
		switch {
		case cd.Unknown:
			return nil, false
		case cd.Fn != nil:
			if d := c.keepDecls[cd.Fn]; d != nil {
				out = compat.Append(out, keepTarget{node: d, shift: cd.Shift})
			}
		case cd.Lit != nil:
			out = compat.Append(out, keepTarget{node: cd.Lit, shift: cd.Shift})
		case cd.Range != nil:
			if cd.Range.Tok != token.DEFINE && (cd.Range.Key != nil || cd.Range.Value != nil) {
				return nil, false
			}
			out = compat.Append(out, keepTarget{node: cd.Range, shift: cd.Shift})
		}
	}
	return out, true
}

// keepsArg reports whether a call may keep the pointer of an argument it
// is given, by what the escape analysis found (af) over every function the
// call may reach: it may store it past the call, or where the walk does not
// follow. A pointer it only returns is not counted: the result is followed
// as what the argument holds (sets). One the analysis has no facts for (af
// nil) is not counted. Through an interface the facts are those of the
// methods of the types converted to it: vec.Vec's Push growing its buffer
// with v.a.Alloc gets memory of the allocator back, and keeps it in v.
func keepsArg(af *ArgFacts) bool {
	if af == nil {
		return false
	}
	if af.Kept {
		return true
	}
	for _, in := range af.Into {
		if in.Arg == nil {
			return true
		}
	}
	return false
}

// staticCallee returns the function or concrete method call calls, by its
// generic origin, or nil.
func (c *checker) staticCallee(call *ast.CallExpr) *types2.Func {
	fun := ast.Unparen(call.Fun)
	switch ix := fun.(type) {
	case *ast.IndexExpr:
		fun = ast.Unparen(ix.X)
	case *ast.IndexListExpr:
		fun = ast.Unparen(ix.X)
	}
	var obj types2.Object
	switch x := fun.(type) {
	case *ast.Ident:
		obj = c.uses[x]
	case *ast.SelectorExpr:
		if sel := c.selections[x]; sel != nil {
			if sel.Kind() == types2.FieldVal || types2.IsInterface(sel.Recv()) {
				return nil
			}
			if _, isParam := types2.Unalias(sel.Recv()).(*types2.TypeParam); isParam {
				return nil
			}
			obj = sel.Obj()
		} else {
			obj = c.uses[x.Sel]
		}
	}
	fn, _ := obj.(*types2.Func)
	if fn == nil {
		return nil
	}
	return fn.Origin()
}

// callArgs calls fn for each argument of a call to a function or method,
// receiver first, with the input of the callee it is bound to (j): key is
// the expression the escape analysis records facts under, and val the
// value passed. A method expression T.M(x, ...) passes the receiver as its
// first argument.
func (c *checker) callArgs(call *ast.CallExpr, fn func(key, val ast.Expr, j int)) {
	off := 0
	if se := funSelector(call.Fun); se != nil {
		if sel := c.selections[se]; sel != nil && sel.Kind() == types2.MethodVal {
			fn(se.X, se.X, 0)
			off = 1
		}
	}
	T := c.typeOf(call.Fun)
	if T == nil {
		return
	}
	sig, _ := T.Underlying().(*types2.Signature)
	if sig == nil {
		return
	}
	n := sig.Params().Len()
	if len(call.Args) == 1 && n > 1 {
		return // f(g()) with several results
	}
	for i, a := range call.Args {
		j := i
		if n > 0 && j >= n {
			j = n - 1 // packed into the variadic parameter
		}
		fn(a, a, j+off)
	}
}

// calleeKeeps returns the call by which a callee of call may keep memory
// loaded through its argument key, or false.
func (c *checker) calleeKeeps(call *ast.CallExpr, key ast.Expr) (keepSite, bool) {
	targets, known := c.keepTargets(call)
	if !known {
		return keepSite{pos: call.Pos(), text: exprText(call)}, true
	}
	var site keepSite
	found := false
	c.callArgs(call, func(k, _ ast.Expr, j int) {
		if k != key || found {
			return
		}
		for _, t := range targets {
			ks := c.keeps[t.node]
			if j+t.shift < len(ks) && ks[j+t.shift].valid() {
				site, found = ks[j+t.shift], true
				return
			}
		}
	})
	return site, found
}

// keepHow says how a call keeps a view of what an owner holds, for
// messages: "passed to lend, which may keep memory reached through it
// (stash(l.head), line 49)".
func (c *checker) keepHow(call *ast.CallExpr, site keepSite) string {
	at := c.fset.Position(site.pos)
	where := compat.Concat("line ", strconv.Itoa(at.Line))
	if at.Filename != c.fset.Position(call.Pos()).Filename {
		where = compat.Concat(strings.SliceFrom(at.Filename, strings.LastIndexByte(at.Filename, '/')+1), ":", strconv.Itoa(at.Line))
	}
	return compat.Concat("passed to ", exprText(call.Fun), ", which may keep memory reached through it (", site.text, ", ", where, ")")
}
