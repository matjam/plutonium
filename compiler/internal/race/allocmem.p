package race

import (
	"compiler/compat"
	"compiler/internal/constant"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
	"pluto/strings"
)

// Memory allocated from an allocator (language spec sections 18.9 and
// 19.4, Memory reached through views). An arena takes back in bulk what it
// handed out (Reset, Release, Drop) and may then hand it out again, so a
// view of an allocation from &a is a view of the memory of a, wherever a
// is held, and a call that takes memory back in bulk is a write of all of
// it. Alloc and Free are not: by section 18.9, Alloc never takes back
// memory it handed out and Free takes back only the memory it is given,
// so an allocation never meets a live view of another. The place
// allocMem(R) stands for the memory the allocator at place R has handed
// out. It lies past a load, so the allocator's own fields do not hold it
// (a task may read a.Used() while another allocates), and R# does.
//
// A call of a bulk method of an allocator type is recorded, in the
// method's summary, as an atomic write of its receiver's memory. In the
// check of scoped tasks, a use of memory reached through a value built on
// the allocator (a vector holding &a, or a pointer into a's buffer) is
// also a plain read of the allocator's memory, which conflicts with a bulk
// call of another party unless both hold a common lock. A task's
// allocation from an allocator of its own is the task's memory, as
// before: allocMem of a variable the task declares is the task's own. The
// default allocator, which does not take back what it handed out (section
// 18.9), needs nothing: the runtime's and pluto/alloc's allocators are not
// counted.

// memStep is the step that names an allocator's memory. It is a field step
// no program can write.
const memStep = ".<mem>"

// anyArenaVar stands for an arena the compiler cannot name: one behind
// memory the analysis does not follow, or past the limits of section 19.3,
// How precise the race check is. A call on it counts as that call on every
// arena of the program, and an object of it as an object of every arena
// (section 19.3, Frees are writes).
var anyArenaVar = types2.NewVar(token.NoPos, nil, "an arena the compiler cannot name", nil)

// anyArena returns the memory of every arena (anyArenaVar).
func anyArena() vpath { return allocMem(vpath{root: root{kind: rGlobal, v: anyArenaVar}}) }

// isAnyArena reports whether p is the memory of every arena.
func isAnyArena(p vpath) bool { return p.root.kind == rGlobal && p.root.v == anyArenaVar }

// bulkPlace returns the place a bulk call's write of x.place, substituted
// as p, stands for: p itself when it still names the allocator's memory,
// or every arena's when the substitution cut the name off (section 19.3,
// How precise the race check is) or reached memory the analysis does not
// follow. A named allocator that is not shareable (a variable or an object
// of this function) stays named in the check of scoped tasks, which follows
// local variables, and counts as every arena in the check of tasks: the
// objects it handed out may have crossed.
func (a *analyzer) bulkPlace(x *access, p vpath, frame bool) vpath {
	if !x.write || !isAllocMem(x.place) {
		return p
	}
	if !isAllocMem(p) {
		return anyArena()
	}
	switch p.root.kind {
	case rParam:
		return p
	case rLocal:
		if frame {
			return p
		}
	case rGlobal:
		if !a.runtimeVar(p.root.v) {
			return p
		}
	}
	return anyArena()
}

// allocMem returns the place of the memory the allocator at place r has
// handed out.
func allocMem(r vpath) vpath { return r.join(compat.Concat("*", memStep)) }

// isAllocMem reports whether p is the memory of an allocator.
func isAllocMem(p vpath) bool { return strings.HasSuffix(p.path, memStep) }

// allocMemOwner returns the allocator whose memory p is.
func allocMemOwner(p vpath) vpath {
	p.path = strings.TrimSuffix(strings.TrimSuffix(p.path, memStep), "*")
	return p
}

// userAllocator reports whether T is an allocator type outside the runtime
// and pluto/alloc, whose allocators are the default allocator's.
func (a *analyzer) userAllocator(T types2.Type) bool {
	n, ok := types2.Unalias(T).(*types2.Named)
	if !ok || n.Obj() == nil || n.Obj().Pkg() == nil {
		return false
	}
	switch p := n.Obj().Pkg().Path(); {
	case p == "pluto/rt", strings.HasPrefix(p, "pluto/rt/"), p == "pluto/alloc":
		return false
	}
	return implementsAllocator(a, T)
}

