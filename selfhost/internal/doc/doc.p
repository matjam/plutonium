// Translated by go2pu from internal/doc/doc.go; edited by hand since.

// Package doc renders checked package source without running or linking it.
package doc

import (
	"pluto/io"
	"pluto/sort"
	"pluto/strings"
	"selfhost/compat"
	"selfhost/internal/load"
	"selfhost/internal/syntax/ast"
	"selfhost/internal/syntax/format"
	"selfhost/internal/syntax/token"
	"selfhost/internal/types2"
	"selfhost/pu"
)

type entry struct {
	name, parent      string
	obj               types2.Object
	node              ast.Decl
	comment           *ast.CommentGroup
	source, signature string
}

// Write documents the package, or a symbol (optionally Type.Method).
// Package listings include exported declarations unless all is true. An
// explicit symbol may name a private declaration.
func Write(w io.Writer, prog *load.Program, symbol string, all bool) error {
	p := prog.Main
	entries := entriesOf(p)
	selected := entries[:0:0]
	if symbol != "" {
		name, method, hasMethod := strings.Cut(symbol, ".")
		if hasMethod {
			e, ok := methodEntry(prog, name, method)
			if !ok {
				return compat.NewError(compat.Sprintfb("no symbol %q in package %s", compat.ArgS(symbol), compat.ArgS(p.Types.Name())))
			}
			selected = compat.Append(selected, e)
		} else {
			for _, e := range entries {
				if e.parent == "" && e.name == name {
					selected = compat.Append(selected, e)
				}
			}
			if len(selected) == 0 {
				return compat.NewError(compat.Sprintfb("no symbol %q in package %s", compat.ArgS(symbol), compat.ArgS(p.Types.Name())))
			}
		}
		if !hasMethod {
			if _, ok := selected[0].obj.(*types2.TypeName); ok {
				methods := map[string]bool{}
				for _, e := range entries {
					if e.parent == name && (all || ast.IsExported(e.name)) {
						methods[e.name] = true
					}
				}
				t := types2.Unalias(selected[0].obj.Type())
				if _, ok := t.(*types2.Pointer); !ok && !types2.IsInterface(t) {
					t = types2.NewPointer(t)
				}
				set := types2.NewMethodSet(t)
				for i := range set.Len() {
					method := set.At(i).Obj().Name()
					if all || ast.IsExported(method) {
						methods[method] = true
					}
				}
				var names []string
				for method := range methods {
					names = compat.Append(names, method)
				}
				sort.Strings(names)
				for _, method := range names {
					if e, ok := methodEntry(prog, name, method); ok {
						selected = compat.Append(selected, e)
					}
				}
			}
		}
	} else {
		for _, e := range entries {
			if all || ast.IsExported(e.name) && (e.parent == "" || ast.IsExported(e.parent)) {
				selected = compat.Append(selected, e)
			}
		}
	}
	out := pu.Alloc[compat.Buffer]()
	compat.Fprintfb(out, "package %s // import %q\n", compat.ArgS(p.Types.Name()), compat.ArgS(p.Path))
	if symbol == "" {
		for _, f := range p.Files {
			if text := strings.TrimSpace(f.Doc.Text()); text != "" {
				compat.Fprintfb(out, "\n%s\n", compat.ArgS(text))
			}
		}
	}
	qf := nameQualifier(p.Types)
	for _, e := range selected {
		out.WriteByte('\n')
		if e.source != "" {
			compat.Fprintfb(out, "Source: %s\n", compat.ArgS(e.source))
		}
		if _, ok := e.obj.(*types2.TypeName); ok {
			compat.Fprintfb(out, "%s [%s]\n", compat.ArgS(e.name), compat.ArgS((types2.KindOf(e.obj.Type())).String()))
		}
		if e.node != nil {
			format.Node(out, prog.Fset, e.node) or return
			out.WriteByte('\n')
		} else {
			if c, ok := e.obj.(*types2.Const); ok {
				if basic, ok := c.Type().(*types2.Basic); ok && basic.Info()&types2.IsUntyped != 0 {
					compat.Fprintfb(out, "const %s = %s\n", compat.ArgS(c.Name()), compat.ArgS(c.Val().ExactString()))
				} else {
					compat.Fprintfb(out, "%s = %s\n", compat.ArgS(types2.ObjectString(c, qf)), compat.ArgS(c.Val().ExactString()))
				}
			} else {
				compat.Fprintfb(out, "%v\n", compat.ArgS(types2.ObjectString(e.obj, qf)))
			}
		}
		if e.signature != "" {
			compat.Fprintfb(out, "Signature: %s\n", compat.ArgS(e.signature))
		}
		if text := strings.TrimSpace(e.comment.Text()); text != "" {
			for line := range strings.SplitSeq(text, "\n") {
				compat.Fprintfb(out, "    %s\n", compat.ArgS(line))
			}
		}
	}
	n, err := w.Write(out.Bytes())
	if err == nil && n != out.Len() {
		err = io.ErrShortWrite
	}
	return err
}

