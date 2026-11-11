package race

import (
	"compiler/compat"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
	"pluto/strings"
)

// This file decides which places of maps are kept in place (language spec
// section 19.3, Maps kept in place). A place is a package variable of map
// type, or a field of map type of a struct type. It is kept in place when
// every value stored in it is new and every other occurrence of it indexes
// it, passes it to len, delete or clear, ranges over it, compares it with
// nil, or passes it to a parameter or receiver that keeps it in place. The
// contents of a map kept in place are a datum of the place, per object;
// those of any other map are one datum for all maps of its type
// (checkMaps).

// A mapOut is an occurrence that takes a map out of its place: a copy, or
// a store of a value that is not new.
type mapOut struct {
	pkg  string
	pos  token.Pos
	text string // "`return c.m` at cache.p:12 copies the map out of `Cache.m`"
}

// before orders occurrences by package path, then file name, then
// position.
func (a *analyzer) outBefore(x, y *mapOut) bool {
	if x.pkg != y.pkg {
		return x.pkg < y.pkg
	}
	px, py := a.pos(x.pos), a.pos(y.pos)
	if px.Filename != py.Filename {
		return px.Filename < py.Filename
	}
	return px.Offset < py.Offset
}

type mapPlaces struct {
	a      *analyzer
	pkg    string
	params map[*types2.Var]bool             // map-typed parameters and receivers of declared functions
	direct map[*types2.Var]*mapOut          // place or parameter: its first occurrence that takes the map out
	deps   map[*types2.Var][]*types2.Var    // place or parameter: the parameters it is passed to
	label  map[*types2.Var]string           // field place: "Cache.m"
	ids    map[*types2.Var][]int            // field place: the map type numbers of its instantiations
	fields []*types2.Var                    // field places, in the order seen
	inMemo map[types2.Type][]*types2.Var    // the field places a struct or array value holds
}

// A field place's key in mapFieldOut: its name and its map type number,
// as an access's place shows them (".m$5").
func fieldKey(name string, id int) string {
	return compat.Sprintfb("%s|%d", compat.ArgS(name), compat.ArgI(int64(id)))
}

// findMapPlaces walks the whole program and fills mapVarOut, mapFieldOut
// and mapFieldLabel.
func (a *analyzer) findMapPlaces() {
	mp := pu.New(mapPlaces{
		a:      a,
		params: map[*types2.Var]bool{},
		direct: map[*types2.Var]*mapOut{},
		deps:   map[*types2.Var][]*types2.Var{},
		label:  map[*types2.Var]string{},
		ids:    map[*types2.Var][]int{},
		inMemo: map[types2.Type][]*types2.Var{},
	})
	for _, fn := range a.funcs {
		for _, v := range fn.params {
			if v != nil && isMapType(v.Type()) {
				mp.params[v] = true
			}
		}
	}
	cc := pu.New(copyCheck{a: a, memo: map[types2.Type]string{}})
	cc.sink = mp
	for _, p := range a.prog.Packages {
		mp.pkg = p.Path
		for _, f := range p.Files {
			mp.packageVars(f)
			ast.PreorderStack(f, nil, func(n ast.Node, stack []ast.Node) bool {
				mp.visit(n, stack)
				return true
			})
			ast.Inspect(f, cc.node)
		}
	}
	a.mapVarOut = map[*types2.Var]*mapOut{}
	a.mapFieldOut = map[string]*mapOut{}
	a.mapFieldKept = map[string]bool{}
	a.mapFieldLabel = map[string]string{}
	for v := range mp.direct {
		if !v.IsField() && !mp.params[v] {
			a.mapVarOut[v] = mp.out(v)
		}
	}
	for v := range mp.deps {
		if !v.IsField() && !mp.params[v] && a.mapVarOut[v] == nil {
			if o := mp.out(v); o != nil {
				a.mapVarOut[v] = o
			}
		}
	}
	for _, v := range mp.fields {
		o := mp.out(v)
		if o == nil && mentionsTypeParam(v.Type()) {
			// The contents of a map whose type depends on a type parameter
			// are reached in generic code under another type number: they
			// stay with the datum of the type.
			o = pu.New(mapOut{pkg: mp.pkgOf(v), pos: v.Pos(), text: compat.Concat("the type of `", mp.label[v], "` depends on a type parameter, so the map is not kept in place")})
		}
		for _, id := range mp.ids[v] {
			k := fieldKey(v.Name(), id)
			if _, ok := a.mapFieldLabel[k]; !ok {
				a.mapFieldLabel[k] = mp.label[v]
				a.mapFieldKept[k] = true
			}
			if o != nil {
				// Two fields of one name and map type share the key: a
				// copy of either takes both out.
				a.mapFieldKept[k] = false
				if old := a.mapFieldOut[k]; old == nil || a.outBefore(o, old) {
					a.mapFieldOut[k] = o
				}
			}
		}
	}
}