// recycler reports whether f is a method of an allocator type that takes
// back in bulk memory the allocator handed out.
func (a *analyzer) recycler(f *function) bool {
	if f.obj == nil || f.sig == nil || f.sig.Recv() == nil || f.bodyless || len(f.params) == 0 {
		return false
	}
	switch f.obj.Name() {
	case "Reset", "Release", "Drop":
	default:
		return false
	}
	el := pointerElem(f.sig.Recv().Type())
	return el != nil && a.userAllocator(el)
}

// noteRecycle records, in the summary of a bulk method of an allocator, the write of all the memory its receiver handed out.
func (w *walker) noteRecycle() {
	if w.frame != nil || !w.a.recycler(w.fn) {
		return
	}
	var pos token.Pos
	if w.fn.decl != nil {
		pos = w.fn.decl.Name.Pos()
	}
	recv := vpath{root: root{kind: rParam, idx: 0}}.join("*")
	w.access(allocMem(recv), true, true, pos, nil, false, "")
}

// noteAllocMem records, in the check of scoped tasks, the read of an
// allocator's memory that use x makes when it reaches memory allocated
// from it: x is a use through variable v, past a load, and v is the
// allocator or holds its address in a field of allocator type.
func (fc *frameCheck) noteAllocMem(w *walker, x *frameAcc) {
	r := x.place.root
	if r.kind == rFresh {
		// An object of an arena's New or Slice (call): the arena's memory
		// (section 19.3, Frees are writes).
		if c, ok := r.node.(ast.Expr); ok {
			if R, ok := fc.a.arenaCallRecv(c); ok {
				for _, R := range fc.arenaPlaces(w, R) {
					fc.add(w.party, pu.New(frameAcc{place: allocMem(R), locks: x.locks, pos: x.pos, via: x.via, typ: x.typ, run: x.run, wrote: x.write}))
				}
				return
			} else if c, ok := ast.Unparen(c).(*ast.CallExpr); ok {
				// An object a callee returns that may come from an arena
				// it is given or reaches: the compiler does not follow
				// which, so it counts as every arena's memory (item 196).
				if fc.a.mayArenaResult(fc.a.staticCallee(c), 0) {
					fc.add(w.party, pu.New(frameAcc{place: anyArena(), locks: x.locks, pos: x.pos, via: x.via, typ: x.typ, run: x.run, wrote: x.write}))
					return
				}
			}
			if fc.a.callsAllocUse(fc.starter) {
				// An object made where an alloc.Use window may be open
				// comes from the allocator it installed, which the
				// compiler does not follow: every allocator's (item 196).
				fc.add(w.party, pu.New(frameAcc{place: anyArena(), locks: x.locks, pos: x.pos, via: x.via, typ: x.typ, run: x.run, wrote: x.write}))
			}
		}
		return
	}
	if r.kind != rLocal && r.kind != rUnknown || r.v == nil {
		return
	}
	if isAllocMem(x.place) {
		if bulkFree(x) && r.kind == rLocal {
			// The name follows a move: after b := take(a), a call on b
			// frees what a handed out before (section 19.3, Frees are
			// writes).
			for _, src := range fc.a.arenaMoves(fc.starter)[r.v] {
				y := *x
				y.place = vpath{root: root{kind: rLocal, v: src}, path: x.place.path}
				if isGlobal(src) {
					y.place.root.kind = rGlobal
				}
				fc.add(w.party, pu.New(y))
			}
		}
		fc.mapAllocMem(w, x)
		return
	}
	if !strings.ContainsAny(x.place.path, "*%$#") {
		return
	}
	for _, R := range fc.allocatorsOf(w, r) {
		fc.add(w.party, pu.New(frameAcc{place: allocMem(R), locks: x.locks, pos: x.pos, via: x.via, typ: x.typ, run: x.run, wrote: x.write}))
	}
}

// mapAllocMem carries a bulk call made through a field of variable v that
// holds an allocator's address (h.a.Reset() where h.a holds &a) over to
// that allocator's memory: the memory of v.f* is the memory of a when v.f
// holds &a.
func (fc *frameCheck) mapAllocMem(w *walker, x *frameAcc) {
	owner := allocMemOwner(x.place)
	if !strings.HasSuffix(owner.path, "*") {
		return
	}
	fp := strings.TrimSuffix(owner.path, "*")
	if fp == "" {
		return
	}
	for _, s := range steps(fp) {
		if !strings.HasPrefix(s, ".") {
			return
		}
	}
	for _, o := range w.u.vars[owner.root.v] {
		if o.tag != fp || !strings.HasSuffix(o.vp.path, "&") {
			continue
		}
		switch o.vp.root.kind {
		case rLocal, rGlobal, rParam:
		default:
			continue
		}
		if o.vp.root.kind == rGlobal && fc.a.runtimeVar(o.vp.root.v) {
			continue
		}
		y := *x
		y.place = allocMem(o.vp.join("*"))
		fc.add(w.party, pu.New(y))
	}
}

