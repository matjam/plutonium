// Translated by go2pu from internal/lsp/hover.go; edited by hand since.

package lsp

import (
	"pluto/strings"
	"compiler/compat"
	"compiler/internal/syntax/ast"
	"compiler/internal/types2"
	"compiler/pu"
)

// codeLang is the language of code blocks in hover text: the extension's
// language identifier, which gives them its highlighting.
const codeLang = "plutonium"

func (s *server) hover(p *TextDocumentPositionParams) (*Hover, *rpcError) {
	f, off, rerr := s.fileAt(p.TextDocument.URI, p.Position)
	if f == nil {
		return nil, rerr
	}
	h := directiveHover(f, off)
	if h == nil {
		h = orHover(f, off)
	}
	if h == nil {
		h = s.objectHover(f, off)
	}
	// The explanation of an error here comes first.
	if errs := errorHover(f, off); errs != "" {
		if h == nil {
			return pu.New(Hover{Contents: MarkupContent{Kind: "markdown", Value: errs}}), nil
		}
		h.Contents.Value = compat.Concat(errs, "\n---\n\n", h.Contents.Value)
	}
	return h, nil
}

// objectHover describes the object named at off, or returns nil.
func (s *server) objectHover(f *srcFile, off int) *Hover {
	id, obj := objectAt(f, off)
	if obj == nil {
		return nil
	}
	b := pu.Alloc[compat.Builder]()
	s.describe(b, f.pkg.types, obj)
	h := pu.New(Hover{Contents: MarkupContent{Kind: "markdown", Value: b.String()}})
	if id != nil {
		start := f.tok.Offset(id.Pos())
		r := pu.New(f.rangeOf(start, start+len(id.Name)))
		h.Range = r
	}
	return h
}

// describe writes the markdown description of an object: its declaration,
// its kind under the ownership rules, enum members, directives and doc
// comment.
func (s *server) describe(b *compat.Builder, cur *types2.Package, obj types2.Object) {
	qf := qualifier(cur)
	compat.Fprintfb(b, "```%s\n%s\n```\n", compat.ArgS(codeLang), compat.ArgS(declString(obj, qf)))

	switch obj := obj.(type) {
	case *types2.TypeName:
		if line := kindLine(obj.Type()); line != "" {
			compat.Fprintfb(b, "\n%s\n", compat.ArgS(line))
		}
	case *types2.Var:
		if line := kindLine(obj.Type()); line != "" {
			compat.Fprintfb(b, "\n%s\n", compat.ArgS(line))
		}
	}

	if obj.Parent() == types2.Universe {
		if text := predeclaredDocs[obj.Name()]; text != "" {
			compat.Fprintfb(b, "\n%s\n", compat.ArgS(text))
		}
	}

	doc := s.ws.docOf(obj)
	if pn, ok := obj.(*types2.PkgName); ok {
		doc = s.ws.packageDoc(pn.Imported())
	}
	if doc != nil {
		var dirs []string
		for _, c := range doc.List {
			if _, ok := directiveName(c.Text); ok {
				d := compat.Concat("`", strings.TrimSpace(c.Text), "`")
				if meaning := directiveMeaning(c.Text); meaning != "" {
					d = compat.Concat(d, compat.Concat(": ", meaning))
				}
				dirs = compat.Append(dirs, d)
			}
		}
		if text := strings.TrimSpace(doc.Text()); text != "" {
			compat.Fprintfb(b, "\n%s\n", compat.ArgS(text))
		}
		if len(dirs) > 0 {
			compat.Fprintfb(b, "\n%s\n", compat.ArgS(compat.Join(dirs, "  \n")))
		}
	}
}

// predeclaredDocs describes the predeclared identifiers whose meaning is
// more than their declaration shows (spec section 10.3).
var predeclaredDocs = map[string]string{
	"portable": "**constraint** (spec section 8.9): satisfied by the types whose values can be sent between address spaces, such as two processes that share no memory. A portable type is built only from booleans, numbers, enums, packed types, and arrays, structs and unions of them. A pointer, slice, string, function value, channel, map, interface value, box or owner anywhere inside makes a type not portable. Use it as a type constraint only: `func Send[T portable](v T)`.",
}

// declString returns the declaration of an object as it would be written,
// with an enum's members.
func declString(obj types2.Object, qf types2.Qualifier) string {
	switch obj := obj.(type) {
	case *types2.TypeName:
		if en := types2.EnumOf(obj.Type()); en != nil {
			b := pu.Alloc[compat.Builder]()
			compat.Fprintfb(b, "type %s enum %s {\n", compat.ArgS(obj.Name()), compat.ArgS(types2.TypeString(obj.Type().Underlying(), qf)))
			width := 0
			for i := range en.Len() {
				width = max(width, len(en.Member(i).Name()))
			}
			for i := range en.Len() {
				m := en.Member(i)
				compat.Fprintfb(b, "\t%s%s = %s\n", compat.ArgS(m.Name()), compat.ArgS(strings.Repeat(" ", width-len(m.Name()))), compat.ArgS(m.Val().ExactString()))
			}
			b.WriteString("}")
			return b.String()
		}
	case *types2.Const:
		if en := types2.EnumOf(obj.Type()); en != nil && en.Obj() != nil {
			for i := range en.Len() {
				if en.Member(i) == obj {
					return compat.Sprintfb("const %s.%s %s = %s", compat.ArgS(en.Obj().Name()), compat.ArgS(obj.Name()), compat.ArgS(types2.TypeString(obj.Type(), qf)), compat.ArgS(obj.Val().ExactString()))
				}
			}
		}
	case *types2.PkgName:
		return compat.Sprintfb("package %s (%q)", compat.ArgS(obj.Imported().Name()), compat.ArgS(obj.Imported().Path()))
	}
	return types2.ObjectString(obj, qf)
}

