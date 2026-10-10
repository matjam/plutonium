package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

// rewrite returns the translation of n if n itself needs one, and false if
// n translates to its own source with its descendants translated.
func (t *fileTr) rewrite(n ast.Node) (string, bool) {
	if s, ok := t.subst[n]; ok {
		return s, true
	}
	switch n := n.(type) {
	case *ast.Ident:
		if t.isHeapUse(n) {
			if sel, ok := t.parent[n].(*ast.SelectorExpr); ok && sel.X == n {
				return n.Name, true // x.f and x.m() dereference the pointer themselves
			}
			return "(*" + n.Name + ")", true
		}
	case *ast.FuncDecl:
		return t.funcWithHeapParams(n, n.Type, n.Recv, n.Body)
	case *ast.GenDecl:
		if s, ok := t.heapVarDecl(n); ok {
			return s, true
		}
		if n.Tok == token.IMPORT {
			for _, d := range t.file.Decls {
				if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.IMPORT {
					if g == n {
						return importMarker, true
					}
					break
				}
			}
			return "", true
		}
	case *ast.SelectorExpr:
		return t.qualified(n)
	case *ast.MapType:
		return fmt.Sprintf("*%s.Map[%s, %s]", t.use(hashPkg, "hash"), t.render(n.Key), t.render(n.Value)), true
	case *ast.IndexExpr:
		if t.isMapValue(n.X) {
			return fmt.Sprintf("%s.MapAt(%s, %s)", t.compat(), t.render(n.X), t.mapKey(n.X, n.Index)), true
		}
	case *ast.AssignStmt:
		return t.assign(n)
	case *ast.ValueSpec:
		return t.valueSpec(n)
	case *ast.IncDecStmt:
		if idx, ok := ast.Unparen(n.X).(*ast.IndexExpr); ok && t.isMapValue(idx.X) {
			return t.mapSlot(idx) + n.Tok.String(), true
		}
	case *ast.RangeStmt:
		t.checkHeapRange(n)
		if t.isMapValue(n.X) {
			return t.rangeMap(n, ""), true
		}
		if _, ok := t.typeOf(n.X).Underlying().(*types.Signature); ok {
			t.note(n, "range over a function: not supported yet (roadmap item 22)")
		}
	case *ast.LabeledStmt:
		if r, ok := n.Stmt.(*ast.RangeStmt); ok && t.isMapValue(r.X) {
			return t.rangeMap(r, n.Label.Name), true
		}
	case *ast.CallExpr:
		return t.call(n)
	case *ast.BinaryExpr:
		if n.Op == token.ADD && isString(t.typeOf(n)) && t.info.Types[n].Value == nil {
			var parts []string
			for _, e := range t.concatOperands(n, nil) {
				parts = append(parts, t.render(e))
			}
			return fmt.Sprintf("%s.Concat(%s)", t.compat(), strings.Join(parts, ", ")), true
		}
	case *ast.CompositeLit:
		if s, ok := t.mapLiteral(n); ok {
			return s, true
		}
		return t.heapLiteral(n)
	case *ast.UnaryExpr:
		if n.Op == token.AND && t.isHeapUse(n.X) {
			return ast.Unparen(n.X).(*ast.Ident).Name, true // &x of a heap variable is the pointer
		}
		if lit, ok := ast.Unparen(n.X).(*ast.CompositeLit); ok && n.Op == token.AND &&
			t.esc.has(t.fset, n.Pos(), "&", "escapes to heap") {
			return fmt.Sprintf("%s.New(%s)", t.compat(), t.render(lit)), true
		}
	case *ast.FuncLit:
		why := t.closureEscape(n)
		s, ok := t.funcWithHeapParams(n, n.Type, nil, n.Body)
		if why == "" {
			return s, ok
		}
		t.note(n, "closure escapes its frame (%s): hand-fix", why)
		if !ok {
			s = t.splice(n)
		}
		return fmt.Sprintf("/* go2pu: closure escapes its frame (%s) */ %s", why, s), true
	case *ast.ReturnStmt:
		t.checkReturnBoxes(n)
	case *ast.GoStmt:
		t.note(n, "go statement: puroutines are planned (roadmap item 14)")
	case *ast.ChanType:
		t.note(n, "channel type: channels are planned (roadmap item 15)")
	case *ast.DeferStmt:
		if t.inLoop(n) {
			t.note(n, "defer in a loop: rejected by Plutonium")
		}
	}
	return "", false
}