// allocatorsOf returns the allocators whose memory a value reached through
// root r may be in: r's variable itself, when it is an allocator, and the
// allocators whose addresses it holds in fields of allocator type.
func (fc *frameCheck) allocatorsOf(w *walker, r root) []vpath {
	var out []vpath
	v := r.v
	if r.kind == rLocal && fc.a.userAllocator(v.Type()) {
		out = compat.Append(out, vpath{root: r})
	}
	if r.kind == rLocal || r.kind == rUnknown {
		// An object of the arena's New or Slice is the arena's memory
		// (section 19.3, Frees are writes).
		for _, R := range fc.a.arenaObjects(fc.starter)[v] {
			out = compat.Append(out, fc.arenaPlaces(w, R)...)
		}
	}
	for _, o := range w.u.vars[v] {
		if o.tag == "" || !strings.HasSuffix(o.vp.path, "&") {
			continue
		}
		switch o.vp.root.kind {
		case rLocal, rGlobal, rParam:
		default:
			continue
		}
		if o.vp.root.kind == rGlobal && fc.a.runtimeVar(o.vp.root.v) {
			continue
		}
		ft := fieldTypeAt(v.Type(), o.tag)
		if ft == nil {
			continue
		}
		ok := false
		if el := pointerElem(ft); el != nil {
			ok = fc.a.userAllocator(el)
		} else if obj := fc.a.allocatorIface(); obj != nil && types2.Identical(types2.Unalias(ft), types2.Unalias(obj.Type())) {
			// An interface: the allocator is the variable whose address it
			// holds, when its type is a user allocator's.
			ok = o.vp.path == "&" && o.vp.root.v != nil && fc.a.userAllocator(o.vp.root.v.Type())
		}
		if !ok {
			continue
		}
		R := o.vp.join("*")
		dup := false
		for _, p := range out {
			if p == R {
				dup = true
			}
		}
		if !dup {
			out = compat.Append(out, R)
		}
	}
	return out
}

// arenaPlaces returns the arenas that R, an arena from arenaCallRecv, may
// be in this walk: R itself when it is reached from its variable with no
// pointer load, else the places the variable's values reach (an arena
// behind a parameter or a pointer), or every arena when the walk does not
// follow them.
func (fc *frameCheck) arenaPlaces(w *walker, R vpath) []vpath {
	if R.root.kind != rLocal || !strings.HasPrefix(R.path, "*") {
		return compat.Clone([]vpath{R})
	}
	var out []vpath
	for _, o := range w.u.vars[R.root.v] {
		switch o.vp.root.kind {
		case rLocal, rGlobal, rParam:
		default:
			return compat.Clone([]vpath{allocMemOwner(anyArena())})
		}
		p := o.vp.join(R.path)
		dup := false
		for _, q := range out {
			dup = dup || q == p
		}
		if !dup {
			out = compat.Append(out, p)
		}
	}
	if len(out) == 0 {
		return compat.Clone([]vpath{allocMemOwner(anyArena())})
	}
	return out
}

