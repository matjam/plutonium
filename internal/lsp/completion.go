package lsp

import (
	"slices"
	"strings"

	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

func (s *server) completion(p *TextDocumentPositionParams) (*CompletionList, *rpcError) {
	f, off, rerr := s.fileAt(p.TextDocument.URI, p.Position)
	if f == nil {
		return nil, rerr
	}
	list := &CompletionList{Items: []CompletionItem{}}
	if inCommentOrString(f, off) {
		return list, nil
	}
	start := identBefore(f.src, off)
	prefix := strings.ToLower(string(f.src[start:off]))
	var items []CompletionItem
	if start > 0 && f.src[start-1] == '.' {
		items = selectorCompletions(f, start-1)
	} else {
		items = scopeCompletions(f, f.tok.Pos(off))
	}
	for _, it := range items {
		if fuzzyMatch(prefix, strings.ToLower(it.Label)) {
			list.Items = append(list.Items, it)
		}
	}
	return list, nil
}

// inCommentOrString reports whether off lies inside a comment or a string
// or character literal.
func inCommentOrString(f *srcFile, off int) bool {
	pos := f.tok.Pos(off)
	for _, g := range f.ast.Comments {
		if g.Pos() < pos && pos <= g.End() {
			for _, c := range g.List {
				// A line comment runs to the end of its line.
				if c.Pos() < pos && (pos < c.End() || pos == c.End() && strings.HasPrefix(c.Text, "//")) {
					return true
				}
			}
		}
	}
	if openQuote(f.src, off) {
		return true
	}
	found := false
	ast.Inspect(f.ast, func(n ast.Node) bool {
		if found || n == nil || n.Pos() > pos || n.End() < pos {
			return false
		}
		if lit, ok := n.(*ast.BasicLit); ok && (lit.Kind == token.STRING || lit.Kind == token.CHAR) {
			found = lit.Pos() < pos && pos < lit.End()
		}
		return true
	})
	return found
}

// openQuote reports whether a string or character literal is open at off
// on its line: the literal being typed, which does not parse yet.
func openQuote(src []byte, off int) bool {
	var quote byte
	start := off
	for start > 0 && src[start-1] != '\n' {
		start--
	}
	for i := start; i < off; i++ {
		c := src[i]
		switch {
		case quote != 0 && c == '\\' && quote != '`':
			i++
		case quote != 0 && c == quote:
			quote = 0
		case quote == 0 && (c == '"' || c == '\'' || c == '`'):
			quote = c
		case quote == 0 && c == '/' && i+1 < off && src[i+1] == '/':
			return false // a comment, handled by the caller
		}
	}
	return quote != 0
}

// selectorCompletions completes after the dot at offset dot: the members
// of a package, the members and methods of a type, or the fields and
// methods of a value.
func selectorCompletions(f *srcFile, dot int) []CompletionItem {
	info, cur := f.pkg.info, f.pkg.types
	var x ast.Expr
	ast.Inspect(f.ast, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		if sel, ok := n.(*ast.SelectorExpr); ok && f.tok.Offset(sel.X.End()) <= dot && f.tok.Offset(sel.Sel.Pos()) > dot {
			x = sel.X // the innermost one wins: Inspect visits it last
		}
		return f.tok.Offset(n.Pos()) <= dot && dot <= f.tok.Offset(n.End())
	})
	if x != nil {
		if id, ok := x.(*ast.Ident); ok {
			if pn, ok := info.Uses[id].(*types2.PkgName); ok {
				return packageCompletions(pn.Imported(), cur)
			}
		}
		if tv, ok := info.Types[x]; ok && tv.Type != nil {
			if tv.IsType() {
				return typeCompletions(tv.Type, cur)
			}
			return valueCompletions(tv.Type, cur)
		}
	}
	// The parser may have dropped an incomplete selector; resolve the name
	// before the dot in the scope at the cursor.
	start := identBefore(f.src, dot)
	if start == dot {
		return nil
	}
	name := string(f.src[start:dot])
	scope := scopeAt(f, f.tok.Pos(dot))
	if scope == nil {
		return nil
	}
	_, obj := scope.LookupParent(name, f.tok.Pos(dot))
	switch obj := obj.(type) {
	case *types2.PkgName:
		return packageCompletions(obj.Imported(), cur)
	case *types2.TypeName:
		return typeCompletions(obj.Type(), cur)
	case *types2.Var, *types2.Const:
		return valueCompletions(obj.Type(), cur)
	}
	return nil
}

func packageCompletions(p *types2.Package, cur *types2.Package) []CompletionItem {
	var items []CompletionItem
	scope := p.Scope()
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		if obj.Exported() || p == cur {
			items = append(items, objectItem(obj, cur))
		}
	}
	return items
}

// typeCompletions lists what can follow a type name: enum members and
// method expressions.
func typeCompletions(t types2.Type, cur *types2.Package) []CompletionItem {
	var items []CompletionItem
	if en := types2.EnumOf(t); en != nil {
		for i := range en.Len() {
			m := en.Member(i)
			if m.Exported() || m.Pkg() == cur {
				it := objectItem(m, cur)
				it.Kind = CompletionEnumMember
				items = append(items, it)
			}
		}
	}
	return append(items, methodItems(t, cur, nil)...)
}

// valueCompletions lists the fields and methods of a value of type t.
func valueCompletions(t types2.Type, cur *types2.Package) []CompletionItem {
	t = types2.Unalias(t)
	// owned[T], shared[T] and synced[T] select like *T.
	if w, ok := t.(*types2.Wrapper); ok {
		switch {
		case w.IsPointer():
			t = types2.NewPointer(w.Elem())
		case w.Kind() == types2.WrapRing:
			var items []CompletionItem
			for _, m := range types2.RingMethods {
				items = append(items, CompletionItem{Label: m, Kind: CompletionMethod, Detail: "ring method"})
			}
			return items
		default:
			t = w.Elem()
		}
	}
	seen := map[string]bool{}
	var items []CompletionItem
	base := t
	if p, ok := t.Underlying().(*types2.Pointer); ok {
		base = p.Elem()
	}
	addFields(&items, seen, base, cur, 0)
	return append(items, methodItems(base, cur, seen)...)
}