// qualified handles a reference to a name of an imported package.
func (t *fileTr) qualified(sel *ast.SelectorExpr) (string, bool) {
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	pn, ok := t.info.Uses[id].(*types.PkgName)
	if !ok {
		return "", false
	}
	p := pn.Imported().Path()
	if r, ok := special[p+"."+sel.Sel.Name]; ok {
		t.compat()
		return r, true
	}
	to := t.mapPath(p)
	if to == "" {
		t.note(sel, "%s.%s: no Plutonium translation", p, sel.Sel.Name)
		return "", false
	}
	t.use(to, id.Name)
	if ex := t.exports[to]; strings.HasPrefix(to, "pluto/") && ex != nil && !ex[sel.Sel.Name] {
		t.note(sel, "%s has no %s", to, sel.Sel.Name)
	}
	return "", false
}

// assign handles assignments to and comma-ok reads of map elements, and
// += on strings.
func (t *fileTr) assign(n *ast.AssignStmt) (string, bool) {
	if n.Tok == token.DEFINE {
		if s, ok := t.heapDefine(n); ok {
			return s, true
		}
	}
	if len(n.Lhs) == 2 && len(n.Rhs) == 1 {
		if idx, ok := ast.Unparen(n.Rhs[0]).(*ast.IndexExpr); ok && t.isMapValue(idx.X) {
			return fmt.Sprintf("%s, %s %s %s.MapGet(%s, %s)", t.render(n.Lhs[0]), t.render(n.Lhs[1]), n.Tok,
				t.compat(), t.render(idx.X), t.mapKey(idx.X, idx.Index)), true
		}
	}
	if n.Tok == token.ADD_ASSIGN && len(n.Lhs) == 1 && isString(t.typeOf(n.Lhs[0])) {
		l := t.renderLHS(n.Lhs[0])
		return fmt.Sprintf("%s = %s.Concat(%s, %s)", l, t.compat(), t.render(n.Lhs[0]), t.render(n.Rhs[0])), true
	}
	hasMap := false
	for _, l := range n.Lhs {
		if idx, ok := ast.Unparen(l).(*ast.IndexExpr); ok && t.isMapValue(idx.X) {
			hasMap = true
		}
	}
	if !hasMap {
		return "", false
	}
	if len(n.Lhs) == 1 && n.Tok == token.ASSIGN {
		idx := ast.Unparen(n.Lhs[0]).(*ast.IndexExpr)
		return fmt.Sprintf("%s.Set(%s, %s)", t.render(idx.X), t.mapKey(idx.X, idx.Index), t.render(n.Rhs[0])), true
	}
	lhs := make([]string, len(n.Lhs))
	for i, l := range n.Lhs {
		lhs[i] = t.renderLHS(l)
	}
	return fmt.Sprintf("%s %s %s", strings.Join(lhs, ", "), n.Tok, t.list(n.Rhs)), true
}

// renderLHS renders an assignment target; a map element becomes the slot
// that holds it.
func (t *fileTr) renderLHS(e ast.Expr) string {
	if idx, ok := ast.Unparen(e).(*ast.IndexExpr); ok && t.isMapValue(idx.X) {
		return t.mapSlot(idx)
	}
	return t.render(e)
}

func (t *fileTr) mapSlot(idx *ast.IndexExpr) string {
	return fmt.Sprintf("*%s.MapSlot(%s, %s)", t.compat(), t.render(idx.X), t.mapKey(idx.X, idx.Index))
}