// arenaMoves returns, for the arena variables of f (and of the literals in
// it) set from take of another arena variable, the variables they were
// taken from, transitively.
func (a *analyzer) arenaMoves(f *function) map[*types2.Var][]*types2.Var {
	if f == nil || f.body == nil {
		return nil
	}
	if m, ok := a.arenaTakes[f]; ok {
		return m
	}
	if a.arenaTakes == nil {
		a.arenaTakes = map[*function]map[*types2.Var][]*types2.Var{}
	}
	direct := map[*types2.Var][]*types2.Var{}
	ast.Inspect(f.body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Lhs) != len(as.Rhs) {
			return true
		}
		for i, r := range as.Rhs {
			c, ok := ast.Unparen(r).(*ast.CallExpr)
			if !ok || len(c.Args) != 1 {
				continue
			}
			if id, ok := ast.Unparen(c.Fun).(*ast.Ident); !ok || id.Name != "take" || !a.types[c.Fun].IsBuiltin() {
				continue
			}
			dst, ok1 := ast.Unparen(as.Lhs[i]).(*ast.Ident)
			arg := ast.Unparen(c.Args[0])
			if u, ok := arg.(*ast.UnaryExpr); ok && u.Op == token.AND {
				arg = ast.Unparen(u.X)
			}
			src, ok2 := arg.(*ast.Ident)
			if !ok1 || !ok2 {
				continue
			}
			dv, ok1 := a.objOf(dst).(*types2.Var)
			sv, ok2 := a.objOf(src).(*types2.Var)
			if !ok1 || !ok2 || !a.userAllocator(dv.Type()) {
				continue
			}
			direct[dv] = compat.Append(direct[dv], sv)
		}
		return true
	})
	m := map[*types2.Var][]*types2.Var{}
	for v := range direct {
		seen := map[*types2.Var]bool{v: true}
		work := compat.Clone(direct[v])
		for len(work) > 0 {
			x := work[len(work)-1]
			work = work[:len(work)-1]
			if seen[x] {
				continue
			}
			seen[x] = true
			m[v] = compat.Append(m[v], x)
			work = compat.Append(work, direct[x]...)
		}
	}
	a.arenaTakes[f] = m
	return m
}

// arenaObjects returns, for the variables of f (and of the literals in it)
// set from a call of an arena's New or Slice, the places of the arenas the
// calls allocate from: the receiver variable, or what a pointer receiver
// variable points to.
func (a *analyzer) arenaObjects(f *function) map[*types2.Var][]vpath {
	if f == nil || f.body == nil {
		return nil
	}
	if m, ok := a.arenaVars[f]; ok {
		return m
	}
	if a.arenaVars == nil {
		a.arenaVars = map[*function]map[*types2.Var][]vpath{}
	}
	m := map[*types2.Var][]vpath{}
	a.arenaVars[f] = m
	ast.Inspect(f.body, func(n ast.Node) bool {
		var lhs []ast.Expr
		var rhs []ast.Expr
		switch as := n.(type) {
		case *ast.AssignStmt:
			lhs, rhs = as.Lhs, as.Rhs
		case *ast.ValueSpec:
			for _, id := range as.Names {
				lhs = compat.Append(lhs, ast.Expr(id))
			}
			rhs = as.Values
		}
		if len(lhs) != len(rhs) {
			return true
		}
		for i, r := range rhs {
			id, ok := ast.Unparen(lhs[i]).(*ast.Ident)
			if !ok {
				continue
			}
			v, ok := a.objOf(id).(*types2.Var)
			if !ok {
				continue
			}
			if R, ok := a.arenaCallRecv(r); ok {
				m[v] = compat.Append(m[v], R)
			} else if c, ok := ast.Unparen(r).(*ast.CallExpr); ok && a.mayArenaResult(a.staticCallee(c), 0) {
				// An object a callee may have taken from an arena: the
				// compiler does not follow which, so every arena's
				// (item 196).
				m[v] = compat.Append(m[v], allocMemOwner(anyArena()))
			} else if a.callsAllocUse(f) && madeHere(r) {
				// An object made, or returned by a call, where an
				// alloc.Use window may be open: the allocator it installed
				// is not followed, so every allocator's (item 196).
				m[v] = compat.Append(m[v], allocMemOwner(anyArena()))
			}
		}
		return true
	})
	return m
}

// arenaCallRecv returns the arena a call of New or Slice of pluto/arena
// allocates from, when its receiver is a variable.
func (a *analyzer) arenaCallRecv(e ast.Expr) (vpath, bool) {
	c, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return vpath{}, false
	}
	sel, ok := ast.Unparen(stripIndex(c.Fun)).(*ast.SelectorExpr)
	if !ok {
		return vpath{}, false
	}
	s := a.selections[sel]
	if s == nil || s.Kind() != types2.MethodVal {
		return vpath{}, false
	}
	fn, ok := s.Obj().(*types2.Func)
	if !ok || !arenaAllocFunc(fn) || fn.Pkg().Path() != "pluto/arena" {
		return vpath{}, false
	}
	if R, ok := a.arenaRecvName(sel); ok {
		return R, true
	}
	// An arena the compiler cannot name: every arena.
	return allocMemOwner(anyArena()), true
}