func (mp *mapPlaces) pkgOf(v *types2.Var) string {
	if v.Pkg() != nil {
		return v.Pkg().Path()
	}
	return ""
}

// out returns the first occurrence that takes the map out of place v,
// directly or through the parameters it is passed to; nil when v is kept
// in place.
func (mp *mapPlaces) out(v *types2.Var) *mapOut {
	seen := map[*types2.Var]bool{v: true}
	work := []*types2.Var{v}
	var best *mapOut
	for len(work) > 0 {
		x := work[len(work)-1]
		work = work[:len(work)-1]
		if o := mp.direct[x]; o != nil && (best == nil || mp.a.outBefore(o, best)) {
			best = o
		}
		for _, d := range mp.deps[x] {
			if !seen[d] {
				seen[d] = true
				work = compat.Append(work, d)
			}
		}
	}
	return best
}

func isMapType(T types2.Type) bool {
	if T == nil {
		return false
	}
	_, ok := underlying(T).(*types2.Map)
	return ok
}

// field records a field place, its label and the map type number of this
// instantiation of it.
func (mp *mapPlaces) field(v *types2.Var, holder types2.Type) *types2.Var {
	o := v.Origin()
	if _, ok := mp.label[o]; !ok {
		mp.fields = compat.Append(mp.fields, o)
		name := types2.TypeString(holder, mp.a.qualifier)
		if p := pointerElem(holder); p != nil {
			holder = p
		}
		if n, ok := types2.Unalias(holder).(*types2.Named); ok {
			name = n.Obj().Name()
		}
		mp.label[o] = compat.Concat(name, ".", o.Name())
	}
	for _, T := range []types2.Type{v.Type(), o.Type()} {
		if m, ok := underlying(T).(*types2.Map); ok {
			id := mp.a.mapTypeID(m)
			found := false
			for _, x := range mp.ids[o] {
				if x == id {
					found = true
				}
			}
			if !found {
				mp.ids[o] = compat.AppendPlain(mp.ids[o], id)
			}
		}
	}
	return o
}

// bad records an occurrence that takes the map out of place v.
func (mp *mapPlaces) bad(v *types2.Var, pos token.Pos, what string, store bool) {
	verb := " copies the map out of `"
	if store {
		verb = " stores a map that is not new in `"
	}
	name := v.Name()
	if l, ok := mp.label[v]; ok {
		name = l
	}
	if mp.params[v] {
		verb = " copies the map out of parameter `"
	}
	o := pu.New(mapOut{pkg: mp.pkg, pos: pos, text: compat.Concat(what, " at ", mp.a.short(pos), verb, name, "`")})
	if old := mp.direct[v]; old == nil || mp.a.outBefore(o, old) {
		mp.direct[v] = o
	}
}

// isNew reports whether e is a new map: a make call, a map composite
// literal or nil.
func (mp *mapPlaces) isNew(e ast.Expr) bool {
	a := mp.a
	e = ast.Unparen(e)
	if a.types[e].IsNil() {
		return true
	}
	switch e := e.(type) {
	case *ast.CompositeLit:
		return isMapType(a.typeOf(e))
	case *ast.CallExpr:
		if id, ok := ast.Unparen(e.Fun).(*ast.Ident); ok && id.Name == "make" && a.types[e.Fun].IsBuiltin() {
			return true
		}
	}
	return false
}