// mapKey renders a key of map m, converted to the key type when it has
// another type: Go converts it implicitly, but as an argument of a generic
// compat function it would conflict with the key type inferred from m.
func (t *fileTr) mapKey(m, k ast.Expr) string {
	s := t.render(k)
	mt, ok := t.typeOf(m).Underlying().(*types.Map)
	kt := t.typeOf(k)
	if !ok || kt == nil || types.Identical(kt, mt.Key()) {
		return s
	}
	if b, ok := kt.(*types.Basic); ok && b.Info()&types.IsUntyped != 0 {
		return s
	}
	return t.typeText(mt.Key()) + "(" + s + ")"
}

// valueSpec handles var v, ok = m[k].
func (t *fileTr) valueSpec(n *ast.ValueSpec) (string, bool) {
	if len(n.Names) != 2 || len(n.Values) != 1 {
		return "", false
	}
	idx, ok := ast.Unparen(n.Values[0]).(*ast.IndexExpr)
	if !ok || !t.isMapValue(idx.X) {
		return "", false
	}
	typ := ""
	if n.Type != nil {
		typ = " " + t.render(n.Type)
	}
	return fmt.Sprintf("%s, %s%s = %s.MapGet(%s, %s)", n.Names[0].Name, n.Names[1].Name, typ, t.compat(),
		t.render(idx.X), t.mapKey(idx.X, idx.Index)), true
}

// rangeMap translates a range over a map into a loop over its iterator.
func (t *fileTr) rangeMap(r *ast.RangeStmt, label string) string {
	it, ok := t.fresh("goIt"), t.fresh("goOk")
	key, val := "_", "_"
	if r.Key != nil {
		key = t.render(r.Key)
	}
	if r.Value != nil {
		val = t.render(r.Value)
	}
	t.checkMapModified(r)
	var b strings.Builder
	fmt.Fprintf(&b, "{\n%s := %s.MapIter(%s)\n", it, t.compat(), t.render(r.X))
	def := ":="
	if r.Tok == token.ASSIGN {
		fmt.Fprintf(&b, "var %s bool\n", ok)
		def = "="
	}
	if label != "" {
		fmt.Fprintf(&b, "%s:\n", label)
	}
	fmt.Fprintf(&b, "for %s, %s, %s %s %s.Next(); %s; %s, %s, %s = %s.Next() %s\n}",
		key, val, ok, def, it, ok, key, val, ok, it, t.render(r.Body))
	return b.String()
}

// checkMapModified notes a range over a map whose body stores into or
// deletes from the same map, which Go allows and hash.Map does not.
func (t *fileTr) checkMapModified(r *ast.RangeStmt) {
	m := t.text(r.X)
	ast.Inspect(r.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			for _, l := range n.Lhs {
				if idx, ok := ast.Unparen(l).(*ast.IndexExpr); ok && t.isMapValue(idx.X) && t.text(idx.X) == m {
					t.note(n, "map %s modified while ranging over it: hash.Map forbids this", m)
				}
			}
		case *ast.CallExpr:
			if id, ok := n.Fun.(*ast.Ident); ok && id.Name == "delete" && len(n.Args) > 0 && t.text(n.Args[0]) == m {
				t.note(n, "map %s modified while ranging over it: hash.Map forbids this", m)
			}
		}
		return true
	})
}

// concatOperands flattens a chain of string additions.
func (t *fileTr) concatOperands(e ast.Expr, out []ast.Expr) []ast.Expr {
	if b, ok := ast.Unparen(e).(*ast.BinaryExpr); ok && b.Op == token.ADD && isString(t.typeOf(b)) && t.info.Types[b].Value == nil {
		out = t.concatOperands(b.X, out)
		return t.concatOperands(b.Y, out)
	}
	return append(out, e)
}