// arenaRecvName names the arena that the receiver of sel, a New or Slice of
// pluto/arena, is: a variable, or a field of one reached by selections of
// struct fields, named as a lock is.
func (a *analyzer) arenaRecvName(sel *ast.SelectorExpr) (vpath, bool) {
	// The receiver: a variable, or a field of one reached by selections
	// of struct fields (h.a, or h.a where h is a pointer), named as a lock
	// is.
	path := ""
	x := ast.Unparen(sel.X)
	for {
		fs, ok := x.(*ast.SelectorExpr)
		if !ok {
			break
		}
		s := a.selections[fs]
		if s == nil || s.Kind() != types2.FieldVal || len(s.Index()) != 1 {
			return vpath{}, false
		}
		step := compat.Concat(".", fs.Sel.Name)
		if pointerElem(a.typeOf(fs.X)) != nil {
			step = compat.Concat("*", step)
		}
		path = compat.Concat(step, path)
		x = ast.Unparen(fs.X)
	}
	id, ok := x.(*ast.Ident)
	if !ok {
		return vpath{}, false
	}
	v, ok := a.objOf(id).(*types2.Var)
	if !ok {
		return vpath{}, false
	}
	rt := root{kind: rLocal, v: v}
	if isGlobal(v) {
		rt = root{kind: rGlobal, v: v}
	}
	T := a.typeOf(sel.X)
	if a.userAllocator(T) {
		return vpath{root: rt, path: path}, true
	}
	if el := pointerElem(T); el != nil && a.userAllocator(el) {
		return vpath{root: rt, path: compat.Concat(path, "*")}, true
	}
	return vpath{}, false
}

// fieldTypeAt returns the type of the field that tag, a path of field
// steps, selects in a value of type T, or nil.
func fieldTypeAt(T types2.Type, tag string) types2.Type {
	for _, s := range steps(tag) {
		if !strings.HasPrefix(s, ".") {
			return nil
		}
		st, ok := underlying(T).(*types2.Struct)
		if !ok {
			return nil
		}
		name := strings.SliceFrom(s, 1)
		var next types2.Type
		for i := 0; i < st.NumFields(); i++ {
			if f := st.Field(i); f.Name() == name {
				next = f.Type()
				break
			}
		}
		if next == nil {
			return nil
		}
		T = next
	}
	return T
}

// Drop entries (section 18.9, Drop entries). New and Slice of an
// arena.Fixed or arena.Chunked record an entry for a type with drop glue,
// and Reset, Release and Drop of the arena run the entries: the call runs
// the Drop methods that the drop glue of each such type reaches. The race
// check counts what those methods do at the call (section 19.3), for every
// type passed to New or Slice of that arena, or of every arena where the
// compiler cannot tell which.

// entryArena reports whether fn is a method of arena.Fixed or
// arena.Chunked.
func entryArena(fn *types2.Func) bool {
	if fn == nil || fn.Pkg() == nil || fn.Pkg().Path() != "pluto/arena" {
		return false
	}
	sig, ok := fn.Type().(*types2.Signature)
	if !ok || sig.Recv() == nil {
		return false
	}
	T := sig.Recv().Type()
	if el := pointerElem(T); el != nil {
		T = el
	}
	n, ok := types2.Unalias(T).(*types2.Named)
	if !ok {
		return false
	}
	switch n.Obj().Name() {
	case "Fixed", "Chunked":
		return true
	}
	return false
}

// entryRun reports whether fn runs an arena's drop entries: Reset,
// Release or Drop of arena.Fixed or arena.Chunked.
func entryRun(fn *types2.Func) bool {
	if !entryArena(fn) {
		return false
	}
	switch fn.Name() {
	case "Reset", "Release", "Drop":
		return true
	}
	return false
}