// packageVars checks the initializers of a file's package variables of
// map type.
func (mp *mapPlaces) packageVars(f *ast.File) {
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, s := range gd.Specs {
			vs, ok := s.(*ast.ValueSpec)
			if !ok || len(vs.Values) == 0 {
				continue
			}
			for i, name := range vs.Names {
				v, ok := mp.a.defs[name].(*types2.Var)
				if !ok || !isMapType(v.Type()) {
					continue
				}
				if len(vs.Values) == len(vs.Names) && mp.isNew(vs.Values[i]) {
					continue
				}
				mp.bad(v, name.Pos(), compat.Concat("the initializer of `", name.Name, "`"), true)
			}
		}
	}
}

// visit classifies an occurrence of a place or of a map-typed parameter,
// and checks the stores of struct composite literals.
func (mp *mapPlaces) visit(n ast.Node, stack []ast.Node) {
	a := mp.a
	switch n := n.(type) {
	case *ast.SelectorExpr:
		if sel := a.selections[n]; sel != nil {
			if sel.Kind() != types2.FieldVal {
				return
			}
			if v, ok := sel.Obj().(*types2.Var); ok && isMapType(v.Type()) {
				mp.use(mp.field(v, sel.Recv()), n, stack)
			}
			return
		}
		if v, ok := a.uses[n.Sel].(*types2.Var); ok && isGlobal(v) && isMapType(v.Type()) {
			mp.use(v, n, stack)
		}
	case *ast.Ident:
		if len(stack) > 0 {
			switch p := stack[len(stack)-1].(type) {
			case *ast.SelectorExpr:
				if p.Sel == n {
					return
				}
			case *ast.KeyValueExpr:
				if p.Key == n && len(stack) > 1 {
					if cl, ok := stack[len(stack)-2].(*ast.CompositeLit); ok && mp.structOf(cl) != nil {
						return
					}
				}
			}
		}
		v, ok := a.uses[n].(*types2.Var)
		if !ok || !isMapType(v.Type()) {
			return
		}
		if (isGlobal(v) && !v.IsField()) || mp.params[v] {
			mp.use(v, n, stack)
		}
	case *ast.CompositeLit:
		st := mp.structOf(n)
		if st == nil {
			return
		}
		T := a.typeOf(n)
		for i, el := range n.Elts {
			var f *types2.Var
			val := el
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				val = kv.Value
				if id, ok := kv.Key.(*ast.Ident); ok {
					f, _ = a.uses[id].(*types2.Var)
				}
			} else if i < st.NumFields() {
				f = st.Field(i)
			}
			if f == nil || !isMapType(f.Type()) {
				continue
			}
			o := mp.field(f, T)
			if !mp.isNew(val) {
				mp.bad(o, val.Pos(), compat.Concat("`", f.Name(), ": ", exprText(val), "`"), true)
			}
		}
	}
}

// structOf returns the struct type of a struct composite literal.
func (mp *mapPlaces) structOf(cl *ast.CompositeLit) *types2.Struct {
	T := mp.a.typeOf(cl)
	if p := pointerElem(T); p != nil {
		T = p
	}
	if T == nil {
		return nil
	}
	st, _ := underlying(T).(*types2.Struct)
	return st
}