// addFields lists the fields of a struct or packed type, then those
// promoted from embedded fields, nearest first.
func addFields(items *[]CompletionItem, seen map[string]bool, t types2.Type, cur *types2.Package, depth int) {
	if depth > 4 {
		return
	}
	var embedded []types2.Type
	switch u := t.Underlying().(type) {
	case *types2.Struct:
		for i := range u.NumFields() {
			fld := u.Field(i)
			if fld.Embedded() {
				embedded = append(embedded, fld.Type())
			}
			if !seen[fld.Name()] && (fld.Exported() || fld.Pkg() == cur) {
				seen[fld.Name()] = true
				*items = append(*items, objectItem(fld, cur))
			}
		}
	case *types2.Packed:
		for i := range u.NumFields() {
			fld := u.Field(i).Var()
			if !seen[fld.Name()] && (fld.Exported() || fld.Pkg() == cur) {
				seen[fld.Name()] = true
				*items = append(*items, objectItem(fld, cur))
			}
		}
	}
	for _, e := range embedded {
		if p, ok := e.Underlying().(*types2.Pointer); ok {
			e = p.Elem()
		}
		addFields(items, seen, e, cur, depth+1)
	}
}

// methodItems lists the methods of t and *t.
func methodItems(t types2.Type, cur *types2.Package, seen map[string]bool) []CompletionItem {
	if seen == nil {
		seen = map[string]bool{}
	}
	mt := t
	if _, isPtr := t.Underlying().(*types2.Pointer); !isPtr && !types2.IsInterface(t) {
		mt = types2.NewPointer(t)
	}
	ms := types2.NewMethodSet(mt)
	var items []CompletionItem
	for i := range ms.Len() {
		m := ms.At(i).Obj()
		if seen[m.Name()] || !m.Exported() && m.Pkg() != cur {
			continue
		}
		seen[m.Name()] = true
		items = append(items, objectItem(m, cur))
	}
	return items
}

// scopeAt returns the innermost scope at pos in f.
func scopeAt(f *srcFile, pos token.Pos) *types2.Scope {
	if f.pkg.types == nil {
		return nil
	}
	if s := f.pkg.types.Scope().Innermost(pos); s != nil {
		return s
	}
	return f.pkg.info.Scopes[f.ast]
}

// scopeCompletions lists the names visible at pos, innermost first, and
// the keywords.
func scopeCompletions(f *srcFile, pos token.Pos) []CompletionItem {
	cur := f.pkg.types
	var items []CompletionItem
	seen := map[string]bool{}
	for s := scopeAt(f, pos); s != nil; s = s.Parent() {
		for _, name := range s.Names() {
			if seen[name] {
				continue
			}
			obj := s.Lookup(name)
			if found, _ := s.LookupParent(name, pos); found != s {
				continue // declared after pos
			}
			seen[name] = true
			it := objectItem(obj, cur)
			if s == types2.Universe {
				it.SortText = "2" + name
			} else {
				it.SortText = "1" + name
			}
			items = append(items, it)
		}
	}
	for _, kw := range keywords {
		if !seen[kw] {
			items = append(items, CompletionItem{Label: kw, Kind: CompletionKeyword, SortText: "3" + kw})
		}
	}
	return items
}

// keywords are the language's keywords.
var keywords = func() []string {
	var kws []string
	for tok := token.Token(0); tok < 256; tok++ {
		if tok.IsKeyword() {
			kws = append(kws, tok.String())
		}
	}
	slices.Sort(kws)
	return kws
}()

// objectItem returns the completion item of an object.
func objectItem(obj types2.Object, cur *types2.Package) CompletionItem {
	qf := qualifier(cur)
	it := CompletionItem{Label: obj.Name()}
	switch obj := obj.(type) {
	case *types2.Func:
		it.Kind = CompletionFunction
		if obj.Signature().Recv() != nil {
			it.Kind = CompletionMethod
		}
		it.Detail = types2.TypeString(obj.Type(), qf)
	case *types2.Var:
		it.Kind = CompletionVariable
		if obj.IsField() {
			it.Kind = CompletionField
		}
		it.Detail = types2.TypeString(obj.Type(), qf)
	case *types2.Const:
		it.Kind = CompletionConstant
		if types2.EnumOf(obj.Type()) != nil {
			it.Kind = CompletionEnumMember
		}
		it.Detail = types2.TypeString(obj.Type(), qf)
	case *types2.TypeName:
		it.Kind = CompletionClass
		switch {
		case types2.EnumOf(obj.Type()) != nil:
			it.Kind = CompletionEnum
		case types2.IsInterface(obj.Type()):
			it.Kind = CompletionInterface
		default:
			if _, ok := obj.Type().Underlying().(*types2.Struct); ok {
				it.Kind = CompletionStruct
			}
		}
		if _, ok := obj.Type().(*types2.TypeParam); ok {
			it.Kind = CompletionTypeParam
		}
		if k := typeKind(obj.Type(), 0); k != "" {
			it.Detail = k
		}
	case *types2.PkgName:
		it.Kind = CompletionModule
		it.Detail = obj.Imported().Path()
	case *types2.Builtin:
		it.Kind = CompletionFunction
		it.Detail = "builtin"
	default:
		it.Kind = CompletionVariable
	}
	return it
}