// arenaEntryTypes returns, for each arena variable, the types passed to
// New or Slice of a Fixed or Chunked arena named by it (arenaRecvName),
// and under anyArenaVar the types passed to an arena the compiler cannot
// name. A Slice of constant length zero records no entry.
func (a *analyzer) arenaEntryTypes() map[*types2.Var][]types2.Type {
	if a.entryTypes != nil {
		return a.entryTypes
	}
	a.entryTypes = map[*types2.Var][]types2.Type{}
	for _, p := range a.prog.Packages {
		for _, file := range p.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				c, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := ast.Unparen(stripIndex(c.Fun)).(*ast.SelectorExpr)
				if !ok {
					return true
				}
				s := a.selections[sel]
				if s == nil || s.Kind() != types2.MethodVal {
					return true
				}
				fn, ok := s.Obj().(*types2.Func)
				if !ok || !arenaAllocFunc(fn) || !entryArena(fn) {
					return true
				}
				if fn.Name() == "Slice" && len(c.Args) == 1 {
					if tv, ok := a.types[c.Args[0]]; ok && !tv.Value.IsNil() {
						if k, exact := constant.Int64Val(constant.ToInt(tv.Value)); exact && k == 0 {
							return true
						}
					}
				}
				sig, ok := a.typeOf(c.Fun).(*types2.Signature)
				if !ok {
					return true
				}
				T := sig.Results().At(0).Type()
				if el := pointerElem(T); el != nil {
					T = el
				} else if sl, ok := underlying(T).(*types2.Slice); ok {
					T = sl.Elem()
				}
				key := anyArenaVar
				if R, ok := a.arenaRecvName(sel); ok && R.root.v != nil {
					key = R.root.v
				}
				a.entryTypes[key] = compat.Append(a.entryTypes[key], T)
				return true
			})
		}
	}
	return a.entryTypes
}

// dropGlueFns returns the Drop methods that dropping a value of type T
// runs: its own, and those of the values it holds in place or owns, so
// through struct fields, array elements and boxes.
func (a *analyzer) dropGlueFns(T types2.Type) []*function {
	if fs, ok := a.dropFns[T]; ok {
		return fs
	}
	if a.dropFns == nil {
		a.dropFns = map[types2.Type][]*function{}
	}
	var out []*function
	seen := map[*function]bool{}
	seenT := map[types2.Type]bool{}
	var walk func(T types2.Type, depth int)
	walk = func(T types2.Type, depth int) {
		if T == nil || depth > 6 || seenT[T] {
			return
		}
		seenT[T] = true
		if tp, ok := types2.Unalias(T).(*types2.TypeParam); ok {
			for _, arg := range a.tpArgs[tp] {
				walk(arg, depth+1)
			}
			return
		}
		if n, ok := types2.Unalias(T).(*types2.Named); ok {
			for i := 0; i < n.NumMethods(); i++ {
				m := n.Method(i)
				if m.Name() != "Drop" {
					continue
				}
				if f := a.funcs[m.Origin()]; f != nil && f.unit != nil && !seen[f] {
					seen[f] = true
					out = compat.Append(out, f)
				}
			}
		}
		switch u := underlying(T).(type) {
		case *types2.Struct:
			for i := 0; i < u.NumFields(); i++ {
				walk(u.Field(i).Type(), depth+1)
			}
		case *types2.Array:
			walk(u.Elem(), depth+1)
		case *types2.Wrapper:
			walk(u.Elem(), depth+1)
		}
	}
	walk(T, 0)
	a.dropFns[T] = out
	return out
}

// entryFns returns the Drop methods the drop entries of the arena at the
// receiver of sel may run: for an arena named by a variable, those of the
// types passed to New or Slice of that variable, of a variable moved into
// it with take, and of an arena the compiler cannot name; otherwise those
// of every type passed to New or Slice of any such arena.
func (a *analyzer) entryFns(sel *ast.SelectorExpr, fn *function) []*function {
	m := a.arenaEntryTypes()
	var Ts []types2.Type
	R, named := vpath{}, false
	if sel != nil {
		R, named = a.arenaRecvName(sel)
	}
	if named && R.root.v != nil && !strings.HasSuffix(R.path, "*") {
		Ts = compat.Append(Ts, m[R.root.v]...)
		for _, src := range a.arenaMoves(fn)[R.root.v] {
			Ts = compat.Append(Ts, m[src]...)
		}
		Ts = compat.Append(Ts, m[anyArenaVar]...)
	} else {
		for _, ts := range m {
			Ts = compat.Append(Ts, ts...)
		}
	}
	var out []*function
	seen := map[*function]bool{}
	for _, T := range Ts {
		for _, f := range a.dropGlueFns(T) {
			if !seen[f] {
				seen[f] = true
				out = compat.Append(out, f)
			}
		}
	}
	return out
}