// use classifies one occurrence e of place or parameter v by what
// encloses it.
func (mp *mapPlaces) use(v *types2.Var, e ast.Expr, stack []ast.Node) {
	a := mp.a
	child := ast.Node(e)
	i := len(stack) - 1
	for i >= 0 {
		pe, ok := stack[i].(*ast.ParenExpr)
		if !ok {
			break
		}
		child = pe
		i--
	}
	what := compat.Concat("`", exprText(e), "`")
	if i < 0 {
		mp.bad(v, e.Pos(), what, false)
		return
	}
	switch p := stack[i].(type) {
	case *ast.IndexExpr:
		if p.X == child {
			return
		}
	case *ast.RangeStmt:
		if p.X == child {
			return
		}
		what = compat.Concat("the range value `", exprText(e), "`")
	case *ast.BinaryExpr:
		if p.Op == token.EQL || p.Op == token.NEQ {
			other := p.X
			if p.X == child {
				other = p.Y
			}
			if a.types[ast.Unparen(other)].IsNil() {
				return
			}
		}
	case *ast.AssignStmt:
		for k, l := range p.Lhs {
			if l != child {
				continue
			}
			if mp.params[v] {
				mp.bad(v, e.Pos(), compat.Concat("the assignment to `", exprText(e), "`"), false)
				return
			}
			if p.Tok == token.ASSIGN && len(p.Lhs) == len(p.Rhs) && mp.isNew(p.Rhs[k]) {
				return
			}
			rhs := "..."
			if len(p.Lhs) == len(p.Rhs) {
				rhs = exprText(p.Rhs[k])
			}
			mp.bad(v, e.Pos(), compat.Concat("`", exprText(e), " ", p.Tok.String(), " ", rhs, "`"), true)
			return
		}
		if len(p.Lhs) == len(p.Rhs) {
			for k, r := range p.Rhs {
				if r == child {
					what = compat.Concat("`", exprText(p.Lhs[k]), " ", p.Tok.String(), " ", exprText(e), "`")
				}
			}
		}
	case *ast.ReturnStmt:
		what = compat.Concat("`return ", exprText(e), "`")
	case *ast.SendStmt:
		if p.Value == child {
			what = compat.Concat("`", exprText(p.Chan), " <- ", exprText(e), "`")
		}
	case *ast.UnaryExpr:
		if p.Op == token.AND {
			what = compat.Concat("`&", exprText(e), "`")
		}
	case *ast.CallExpr:
		if p.Fun == child {
			break
		}
		if mp.argument(v, p, child, stack[:i]) {
			return
		}
		what = compat.Concat("the argument `", exprText(e), "` of `", exprText(p.Fun), "`")
	case *ast.SelectorExpr:
		// A method of a named map type: the receiver keeps the place when
		// the method is called directly and its value receiver keeps it.
		if p.X == child && i > 0 {
			if c, ok := stack[i-1].(*ast.CallExpr); ok && c.Fun == p && !mp.inTask(c, stack[:i-1]) {
				if sel := a.selections[p]; sel != nil && sel.Kind() == types2.MethodVal {
					if fn, ok := sel.Obj().(*types2.Func); ok {
						if r := fn.Type().(*types2.Signature).Recv(); r != nil && isMapType(r.Type()) && pointerElem(r.Type()) == nil {
							if f := a.funcs[fn.Origin()]; f != nil && len(f.params) > 0 && mp.params[f.params[0]] {
								mp.deps[v] = compat.Append(mp.deps[v], f.params[0])
								return
							}
						}
					}
				}
			}
			what = compat.Concat("the receiver `", exprText(e), "` of `", exprText(p), "`")
		}
	}
	mp.bad(v, e.Pos(), what, false)
}

// inTask reports whether call c is the call of a task statement: its
// arguments cross to the new task.
func (mp *mapPlaces) inTask(c *ast.CallExpr, stack []ast.Node) bool {
	if len(stack) == 0 {
		return false
	}
	ts, ok := stack[len(stack)-1].(*ast.TaskStmt)
	return ok && ts.Call == c
}