// mapLiteral translates a map composite literal.
func (t *fileTr) mapLiteral(n *ast.CompositeLit) (string, bool) {
	T := t.typeOf(n)
	if T == nil {
		return "", false
	}
	mt, ok := T.Underlying().(*types.Map)
	if !ok {
		return "", false
	}
	k, v := t.typeText(mt.Key()), t.typeText(mt.Elem())
	c := t.compat()
	if len(n.Elts) == 0 {
		return fmt.Sprintf("%s.NewMap[%s, %s](0)", c, k, v), true
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s.MapOf[%s, %s](\n", c, k, v)
	for _, e := range n.Elts {
		kv, ok := e.(*ast.KeyValueExpr)
		if !ok {
			t.note(e, "map literal element without a key")
			continue
		}
		fmt.Fprintf(&b, "%s.Pair[%s, %s]{%s, %s},\n", c, k, v, t.elided(kv.Key, mt.Key()), t.elided(kv.Value, mt.Elem()))
	}
	b.WriteString(")")
	return b.String(), true
}

// heapLiteral allocates the composite literals Go's escape analysis moves
// to the heap: slice literals, and &T{...} elided inside another literal.
// (An explicit &T{...} is handled at the & operator.)
func (t *fileTr) heapLiteral(n *ast.CompositeLit) (string, bool) {
	T := t.typeOf(n)
	if T == nil {
		return "", false
	}
	if n.Type == nil && t.esc.has(t.fset, n.Pos(), "&", "escapes to heap") {
		if p, ok := T.Underlying().(*types.Pointer); ok {
			T = p.Elem()
		}
		return fmt.Sprintf("%s.New(%s%s)", t.compat(), t.typeText(T), t.splice(n)), true
	}
	// Go reports a slice literal at its opening brace.
	if _, ok := T.Underlying().(*types.Slice); ok && t.esc.has(t.fset, n.Lbrace, "[]", "escapes to heap") {
		typ := ""
		if n.Type == nil {
			typ = t.typeText(T)
		}
		return fmt.Sprintf("%s.Clone(%s%s)", t.compat(), typ, t.splice(n)), true
	}
	return "", false
}

// elided renders a map literal key or value, giving an elided composite
// literal its type, since it moves into a Pair literal where Go and
// Plutonium do not allow elision.
func (t *fileTr) elided(e ast.Expr, T types.Type) string {
	lit, ok := e.(*ast.CompositeLit)
	if !ok || lit.Type != nil {
		return t.render(e)
	}
	if s, ok := t.rewrite(lit); ok {
		return s // a map literal, or one that allocates
	}
	if p, ok := T.(*types.Pointer); ok {
		return "&" + t.typeText(p.Elem()) + t.splice(lit)
	}
	return t.typeText(T) + t.splice(lit)
}

// call translates builtins and library calls that need it.
func (t *fileTr) call(n *ast.CallExpr) (string, bool) {
	fun := ast.Unparen(n.Fun)
	if tv, ok := t.info.Types[n.Fun]; ok && tv.IsType() {
		return t.conversion(n, tv.Type)
	}
	if id, ok := fun.(*ast.Ident); ok {
		if b, ok := t.info.Uses[id].(*types.Builtin); ok {
			return t.builtin(n, b.Name())
		}
	}
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	if id, ok := sel.X.(*ast.Ident); ok {
		if pn, ok := t.info.Uses[id].(*types.PkgName); ok {
			switch pn.Imported().Path() + "." + sel.Sel.Name {
			case "sort.Slice":
				return t.sortSlice(n, "SortFunc")
			case "sort.SliceStable":
				return t.sortSlice(n, "SortStableFunc")
			case "encoding/binary.Write":
				return t.binaryWrite(n)
			}
		}
	}
	// binary.LittleEndian.PutUintNN(b, v)
	if inner, ok := sel.X.(*ast.SelectorExpr); ok {
		if id, ok := inner.X.(*ast.Ident); ok {
			if pn, ok := t.info.Uses[id].(*types.PkgName); ok && pn.Imported().Path() == "encoding/binary" {
				if inner.Sel.Name == "LittleEndian" && strings.HasPrefix(sel.Sel.Name, "PutUint") {
					return fmt.Sprintf("%s.%s(%s)", t.compat(), sel.Sel.Name, t.list(n.Args)), true
				}
				t.note(n, "encoding/binary.%s.%s: no translation", inner.Sel.Name, sel.Sel.Name)
			}
		}
	}
	return "", false
}

// conversion translates conversions that copy in Go.
func (t *fileTr) conversion(n *ast.CallExpr, to types.Type) (string, bool) {
	if len(n.Args) != 1 || t.info.Types[n].Value != nil {
		return "", false
	}
	from := t.typeOf(n.Args[0])
	if from == nil {
		return "", false
	}
	arg := t.render(n.Args[0])
	switch {
	case isString(to) && elemBasic(from) == types.Byte:
		return fmt.Sprintf("%s.String(%s)", t.compat(), arg), true
	case isString(to) && elemBasic(from) == types.Rune, elemBasic(to) == types.Rune && isString(from):
		t.note(n, "conversion between string and []rune: hand-fix")
	case elemBasic(to) == types.Byte && isString(from):
		return fmt.Sprintf("%s.Bytes(%s)", t.compat(), arg), true
	case isString(to) && !isString(from):
		if b, ok := from.Underlying().(*types.Basic); ok && b.Info()&types.IsInteger != 0 {
			return fmt.Sprintf("%s.RuneString(rune(%s))", t.compat(), arg), true
		}
	}
	return "", false
}

// builtin translates builtins whose meaning changes in Plutonium.
func (t *fileTr) builtin(n *ast.CallExpr, name string) (string, bool) {
	switch name {
	case "append":
		if len(n.Args) == 2 && n.Ellipsis.IsValid() && isString(t.typeOf(n.Args[1])) {
			return fmt.Sprintf("%s.AppendString(%s)", t.compat(), t.list(n.Args)), true
		}
		dots := ""
		if n.Ellipsis.IsValid() {
			dots = "..."
		}
		return fmt.Sprintf("%s.Append(%s%s)", t.compat(), t.list(n.Args), dots), true
	case "make":
		if mt, ok := t.typeOf(n).Underlying().(*types.Map); ok {
			size := "0"
			if len(n.Args) > 1 {
				size = t.render(n.Args[1])
			}
			return fmt.Sprintf("%s.NewMap[%s, %s](%s)", t.compat(), t.typeText(mt.Key()), t.typeText(mt.Elem()), size), true
		}
	case "len", "delete", "clear":
		if len(n.Args) > 0 && t.isMapValue(n.Args[0]) {
			fn := map[string]string{"len": "MapLen", "delete": "MapDelete", "clear": "MapClear"}[name]
			args := t.render(n.Args[0])
			if len(n.Args) == 2 {
				args += ", " + t.mapKey(n.Args[0], n.Args[1])
			}
			return fmt.Sprintf("%s.%s(%s)", t.compat(), fn, args), true
		}
	case "recover":
		t.note(n, "recover: Plutonium's panic does not unwind; restructure the error path")
	case "complex", "real", "imag":
		t.note(n, "%s: Plutonium has no complex numbers", name)
	}
	return "", false
}

// sortSlice turns sort.Slice(x, func(i, j int) bool { ... x[i] ... x[j] ... })
// into sort.SortFunc(x, func(a, b T) bool { ... a ... b ... }) when the
// comparison uses its indexes only to index x. Both use the same pdqsort
// (and the same stable merge for the stable forms), so ties come out in the
// same order as in Go.
func (t *fileTr) sortSlice(n *ast.CallExpr, fn string) (string, bool) {
	if len(n.Args) != 2 {
		return "", false
	}
	lit, ok := n.Args[1].(*ast.FuncLit)
	st, isSlice := t.typeOf(n.Args[0]).Underlying().(*types.Slice)
	if !ok || !isSlice {
		t.note(n, "sort.Slice without a literal comparison: hand-fix")
		return "", false
	}
	var params []*ast.Ident
	for _, f := range lit.Type.Params.List {
		params = append(params, f.Names...)
	}
	if len(params) != 2 {
		return "", false
	}
	pi, pj := t.info.Defs[params[0]], t.info.Defs[params[1]]
	x := t.text(n.Args[0])
	a, b := "a", "b"
	found := map[*ast.IndexExpr]string{}
	clean := true
	ast.Inspect(lit.Body, func(c ast.Node) bool {
		switch c := c.(type) {
		case *ast.IndexExpr:
			if id, ok := c.Index.(*ast.Ident); ok && t.text(c.X) == x {
				switch t.info.Uses[id] {
				case pi:
					found[c] = "a"
					return false
				case pj:
					found[c] = "b"
					return false
				}
			}
		case *ast.Ident:
			if obj := t.info.Uses[c]; obj == pi || obj == pj {
				clean = false
			}
			if c.Name == "a" || c.Name == "b" {
				a, b = "goA", "goB"
			}
		}
		return true
	})
	if !clean {
		t.note(n, "sort comparison uses its indexes other than to index the slice: hand-fix")
		return "", false
	}
	for idx, which := range found {
		if which == "a" {
			t.subst[idx] = a
		} else {
			t.subst[idx] = b
		}
	}
	body := t.render(lit.Body)
	for idx := range found {
		delete(t.subst, idx)
	}
	sortName := t.use(sortPkg, "sort")
	return fmt.Sprintf("%s.%s(%s, func(%s, %s %s) bool %s)", sortName, fn, t.render(n.Args[0]), a, b, t.typeText(st.Elem()), body), true
}

// binaryWrite turns binary.Write(w, binary.LittleEndian, x) of a struct of
// fixed-size fields into a call of a generated encoder.
func (t *fileTr) binaryWrite(n *ast.CallExpr) (string, bool) {
	if len(n.Args) != 3 {
		return "", false
	}
	order, ok := n.Args[1].(*ast.SelectorExpr)
	if !ok || order.Sel.Name != "LittleEndian" {
		t.note(n, "binary.Write in an order other than LittleEndian: hand-fix")
		return "", false
	}
	T := t.typeOf(n.Args[2])
	base := T
	suffix := ""
	if p, ok := T.(*types.Pointer); ok {
		base, suffix = p.Elem(), "Ptr"
	}
	named, ok := types.Unalias(base).(*types.Named)
	st, isStruct := base.Underlying().(*types.Struct)
	if !ok || !isStruct {
		t.note(n, "binary.Write of %s: only named structs are translated", T)
		return "", false
	}
	name := "goWriteLE" + named.Obj().Name() + suffix
	if !t.gen[name] {
		body, size, err := t.encoder(st)
		if err != nil {
			t.note(n, "binary.Write of %s: %v", T, err)
			return "", false
		}
		t.gen[name] = true
		t.extra = append(t.extra, fmt.Sprintf(
			"// %s encodes x as encoding/binary.Write does, little-endian.\nfunc %s(w %s.Writer, x %s) error {\n\tvar b [%d]byte\n%s\t_, err := w.Write(b[:])\n\treturn err\n}\n",
			name, name, t.use(ioPkg, "io"), t.typeText(T), size, body))
	}
	return fmt.Sprintf("%s(%s, %s)", name, t.render(n.Args[0]), t.render(n.Args[2])), true
}

var sizes = types.SizesFor("gc", "amd64")

// encoder returns the statements that store the fields of st into b.
func (t *fileTr) encoder(st *types.Struct) (string, int64, error) {
	var b strings.Builder
	var off int64
	put := func(expr string, k *types.Basic, at string) error {
		switch sz := sizes.Sizeof(k); {
		case k.Info()&types.IsInteger == 0:
			return fmt.Errorf("field of type %s", k)
		case sz == 1:
			fmt.Fprintf(&b, "\tb[%s] = byte(%s)\n", at, expr)
		default:
			fmt.Fprintf(&b, "\t%s.PutUint%d(b[%s:], uint%d(%s))\n", t.compat(), sz*8, at, sz*8, expr)
		}
		return nil
	}
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		switch ft := f.Type().Underlying().(type) {
		case *types.Basic:
			if err := put("x."+f.Name(), ft, fmt.Sprint(off)); err != nil {
				return "", 0, err
			}
		case *types.Array:
			eb, ok := ft.Elem().Underlying().(*types.Basic)
			if !ok {
				return "", 0, fmt.Errorf("field %s of type %s", f.Name(), ft)
			}
			if sizes.Sizeof(eb) == 1 {
				fmt.Fprintf(&b, "\tcopy(b[%d:], x.%s[:])\n", off, f.Name())
			} else {
				fmt.Fprintf(&b, "\tfor i, e := range x.%s {\n", f.Name())
				if err := put("e", eb, fmt.Sprintf("%d+i*%d", off, sizes.Sizeof(eb))); err != nil {
					return "", 0, err
				}
				b.WriteString("\t}\n")
			}
		default:
			return "", 0, fmt.Errorf("field %s of type %s", f.Name(), ft)
		}
		off += sizes.Sizeof(f.Type())
	}
	return b.String(), off, nil
}

// closureEscape returns why a capturing function literal outlives its
// frame, or "" if it does not. Go's escape analysis decides; the context
// only gives the reason.
func (t *fileTr) closureEscape(lit *ast.FuncLit) string {
	if !t.captures(lit) || !t.esc.has(t.fset, lit.Pos(), "func literal escapes", "") {
		return ""
	}
	if why := t.escapeContext(lit); why != "" {
		return why
	}
	return "passed to a function that keeps it"
}

// escapeContext describes how the context of lit makes it outlive its
// frame, as far as one level of context shows.
func (t *fileTr) escapeContext(lit *ast.FuncLit) string {
	var e ast.Node = lit
	for {
		p := t.parent[e]
		switch p := p.(type) {
		case *ast.ParenExpr:
			e = p
			continue
		case *ast.ReturnStmt:
			return "returned"
		case *ast.GoStmt:
			return "started as a puroutine"
		case *ast.KeyValueExpr, *ast.CompositeLit:
			return "stored in a composite literal"
		case *ast.CallExpr:
			if id, ok := p.Fun.(*ast.Ident); ok && id.Name == "append" {
				return "appended to a slice"
			}
		case *ast.AssignStmt:
			for i, r := range p.Rhs {
				if r != e || i >= len(p.Lhs) {
					continue
				}
				switch l := ast.Unparen(p.Lhs[i]).(type) {
				case *ast.SelectorExpr:
					return "stored in a field"
				case *ast.IndexExpr:
					return "stored in an element"
				case *ast.StarExpr:
					return "stored through a pointer"
				case *ast.Ident:
					if obj := t.info.ObjectOf(l); obj != nil && obj.Parent() == t.pkg.Scope() {
						return "stored in a package variable"
					}
				}
			}
		}
		return ""
	}
}

// captures reports whether lit refers to a local variable of an enclosing
// function.
func (t *fileTr) captures(lit *ast.FuncLit) bool {
	found := false
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || found {
			return !found
		}
		v, ok := t.info.Uses[id].(*types.Var)
		if !ok || v.IsField() || v.Pkg() == nil || v.Parent() == v.Pkg().Scope() {
			return true
		}
		if v.Pos() < lit.Pos() || v.Pos() > lit.End() {
			found = true
		}
		return true
	})
	return found
}