// nameQualifier returns a qualifier that names the packages other than pkg
// by their package name.
func nameQualifier(pkg *types2.Package) types2.Qualifier {
	return func(other *types2.Package) string {
		if other == pkg {
			return ""
		}
		return other.Name()
	}
}

func comment(groups ...*ast.CommentGroup) *ast.CommentGroup {
	for _, g := range groups {
		if g != nil {
			return g
		}
	}
	return nil
}

func entriesOf(p *load.Package) []entry {
	var entries []entry
	for _, f := range p.Files {
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				obj, ok := p.Info.Defs[d.Name].(*types2.Func)
				if !ok {
					continue
				}
				parent := ""
				if recv := obj.Type().(*types2.Signature).Recv(); recv != nil {
					t := types2.Unalias(recv.Type())
					if ptr, ok := t.(*types2.Pointer); ok {
						t = types2.Unalias(ptr.Elem())
					}
					if named, ok := t.(*types2.Named); ok {
						parent = named.Obj().Name()
					}
				}
				copy := pu.New(*d)
				copy.Body, copy.Doc = nil, nil
				entries = compat.Append(entries, entry{name: d.Name.Name, parent: parent, obj: obj, node: copy, comment: d.Doc})
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						copy := pu.New(*s)
						copy.Doc, copy.Comment = nil, nil
						node := pu.New(ast.GenDecl{Tok: token.TYPE, TokPos: d.TokPos, Specs: compat.Clone([]ast.Spec{copy})})
						entries = compat.Append(entries, entry{name: s.Name.Name, obj: p.Info.Defs[s.Name], node: node, comment: comment(s.Doc, s.Comment, d.Doc)})
					case *ast.ValueSpec:
						for _, name := range s.Names {
							if obj := p.Info.Defs[name]; obj != nil && name.Name != "_" {
								entries = compat.Append(entries, entry{name: name.Name, obj: obj, comment: comment(s.Doc, s.Comment, d.Doc)})
							}
						}
					}
				}
			}
		}
	}
	sort.SortStableFunc(entries, func(a, b entry) bool {
		if a.parent != b.parent {
			return a.parent < b.parent
		}
		return a.name < b.name
	})
	return entries
}

// methodEntry follows the checked receiver type, including aliases, generic
// instantiations and promoted methods. Source is located by the original
// function object rather than by the receiver spelling in the target file.
func methodEntry(prog *load.Program, name, method string) (entry, bool) {
	p := prog.Main
	obj, ok := p.Types.Scope().Lookup(name).(*types2.TypeName)
	if !ok {
		return entry{}, false
	}
	t := types2.Unalias(obj.Type())
	if _, ok := t.(*types2.Pointer); !ok && !types2.IsInterface(t) {
		t = types2.NewPointer(t)
	}
	found, _, _ := types2.LookupFieldOrMethod(t, true, p.Types, method)
	fn, ok := found.(*types2.Func)
	if !ok {
		return entry{}, false
	}
	qf := nameQualifier(p.Types)
	for _, sourcePkg := range prog.Packages {
		for _, e := range entriesOf(sourcePkg) {
			original, ok := e.obj.(*types2.Func)
			if !ok || original.Origin() != fn.Origin() {
				continue
			}
			if sourcePkg != p || e.parent != name {
				e.source = compat.Concat(sourcePkg.Path, ".", e.parent, ".", e.name)
			}
			if types2.ObjectString(fn, qf) != types2.ObjectString(original, qf) {
				e.signature = types2.ObjectString(fn, qf)
			}
			return e, true
		}
	}
	// Interface and built-in methods have no function declaration. Their
	// checked signature still provides documentation without a fake body.
	e := entry{name: method, parent: name, obj: fn}
	for _, sourcePkg := range prog.Packages {
		for _, file := range sourcePkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				field, ok := n.(*ast.Field)
				if !ok {
					return true
				}
				for _, id := range field.Names {
					if sourcePkg.Info.Defs[id] == fn.Origin() {
						e.comment = comment(field.Doc, field.Comment)
					}
				}
				return true
			})
		}
	}
	return e, true
}