// runEntries counts, at call e of the static callee f, the Drop methods
// the drop entries of an arena run when f is its Reset, Release or Drop.
// The values dropped are the arena's, which the call already frees.
func (w *walker) runEntries(e *ast.CallExpr, f *function) {
	sel, _ := ast.Unparen(stripIndex(e.Fun)).(*ast.SelectorExpr)
	w.st = w.entriesAfter(f, sel, e.Pos(), exprText(e.Fun), w.st)
}

// entriesAfter returns the state after the drop entries that f runs, from
// st, when f is an arena's Reset, Release or Drop. sel is the call's
// selector, or nil for a call through an interface or a func value: those
// run the entries of every arena, as the compiler does not tell which.
func (w *walker) entriesAfter(f *function, sel *ast.SelectorExpr, pos token.Pos, name string, st *state) *state {
	if f == nil || !entryRun(f.obj) {
		return st
	}
	for _, d := range w.a.entryFns(sel, w.fn) {
		if d.sum == nil {
			continue
		}
		_, st = w.apply(d, compat.Make[origins](len(d.params)), pos, compat.Concat(d.name, " (a drop entry of ", name, ")"), st, nil)
	}
	return st
}

// mayArenaResult reports whether f, outside the library, may return an
// object of an arena's New or Slice: its body calls one, or calls a
// function that may.
func (a *analyzer) mayArenaResult(f *function, depth int) bool {
	if f == nil || f.body == nil || f.pkg == nil || strings.HasPrefix(f.pkg.Path, "pluto/") || depth > 8 {
		return false
	}
	if r, ok := a.arenaRes[f]; ok {
		return r
	}
	if a.arenaRes == nil {
		a.arenaRes = map[*function]bool{}
	}
	if f.sig == nil || f.sig.Results().Len() == 0 {
		a.arenaRes[f] = false
		return false
	}
	a.arenaRes[f] = false // a cycle adds nothing
	found := false
	ast.Inspect(f.body, func(n ast.Node) bool {
		if found {
			return false
		}
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if _, ok := a.arenaCallRecv(c); ok || a.isAllocUse(c) {
			found = true
			return false
		}
		if a.mayArenaResult(a.staticCallee(c), depth+1) {
			found = true
			return false
		}
		return true
	})
	a.arenaRes[f] = found
	return found
}

// staticCallee returns the declared function or method c calls by name, or
// nil.
func (a *analyzer) staticCallee(c *ast.CallExpr) *function {
	var fn *types2.Func
	switch g := ast.Unparen(stripIndex(c.Fun)).(type) {
	case *ast.Ident:
		fn, _ = a.uses[g].(*types2.Func)
	case *ast.SelectorExpr:
		if sel := a.selections[g]; sel != nil {
			if sel.Kind() == types2.MethodVal {
				fn, _ = sel.Obj().(*types2.Func)
			}
		} else {
			fn, _ = a.uses[g.Sel].(*types2.Func)
		}
	}
	if fn == nil {
		return nil
	}
	return a.funcs[fn.Origin()]
}

// isAllocUse reports whether c calls alloc.Use, which opens a window where
// new objects come from the allocator it installs.
func (a *analyzer) isAllocUse(c *ast.CallExpr) bool {
	var fn *types2.Func
	switch g := ast.Unparen(c.Fun).(type) {
	case *ast.Ident:
		fn, _ = a.uses[g].(*types2.Func)
	case *ast.SelectorExpr:
		fn, _ = a.uses[g.Sel].(*types2.Func)
	}
	return fn != nil && fn.Name() == "Use" && fn.Pkg() != nil && fn.Pkg().Path() == "pluto/alloc"
}

// callsAllocUse reports whether the body of f, its literals included,
// calls alloc.Use.
func (a *analyzer) callsAllocUse(f *function) bool {
	if f == nil || f.body == nil {
		return false
	}
	if r, ok := a.allocUse[f]; ok {
		return r
	}
	if a.allocUse == nil {
		a.allocUse = map[*function]bool{}
	}
	found := false
	ast.Inspect(f.body, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok && !found && a.isAllocUse(c) {
			found = true
		}
		return !found
	})
	a.allocUse[f] = found
	return found
}

// madeHere reports whether e makes a new object or returns one from a call:
// a call, or the address of a composite literal.
func madeHere(e ast.Expr) bool {
	switch x := ast.Unparen(e).(type) {
	case *ast.CallExpr:
		return true
	case *ast.UnaryExpr:
		_, ok := ast.Unparen(x.X).(*ast.CompositeLit)
		return ok && x.Op == token.AND
	}
	return false
}