// checkReturnBoxes notes returned interface values that hold a value too
// large for the interface's data word: Plutonium boxes those in the frame
// that is returning.
func (t *fileTr) checkReturnBoxes(r *ast.ReturnStmt) {
	var sig *types.Signature
	for p := t.parent[r]; p != nil && sig == nil; p = t.parent[p] {
		switch f := p.(type) {
		case *ast.FuncLit:
			sig, _ = t.typeOf(f).(*types.Signature)
		case *ast.FuncDecl:
			if obj := t.info.Defs[f.Name]; obj != nil {
				sig, _ = obj.Type().(*types.Signature)
			}
		}
	}
	if sig == nil || sig.Results().Len() != len(r.Results) {
		return
	}
	for i, e := range r.Results {
		want := sig.Results().At(i).Type()
		if _, ok := want.Underlying().(*types.Interface); !ok {
			continue
		}
		tv := t.info.Types[e]
		if tv.Type == nil || tv.IsNil() || direct(tv.Type) {
			continue
		}
		t.note(e, "returns a %s in an interface: Plutonium boxes it in the returning frame; return a pointer", tv.Type)
	}
}

// direct reports whether a value of T fits an interface's data word in
// Plutonium (see internal/lower isDirect).
func direct(T types.Type) bool {
	switch u := T.Underlying().(type) {
	case *types.Basic:
		return u.Info()&types.IsString == 0
	case *types.Pointer, *types.Signature, *types.Interface, *types.Map, *types.Chan:
		return true
	}
	return false
}