// kindLine uses the same static classification as pluto doc (section 18.1).
func kindLine(t types2.Type) string {
	switch types2.KindOf(t) {
	case types2.KindOwner:
		return "**owner (moves)**: moves on assignment and is dropped when its lifetime ends"
	case types2.KindView:
		return "**view**: may contain borrowed references that must not outlive their source"
	case types2.KindCounted:
		return "**counted**: copies add counts and drops release counts"
	case types2.KindValue:
		return "**value**: copied on assignment"
	}
	return ""
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

// directives gives the meaning of each //pu: directive (spec section 20.3).
var directives = map[string]string{
	"build":       "include the file only when the build constraint holds",
	"inline":      "always inline calls to this function",
	"noinline":    "never inline this function",
	"nochecks":    "remove the bounds checks and the generation checks from the function's body",
	"extern":      "the body-less function is the external symbol with this name, called with the C ABI",
	"export":      "also export the function under this symbol name, callable from C",
	"section":     "place the function or package variable in the named section",
	"interrupt":   "an x86-64 interrupt or exception handler, entered through an interrupt gate (x86.InterruptGate), which clears the interrupt flag: the body is a region with interrupts disabled until x86.Sti or a call that may switch tasks. It saves every register it may change and returns with iretq. The form `//pu:interrupt trap` is a handler that can be entered again",
	"noreturn":    "the assembly or C function never returns, so code after calls to it is dropped (a function with a body needs no directive: the compiler works it out)",
	"noescape":    "the body-less function keeps no pointer argument after it returns, except by storing it where another argument points (a function with a body needs no directive: the compiler works it out)",
	"resultholds": "standard library only: the function keeps the named parameter only while its result, an owner, lives, and gives it back when the result is dropped",
	"nosimd":      "the optimizer adds no vector instructions to the function",
	"fpu":         "the function may use floating point in kernel mode; the caller has saved the FPU state",
	"percpu":      "one copy of the package variable per processor, addressed through GS (kernel mode only)",
	"blocking":    "calling the function may wait in a way the compiler cannot see (a kernel's spinning lock, assembly that halts), so interrupt handlers and the panic handler must not reach it",
	"panic":       "the program's panic handler, func(p *rt.Panic), which must never return",
	"scheduler":   "the package variable is the program's scheduler backend, replacing the hosted one",
	"stack":       "run main on a stack of this size, which the hosted runtime reserves (hosted builds only)",
}

// directiveForms gives the meaning of a directive written with an argument
// that makes it a form of its own (spec section 20.3), by the text after //pu:.
var directiveForms = map[string]string{
	"interrupt trap": "an x86-64 interrupt or exception handler that can be re-entered, entered through a trap gate (x86.TrapGate), which leaves the interrupt flag as it was: its body is not a region with interrupts disabled, so data it shares is protected by a region it opens itself. It saves every register it may change and returns with iretq",
}

// directiveMeaning returns what a //pu: directive comment means: the entry of
// its name and argument if it is a form of its own, else of its name.
func directiveMeaning(comment string) string {
	rest, ok := strings.CutPrefix(comment, "//pu:")
	if !ok {
		return ""
	}
	if m := directiveForms[compat.Join(compat.Fields(rest), " ")]; m != "" {
		return m
	}
	name, _ := directiveName(comment)
	return directives[name]
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
			meaning := directiveMeaning(c.Text)
			if meaning == "" {
				meaning = "not a directive the compiler knows (see section 20.3 of the language specification)"
			}
			start := f.tok.Offset(c.Pos())
			r := pu.New(f.rangeOf(start, start+len(c.Text)))
			return pu.New(Hover{
				Contents: MarkupContent{Kind: "markdown", Value: compat.Sprintfb("`//pu:%s`: %s", compat.ArgS(name), compat.ArgS(meaning))},
				Range:    r,
			})
		}
	}
	return nil
}

// orHoverText explains the contextual keyword or (language spec 11.27).
const orHoverText = "`or`: an or clause. The clause runs when the call before `or` returns an error as its last result; in it, `err` is that error. Otherwise the call's other results are assigned or declared. The clause must leave, by `return`, `break`, `continue`, `goto` or `panic`. `or return` passes the error up as the function's last result, and `or return E` returns `E` as it (see section 11.27 of the language specification)."

// orHover explains the or of an or statement under the cursor.
func orHover(f *srcFile, off int) *Hover {
	pos := f.tok.Pos(off)
	var or *ast.OrStmt
	ast.Inspect(f.ast, func(n ast.Node) bool {
		if or != nil || n == nil || n.Pos() > pos || n.End() < pos {
			return false
		}
		if st, ok := n.(*ast.OrStmt); ok && st.Or <= pos && pos <= st.Or+2 {
			or = st
		}
		return true
	})
	if or == nil {
		return nil
	}
	start := f.tok.Offset(or.Or)
	r := pu.New(f.rangeOf(start, start+2))
	return pu.New(Hover{Contents: MarkupContent{Kind: "markdown", Value: orHoverText}, Range: r})
}
