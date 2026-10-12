// Package doc renders checked package source without running or linking it.
package doc

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/format"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
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
				return fmt.Errorf("no symbol %q in package %s", symbol, p.Types.Name())
			}
			selected = append(selected, e)
		} else {
			for _, e := range entries {
				if e.parent == "" && e.name == name {
					selected = append(selected, e)
				}
			}
			if len(selected) == 0 {
				return fmt.Errorf("no symbol %q in package %s", symbol, p.Types.Name())
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
					names = append(names, method)
				}
				sort.Strings(names)
				for _, method := range names {
					if e, ok := methodEntry(prog, name, method); ok {
						selected = append(selected, e)
					}
				}
			}
		}
	} else {
		for _, e := range entries {
			if all || ast.IsExported(e.name) && (e.parent == "" || ast.IsExported(e.parent)) {
				selected = append(selected, e)
			}
		}
	}
	var out bytes.Buffer
	fmt.Fprintf(&out, "package %s // import %q\n", p.Types.Name(), p.Path)
	if symbol == "" {
		for _, f := range p.Files {
			if text := strings.TrimSpace(f.Doc.Text()); text != "" {
				fmt.Fprintf(&out, "\n%s\n", text)
			}
		}
	}
	qf := func(other *types2.Package) string {
		if other == p.Types {
			return ""
		}
		return other.Name()
	}
	for _, e := range selected {
		out.WriteByte('\n')
		if e.source != "" {
			fmt.Fprintf(&out, "Source: %s\n", e.source)
		}
		if _, ok := e.obj.(*types2.TypeName); ok {
			fmt.Fprintf(&out, "%s [%s]\n", e.name, types2.KindOf(e.obj.Type()))
		}
		if e.node != nil {
			if err := format.Node(&out, prog.Fset, e.node); err != nil {
				return err
			}
			out.WriteByte('\n')
		} else {
			if c, ok := e.obj.(*types2.Const); ok {
				if basic, ok := c.Type().(*types2.Basic); ok && basic.Info()&types2.IsUntyped != 0 {
					fmt.Fprintf(&out, "const %s = %s\n", c.Name(), c.Val().ExactString())
				} else {
					fmt.Fprintf(&out, "%s = %s\n", types2.ObjectString(c, qf), c.Val().ExactString())
				}
			} else {
				fmt.Fprintln(&out, types2.ObjectString(e.obj, qf))
			}
		}
		if e.signature != "" {
			fmt.Fprintf(&out, "Signature: %s\n", e.signature)
		}
		if text := strings.TrimSpace(e.comment.Text()); text != "" {
			for line := range strings.SplitSeq(text, "\n") {
				fmt.Fprintf(&out, "    %s\n", line)
			}
		}
	}
	n, err := w.Write(out.Bytes())
	if err == nil && n != out.Len() {
		err = io.ErrShortWrite
	}
	return err
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
				copy := *d
				copy.Body, copy.Doc = nil, nil
				entries = append(entries, entry{name: d.Name.Name, parent: parent, obj: obj, node: &copy, comment: d.Doc})
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						copy := *s
						copy.Doc, copy.Comment = nil, nil
						node := &ast.GenDecl{Tok: token.TYPE, TokPos: d.TokPos, Specs: []ast.Spec{&copy}}
						entries = append(entries, entry{name: s.Name.Name, obj: p.Info.Defs[s.Name], node: node, comment: comment(s.Doc, s.Comment, d.Doc)})
					case *ast.ValueSpec:
						for _, name := range s.Names {
							if obj := p.Info.Defs[name]; obj != nil && name.Name != "_" {
								entries = append(entries, entry{name: name.Name, obj: obj, comment: comment(s.Doc, s.Comment, d.Doc)})
							}
						}
					}
				}
			}
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].parent != entries[j].parent {
			return entries[i].parent < entries[j].parent
		}
		return entries[i].name < entries[j].name
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
	qf := func(other *types2.Package) string {
		if other == p.Types {
			return ""
		}
		return other.Name()
	}
	for _, sourcePkg := range prog.Packages {
		for _, e := range entriesOf(sourcePkg) {
			original, ok := e.obj.(*types2.Func)
			if !ok || original.Origin() != fn.Origin() {
				continue
			}
			if sourcePkg != p || e.parent != name {
				e.source = sourcePkg.Path + "." + e.parent + "." + e.name
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
