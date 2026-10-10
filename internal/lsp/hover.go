package lsp

import (
	"fmt"
	"strings"

	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/types2"
)

// codeLang is the language of code blocks in hover text: the extension's
// language identifier, which gives them its highlighting.
const codeLang = "plutonium"

func (s *server) hover(p *TextDocumentPositionParams) (*Hover, *rpcError) {
	f, off, rerr := s.fileAt(p.TextDocument.URI, p.Position)
	if f == nil {
		return nil, rerr
	}
	if h := directiveHover(f, off); h != nil {
		return h, nil
	}
	id, obj := objectAt(f, off)
	if obj == nil {
		return nil, nil
	}
	var b strings.Builder
	s.describe(&b, f.pkg.types, obj)
	h := &Hover{Contents: MarkupContent{Kind: "markdown", Value: b.String()}}
	if id != nil {
		start := f.tok.Offset(id.Pos())
		r := f.rangeOf(start, start+len(id.Name))
		h.Range = &r
	}
	return h, nil
}

// describe writes the markdown description of an object: its declaration,
// its kind under the ownership rules, enum members, directives and doc
// comment.
func (s *server) describe(b *strings.Builder, cur *types2.Package, obj types2.Object) {
	qf := qualifier(cur)
	fmt.Fprintf(b, "```%s\n%s\n```\n", codeLang, declString(obj, qf))

	switch obj := obj.(type) {
	case *types2.TypeName:
		if line := kindLine(obj.Type()); line != "" {
			fmt.Fprintf(b, "\n%s\n", line)
		}
	case *types2.Var:
		if line := kindLine(obj.Type()); line != "" {
			fmt.Fprintf(b, "\n%s\n", line)
		}
	}

	doc := s.ws.docOf(obj)
	if pn, ok := obj.(*types2.PkgName); ok {
		doc = s.ws.packageDoc(pn.Imported())
	}
	if doc != nil {
		var dirs []string
		for _, c := range doc.List {
			if name, ok := directiveName(c.Text); ok {
				d := "`" + strings.TrimSpace(c.Text) + "`"
				if meaning := directives[name]; meaning != "" {
					d += ": " + meaning
				}
				dirs = append(dirs, d)
			}
		}
		if text := strings.TrimSpace(doc.Text()); text != "" {
			fmt.Fprintf(b, "\n%s\n", text)
		}
		if len(dirs) > 0 {
			fmt.Fprintf(b, "\n%s\n", strings.Join(dirs, "  \n"))
		}
	}
}

// declString returns the declaration of an object as it would be written,
// with an enum's members.
func declString(obj types2.Object, qf types2.Qualifier) string {
	switch obj := obj.(type) {
	case *types2.TypeName:
		if en := types2.EnumOf(obj.Type()); en != nil {
			var b strings.Builder
			fmt.Fprintf(&b, "type %s enum %s {\n", obj.Name(), types2.TypeString(obj.Type().Underlying(), qf))
			width := 0
			for i := range en.Len() {
				width = max(width, len(en.Member(i).Name()))
			}
			for i := range en.Len() {
				m := en.Member(i)
				fmt.Fprintf(&b, "\t%-*s = %s\n", width, m.Name(), m.Val().ExactString())
			}
			b.WriteString("}")
			return b.String()
		}
	case *types2.Const:
		if en := types2.EnumOf(obj.Type()); en != nil && en.Obj() != nil {
			for i := range en.Len() {
				if en.Member(i) == obj {
					return fmt.Sprintf("const %s.%s %s = %s", en.Obj().Name(), obj.Name(), types2.TypeString(obj.Type(), qf), obj.Val().ExactString())
				}
			}
		}
	case *types2.PkgName:
		return fmt.Sprintf("package %s (%q)", obj.Imported().Name(), obj.Imported().Path())
	}
	return types2.ObjectString(obj, qf)
}

// The kinds of section 6 of the specification, as hover shows them.
const (
	kindValue  = "value"
	kindView   = "view"
	kindOwner  = "owner"
	kindShared = "shared"
)

// kindLine describes the ownership kind of a type, or returns "" for a
// type whose kind depends on how it is used (functions, interfaces, type
// parameters).
func kindLine(t types2.Type) string {
	switch typeKind(t, 0) {
	case kindOwner:
		return "**owner type** (moves): moves on assignment, freed automatically when its owner goes out of scope; copy it with `Clone`"
	case kindView:
		return "**view**: looks at memory something else owns; copying it is free, and it must not outlive that memory"
	case kindShared:
		return "**shared** (counted): copying it adds a reference; the object is freed when the last reference is dropped"
	case kindValue:
		return "**value**: copied on assignment"
	}
	return ""
}