// argument records place v passed as an argument of call c: true when the
// parameter it is passed to is a declared function's map-typed parameter,
// which keeps the place if its uses do.
func (mp *mapPlaces) argument(v *types2.Var, c *ast.CallExpr, arg ast.Node, stack []ast.Node) bool {
	a := mp.a
	if mp.inTask(c, stack) {
		return false
	}
	tv := a.types[c.Fun]
	if tv.IsType() {
		return false
	}
	if tv.IsBuiltin() {
		if id, ok := ast.Unparen(c.Fun).(*ast.Ident); ok && len(c.Args) > 0 && c.Args[0] == arg {
			switch id.Name {
			case "len", "delete", "clear":
				return true
			}
		}
		return false
	}
	idx := -1
	for k, x := range c.Args {
		if x == arg {
			idx = k
		}
	}
	if idx < 0 || (hasDots(c) && idx == len(c.Args)-1) {
		return false
	}
	var fn *types2.Func
	shift := 0
	switch f := ast.Unparen(stripIndex(c.Fun)).(type) {
	case *ast.Ident:
		fn, _ = a.uses[f].(*types2.Func)
	case *ast.SelectorExpr:
		if sel := a.selections[f]; sel != nil {
			switch sel.Kind() {
			case types2.MethodVal:
				if isInterface(sel.Recv()) {
					return false
				}
				fn, _ = sel.Obj().(*types2.Func)
				shift = 1
			case types2.MethodExpr:
				fn, _ = sel.Obj().(*types2.Func)
			}
		} else {
			fn, _ = a.uses[f.Sel].(*types2.Func)
		}
	}
	if fn == nil {
		return false
	}
	sig := fn.Type().(*types2.Signature)
	if sig.Variadic() && idx >= sig.Params().Len()-1 {
		return false
	}
	f := a.funcs[fn.Origin()]
	if f == nil {
		return false
	}
	// The receiver is the first of a method's params; a method expression
	// passes it as the first argument.
	k := idx + shift
	if k >= len(f.params) || f.params[k] == nil || !mp.params[f.params[k]] {
		return false
	}
	mp.deps[v] = compat.Append(mp.deps[v], f.params[k])
	return true
}

// structCopy records a copy of a struct or array value that holds field
// places: each of them is copied out (copyCheck finds the copies).
func (mp *mapPlaces) structCopy(e ast.Expr, T types2.Type, what string) {
	for _, f := range mp.placesIn(T, 0) {
		mp.bad(f, e.Pos(), compat.Concat(what, " `", exprText(e), "`"), false)
	}
}

// placesIn returns the field places that a value of type T holds, through
// fields and array elements.
func (mp *mapPlaces) placesIn(T types2.Type, depth int) []*types2.Var {
	if T == nil || depth > 8 {
		return nil
	}
	if r, ok := mp.inMemo[T]; ok {
		return r
	}
	mp.inMemo[T] = nil
	var r []*types2.Var
	switch u := underlying(T).(type) {
	case *types2.Struct:
		for i := 0; i < u.NumFields(); i++ {
			f := u.Field(i)
			if isMapType(f.Type()) {
				r = compat.Append(r, mp.field(f, T))
				continue
			}
			r = compat.Append(r, mp.placesIn(f.Type(), depth+1)...)
		}
	case *types2.Array:
		r = mp.placesIn(u.Elem(), depth+1)
	}
	mp.inMemo[T] = r
	return r
}

// mapPlaceKey returns the group a map's contents belong to, from the place
// the access reached the map through: a kept field place ("f" and its
// key), a kept package variable ("v" and its name), or "" for the datum of
// the map's type. out is the occurrence that took the map out of a place
// that is not kept, if there is one.
func (a *analyzer) mapPlaceKey(base vpath, id int) (key string, label string, out *mapOut) {
	if base.root.kind == rGlobal && base.path == "" && base.root.v != nil {
		v := base.root.v
		if o := a.mapVarOut[v]; o != nil {
			return "", "", o
		}
		pkg := ""
		if v.Pkg() != nil {
			pkg = v.Pkg().Path()
		}
		return compat.Concat("v", pkg, ".", v.Name()), v.Name(), nil
	}
	if _, field := splitField(base); field {
		ss := steps(base.path)
		name := strings.SliceFrom(ss[len(ss)-1], 1)
		k := fieldKey(name, id)
		if a.mapFieldKept[k] {
			return compat.Concat("f", k), a.mapFieldLabel[k], nil
		}
		return "", "", a.mapFieldOut[k]
	}
	return "", "", nil
}
