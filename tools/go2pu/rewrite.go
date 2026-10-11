package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"
	"unicode/utf8"
)

// rewrite returns the translation of n if n itself needs one, and false if
// n translates to its own source with its descendants translated.
func (t *fileTr) rewrite(n ast.Node) (string, bool) {
	if s, ok := t.subst[n]; ok {
		return s, true
	}
	if e, ok := t.scopeEdits[n]; ok {
		return t.scopeRewrite(n, e), true
	}
	switch n := n.(type) {
	case *ast.BasicLit:
		t.checkBinaryString(n)
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
	case *ast.AssignStmt:
		if r := t.orAssign[n]; r != nil {
			return t.orText(r), true
		}
		return t.assign(n)
	case *ast.IfStmt:
		if r, ok := t.orIf[n]; ok {
			if r == nil {
				return "", true // the propagation of an or statement before it
			}
			return t.orText(r), true
		}
	case *ast.RangeStmt:
		t.checkHeapRange(n)
		if _, ok := t.typeOf(n.X).Underlying().(*types.Signature); ok {
			t.note(n, "range over a function: not supported yet (roadmap item 22)")
		}
	case *ast.CallExpr:
		return t.call(n)
	case *ast.TypeSpec:
		return t.ownerSpec(n)
	case *ast.BinaryExpr:
		if n.Op == token.ADD && isString(t.typeOf(n)) && t.info.Types[n].Value == nil {
			var parts []string
			for _, e := range t.concatOperands(n, nil) {
				parts = append(parts, t.render(e))
			}
			return fmt.Sprintf("%s.Concat(%s)", t.compat(), strings.Join(parts, ", ")), true
		}
	case *ast.CompositeLit:
		return t.heapLiteral(n)
	case *ast.UnaryExpr:
		if n.Op == token.AND && t.isHeapUse(n.X) {
			return ast.Unparen(n.X).(*ast.Ident).Name, true // &x of a heap variable is the pointer
		}
		if lit, ok := ast.Unparen(n.X).(*ast.CompositeLit); ok && n.Op == token.AND &&
			t.esc.has(t.fset, n.Pos(), "&", "escapes to heap") {
			return t.puNew(t.render(lit)), true
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
		return "task " + t.render(n.Call), true
	case *ast.ChanType:
		t.note(n, "channel type: channels are planned (roadmap item 15)")
	case *ast.DeferStmt:
		if t.inLoop(n) {
			t.note(n, "defer in a loop: rejected by Plutonium")
		}
	}
	return "", false
}

// checkBinaryString notes a string literal that holds bytes that are not
// valid UTF-8: Go code keeping binary data in a string, which Plutonium
// rejects, since every string holds valid UTF-8 (language spec section
// 18.8). A conversion string(b) of such bytes would replace them with
// U+FFFD, so the data belongs in a []byte.
func (t *fileTr) checkBinaryString(lit *ast.BasicLit) {
	if lit.Kind != token.STRING {
		return
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil || utf8.ValidString(s) {
		return
	}
	t.note(lit, "string literal %s is not valid UTF-8: keep binary data in a []byte", lit.Value)
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
	if p == puPath {
		r, ok := puNames[sel.Sel.Name]
		if !ok {
			t.note(sel, "pu.%s: no Plutonium counterpart", sel.Sel.Name)
			return "", false
		}
		return t.use(r.pkg, r.local) + "." + r.name, true
	}
	if r, ok := special[p+"."+sel.Sel.Name]; ok {
		t.compat()
		return r, true
	}
	if appendPkgs[p] && strings.HasPrefix(sel.Sel.Name, "Append") {
		t.note(sel, "%s.%s: Plutonium's takes a vec.Appender destination, and compat has no form with Go's []byte; hand-fix", p, sel.Sel.Name)
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

// assign handles variables Go moves to the heap, and += on strings. Maps
// translate to Plutonium's map[K]V as they are.
func (t *fileTr) assign(n *ast.AssignStmt) (string, bool) {
	if n.Tok == token.DEFINE {
		if s, ok := t.heapDefine(n); ok {
			return s, true
		}
	}
	if n.Tok == token.ADD_ASSIGN && len(n.Lhs) == 1 && isString(t.typeOf(n.Lhs[0])) {
		l := t.render(n.Lhs[0])
		return fmt.Sprintf("%s = %s.Concat(%s, %s)", l, t.compat(), l, t.render(n.Rhs[0])), true
	}
	return "", false
}

// concatOperands flattens a chain of string additions.
func (t *fileTr) concatOperands(e ast.Expr, out []ast.Expr) []ast.Expr {
	if b, ok := ast.Unparen(e).(*ast.BinaryExpr); ok && b.Op == token.ADD && isString(t.typeOf(b)) && t.info.Types[b].Value == nil {
		out = t.concatOperands(b.X, out)
		return t.concatOperands(b.Y, out)
	}
	return append(out, e)
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
		return t.puNew(t.typeText(T) + t.splice(n)), true
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
	if s, ok := t.heapVariadic(n); ok {
		return s, true
	}
	// x.Drop() on a heap variable: x is a pointer into the phase arena,
	// which never drops what it holds, so the drop goes through the
	// unchecked layer (language spec section 20.1).
	if se, ok := fun.(*ast.SelectorExpr); ok && se.Sel.Name == "Drop" && len(n.Args) == 0 && t.isHeapUse(se.X) {
		return fmt.Sprintf("%s.Drop(%s)", t.use("unsafe", "unsafe"), t.render(se.X)), true
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
			case "errors.New":
				// Plutonium's errors.New takes a static slot, for
				// package-level sentinels; one made at run time is
				// allocated.
				if t.inFunc(n) {
					return fmt.Sprintf("%s.NewError(%s)", t.compat(), t.list(n.Args)), true
				}
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

// puOwners are the names of internal/pu whose Plutonium counterparts are
// owner types (language spec section 18.5).
var puOwners = map[string]bool{"Vec": true, "Map": true, "Set": true, "Buf": true}

// ownerSpec declares a type owner when its values hold library owners: in
// Plutonium a struct with an owner field must say owner (section 6.4).
func (t *fileTr) ownerSpec(n *ast.TypeSpec) (string, bool) {
	if n.Assign.IsValid() {
		return "", false
	}
	obj, ok := t.info.Defs[n.Name].(*types.TypeName)
	if !ok || !holdsOwner(obj.Type(), map[types.Type]bool{}) {
		return "", false
	}
	s := n.Name.Name
	if n.TypeParams != nil {
		s += t.render(n.TypeParams)
	}
	return s + " owner " + t.render(n.Type), true
}

// holdsOwner reports whether values of T hold a library owner, in a field
// or an array element, so that the translated T is an owner too.
func holdsOwner(T types.Type, seen map[types.Type]bool) bool {
	if seen[T] {
		return false
	}
	seen[T] = true
	if n, ok := T.(*types.Named); ok {
		if o := n.Obj(); o.Pkg() != nil && o.Pkg().Path() == puPath && puOwners[o.Name()] {
			return true
		}
	}
	switch u := T.Underlying().(type) {
	case *types.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if holdsOwner(u.Field(i).Type(), seen) {
				return true
			}
		}
	case *types.Array:
		return holdsOwner(u.Elem(), seen)
	}
	return false
}

// heapVariadic translates a call whose variadic arguments Go's escape
// analysis moves to the heap (the callee keeps the slice). In Plutonium
// they would be passed in an array in the caller's frame, which the
// callee may not keep (language spec section 18.3), so the translation
// allocates the slice: f(a, b, c) becomes f(a, compat.Clone([]T{b, c})...).
func (t *fileTr) heapVariadic(n *ast.CallExpr) (string, bool) {
	if n.Ellipsis.IsValid() || !t.esc.has(t.fset, n.Lparen, "... argument", "escapes to heap") {
		return "", false
	}
	sig, ok := t.typeOf(n.Fun).Underlying().(*types.Signature)
	if !ok || !sig.Variadic() {
		return "", false
	}
	fixed := sig.Params().Len() - 1
	if len(n.Args) <= fixed {
		return "", false // no variadic arguments: nothing to allocate
	}
	if s, ok := sig.Params().At(fixed).Type().(*types.Slice); ok && types.IsInterface(s.Elem()) {
		// Copying the slice would not help: its interfaces would still
		// point at the frame copies of the arguments. Go moves those for
		// formatting calls that keep nothing; Plutonium's analysis is the
		// judge of whether such a call keeps them.
		return "", false
	}
	var args []string
	for _, a := range n.Args[:fixed] {
		args = append(args, t.render(a))
	}
	var rest []string
	for _, a := range n.Args[fixed:] {
		rest = append(rest, t.render(a))
	}
	slice := t.typeText(sig.Params().At(fixed).Type())
	args = append(args, fmt.Sprintf("%s.Clone(%s{%s})...", t.compat(), slice, strings.Join(rest, ", ")))
	return fmt.Sprintf("%s(%s)", t.render(n.Fun), strings.Join(args, ", ")), true
}

// conversion translates conversions that copy in Go. string(b) of a byte
// slice becomes compat.String, the lossy form (strings.FromUTF8Lossy): the
// translation is mechanical, and bytes that are not UTF-8 would become
// U+FFFD where Go kept them; code that relies on that keeps a []byte.
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
	case elemBasic(to) == types.Rune && isString(from):
		return fmt.Sprintf("%s.Runes(%s)", t.compat(), arg), true
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
		T := t.typeOf(n)
		if st, ok := T.Underlying().(*types.Slice); ok {
			// Plutonium has no make([]T, ...): compat.Make allocates as
			// Go's does, from the default allocator.
			call := fmt.Sprintf("%s.Make[%s](%s)", t.compat(), t.typeText(st.Elem()), t.intArg(n.Args[1]))
			if len(n.Args) > 2 {
				call = fmt.Sprintf("%s.MakeCap[%s](%s, %s)", t.compat(), t.typeText(st.Elem()), t.intArg(n.Args[1]), t.intArg(n.Args[2]))
			}
			if _, named := T.(*types.Named); named {
				call = fmt.Sprintf("%s(%s)", t.typeText(T), call)
			}
			return call, true
		}
	case "recover":
		t.note(n, "recover: Plutonium's panic does not unwind; restructure the error path")
	case "complex", "real", "imag":
		t.note(n, "%s: Plutonium has no complex numbers", name)
	}
	return "", false
}

// intArg renders a size argument of make as an int: make takes any
// integer type, compat.Make an int.
func (t *fileTr) intArg(e ast.Expr) string {
	if b, ok := t.typeOf(e).(*types.Basic); ok && (b.Kind() == types.Int || b.Info()&types.IsUntyped != 0) {
		return t.render(e)
	}
	return "int(" + t.render(e) + ")"
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
			return "started as a task"
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
	case *types.Struct:
		// A struct that is a single pointer is held in the data word, as in
		// Go (types2.PointerShaped).
		return u.NumFields() == 1 && pointerShaped(u.Field(0).Type())
	case *types.Array:
		return u.Len() == 1 && pointerShaped(u.Elem())
	}
	return false
}

// pointerShaped mirrors types2.PointerShaped for go/types: a pointer, a func,
// unsafe.Pointer, or a struct or array that is exactly one of those.
func pointerShaped(T types.Type) bool {
	switch u := T.Underlying().(type) {
	case *types.Pointer, *types.Signature, *types.Map, *types.Chan:
		return true
	case *types.Basic:
		return u.Kind() == types.UnsafePointer
	case *types.Struct:
		return u.NumFields() == 1 && pointerShaped(u.Field(0).Type())
	case *types.Array:
		return u.Len() == 1 && pointerShaped(u.Elem())
	}
	return false
}

// inFunc reports whether n is inside a function body (rather than a
// package-level initializer).
func (t *fileTr) inFunc(n ast.Node) bool {
	for p := t.parent[n]; p != nil; p = t.parent[p] {
		switch p.(type) {
		case *ast.FuncLit, *ast.FuncDecl:
			return true
		}
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
		return fmt.Sprintf("map[%s]%s", t.typeText(T.Key()), t.typeText(T.Elem()))
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