// typeKind classifies a type as a value, view, owner or counted reference
// (spec section 6).
//
// The checker does not yet record which types are declared owner, so a
// named type counts as an owner if it has a Drop method, which only an
// owner may declare, or if it holds an owner.
func typeKind(t types2.Type, depth int) string {
	if depth > 8 {
		return ""
	}
	switch t := types2.Unalias(t).(type) {
	case *types2.Named:
		if obj, _, _ := types2.LookupFieldOrMethod(types2.NewPointer(t), false, t.Obj().Pkg(), "Drop"); obj != nil {
			if _, ok := obj.(*types2.Func); ok {
				return kindOwner
			}
		}
		if types2.EnumOf(t) != nil {
			return kindValue
		}
		return typeKind(t.Underlying(), depth+1)
	case *types2.Basic:
		switch {
		case t.Kind() == types2.Invalid || t.Info()&types2.IsUntyped != 0 && t.Kind() == types2.UntypedNil:
			return ""
		case t.Kind() == types2.String || t.Kind() == types2.UnsafePointer:
			return kindView
		}
		return kindValue
	case *types2.Pointer, *types2.Slice:
		return kindView
	case *types2.Chan:
		return kindShared
	case *types2.Wrapper:
		switch t.Kind() {
		case types2.WrapOwned:
			return kindOwner
		case types2.WrapShared, types2.WrapSynced:
			return kindShared
		}
		return typeKind(t.Elem(), depth+1)
	case *types2.Packed:
		return kindValue
	case *types2.Array:
		return aggregateKind(depth, t.Elem())
	case *types2.Struct:
		fields := make([]types2.Type, t.NumFields())
		for i := range fields {
			fields[i] = t.Field(i).Type()
		}
		return aggregateKind(depth, fields...)
	}
	return ""
}

// aggregateKind is the kind of a struct or array: an owner if it holds an
// owner, else a view if it holds a view, else a value (counted references
// in it are counted on copy, but the aggregate still copies).
func aggregateKind(depth int, elems ...types2.Type) string {
	kind := kindValue
	for _, e := range elems {
		switch typeKind(e, depth+1) {
		case kindOwner:
			return kindOwner
		case kindView:
			kind = kindView
		case "":
			if kind == kindValue {
				kind = ""
			}
		}
	}
	return kind
}

// packageDoc returns the package comment of a checked package.
func (w *workspace) packageDoc(p *types2.Package) *ast.CommentGroup {
	for _, f := range w.files {
		if f.pkg.types == p {
			for _, g := range f.pkg.files {
				if g.ast.Doc != nil {
					return g.ast.Doc
				}
			}
			return nil
		}
	}
	return nil
}

// directives gives the meaning of each //pu: directive (spec section 9).
var directives = map[string]string{
	"build":     "include the file only when the build constraint holds",
	"inline":    "always inline calls to this function",
	"noinline":  "never inline this function",
	"nobounds":  "omit bounds checks in the function's body",
	"extern":    "the body-less function is the external symbol with this name, called with the C ABI",
	"export":    "also export the function under this symbol name, callable from C",
	"section":   "place the function or package variable in the named section",
	"interrupt": "an x86-64 interrupt handler: saves every register it may change and returns with iretq",
	"noreturn":  "the function never returns, so code after calls to it is dropped",
	"nosimd":    "the optimiser adds no vector instructions to the function",
	"fpu":       "the function may use floating point in kernel mode; the caller has saved the FPU state",
	"percpu":    "one copy of the package variable per processor, addressed through GS (kernel mode only)",
}

// directiveName returns the name of a //pu: directive comment.
func directiveName(comment string) (string, bool) {
	rest, ok := strings.CutPrefix(comment, "//pu:")
	if !ok {
		return "", false
	}
	name, _, _ := strings.Cut(rest, " ")
	return name, name != ""
}

// directiveHover explains the //pu: directive under the cursor.
func directiveHover(f *srcFile, off int) *Hover {
	pos := f.tok.Pos(off)
	for _, g := range f.ast.Comments {
		if pos < g.Pos() || pos > g.End() {
			continue
		}
		for _, c := range g.List {
			if pos < c.Pos() || pos > c.End() {
				continue
			}
			name, ok := directiveName(c.Text)
			if !ok {
				return nil
			}
			meaning := directives[name]
			if meaning == "" {
				meaning = "not a directive the compiler knows (see section 9 of the language specification)"
			}
			start := f.tok.Offset(c.Pos())
			r := f.rangeOf(start, start+len(c.Text))
			return &Hover{
				Contents: MarkupContent{Kind: "markdown", Value: fmt.Sprintf("`//pu:%s`: %s", name, meaning)},
				Range:    &r,
			}
		}
	}
	return nil
}