// inLoop reports whether n is inside a loop of its function.
func (t *fileTr) inLoop(n ast.Node) bool {
	for p := t.parent[n]; p != nil; p = t.parent[p] {
		switch p.(type) {
		case *ast.ForStmt, *ast.RangeStmt:
			return true
		case *ast.FuncLit, *ast.FuncDecl:
			return false
		}
	}
	return false
}

// typeText writes T as Plutonium source in this file.
func (t *fileTr) typeText(T types.Type) string {
	switch T := T.(type) {
	case *types.Alias:
		if T.Obj().Pkg() == nil {
			return T.Obj().Name() // any
		}
		return t.namedText(T.Obj(), nil)
	case *types.Basic:
		return T.Name()
	case *types.Pointer:
		return "*" + t.typeText(T.Elem())
	case *types.Slice:
		return "[]" + t.typeText(T.Elem())
	case *types.Array:
		return fmt.Sprintf("[%d]%s", T.Len(), t.typeText(T.Elem()))
	case *types.Map:
		return fmt.Sprintf("*%s.Map[%s, %s]", t.use(hashPkg, "hash"), t.typeText(T.Key()), t.typeText(T.Elem()))
	case *types.Named:
		return t.namedText(T.Obj(), T.TypeArgs())
	case *types.TypeParam:
		return T.Obj().Name()
	case *types.Interface:
		if T.Empty() {
			return "any"
		}
	case *types.Signature:
		var b strings.Builder
		b.WriteString("func(")
		for i := 0; i < T.Params().Len(); i++ {
			if i > 0 {
				b.WriteString(", ")
			}
			pt := T.Params().At(i).Type()
			if T.Variadic() && i == T.Params().Len()-1 {
				b.WriteString("..." + t.typeText(pt.(*types.Slice).Elem()))
				continue
			}
			b.WriteString(t.typeText(pt))
		}
		b.WriteString(")")
		switch T.Results().Len() {
		case 0:
		case 1:
			b.WriteString(" " + t.typeText(T.Results().At(0).Type()))
		default:
			b.WriteString(" (")
			for i := 0; i < T.Results().Len(); i++ {
				if i > 0 {
					b.WriteString(", ")
				}
				b.WriteString(t.typeText(T.Results().At(i).Type()))
			}
			b.WriteString(")")
		}
		return b.String()
	}
	return types.TypeString(T, func(p *types.Package) string { return p.Name() })
}

func (t *fileTr) namedText(obj *types.TypeName, targs *types.TypeList) string {
	name := obj.Name()
	if p := obj.Pkg(); p != nil && p != t.pkg {
		if r, ok := special[p.Path()+"."+name]; ok {
			t.compat()
			return r
		}
		local, ok := t.local[p.Path()]
		if !ok {
			local = p.Name()
		}
		if to := t.mapPath(p.Path()); to != "" {
			t.use(to, local)
		}
		name = local + "." + name
	}
	if targs != nil && targs.Len() > 0 {
		var args []string
		for i := 0; i < targs.Len(); i++ {
			args = append(args, t.typeText(targs.At(i)))
		}
		name += "[" + strings.Join(args, ", ") + "]"
	}
	return name
}
