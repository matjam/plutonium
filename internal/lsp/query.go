package lsp

import (
	"path/filepath"

	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// enclosing returns the nodes of f's syntax tree that contain pos,
// outermost first. A node contains the position just past its end too, so
// that a cursor after an identifier finds it.
func enclosing(f *ast.File, pos token.Pos) []ast.Node {
	var path []ast.Node
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		if n.Pos() <= pos && pos <= n.End() {
			path = append(path, n)
			return true
		}
		return false
	})
	return path
}

// identAt returns the identifier at off in f, or nil. An identifier that
// starts at off wins over one that ends there.
func identAt(f *srcFile, off int) *ast.Ident {
	pos := f.tok.Pos(off)
	var found *ast.Ident
	ast.Inspect(f.ast, func(n ast.Node) bool {
		if n == nil || n.Pos() > pos || n.End() < pos {
			return false
		}
		if id, ok := n.(*ast.Ident); ok {
			if found == nil || id.Pos() == pos {
				found = id
			}
		}
		return true
	})
	return found
}

// objectOf returns the object an identifier defines or uses.
func objectOf(info *types2.Info, id *ast.Ident) types2.Object {
	if obj := info.Defs[id]; obj != nil {
		return obj
	}
	return info.Uses[id]
}

// objectAt returns the identifier at off and the object it denotes. On an
// import path, it returns the imported package's name.
func objectAt(f *srcFile, off int) (*ast.Ident, types2.Object) {
	if id := identAt(f, off); id != nil {
		if obj := objectOf(f.pkg.info, id); obj != nil {
			return id, obj
		}
		// The package name of a declaration: no object, so nothing to show.
		return id, nil
	}
	pos := f.tok.Pos(off)
	for _, spec := range f.ast.Imports {
		if spec.Path.Pos() <= pos && pos <= spec.Path.End() {
			if obj := f.pkg.info.Implicits[spec]; obj != nil {
				return nil, obj
			}
			if spec.Name != nil {
				return nil, f.pkg.info.Defs[spec.Name]
			}
		}
	}
	return nil, nil
}

// An objKey identifies an object across the packages of a workspace, which
// check the same file more than once (with and without tests, and in
// different modes): its name and where it is declared.
type objKey struct {
	name string
	file string
	off  int
}

func (w *workspace) keyOf(obj types2.Object) (objKey, bool) {
	f := w.srcFileAt(obj.Pos())
	if f == nil {
		return objKey{}, false
	}
	return objKey{name: obj.Name(), file: f.path, off: f.tok.Offset(obj.Pos())}, true
}

// locationOf returns where an object is declared; for an imported
// package's name, the imported package.
func (w *workspace) locationOf(obj types2.Object) (Location, bool) {
	if pn, ok := obj.(*types2.PkgName); ok {
		return w.packageLocation(pn.Imported())
	}
	f := w.srcFileAt(obj.Pos())
	if f == nil {
		return Location{}, false
	}
	off := f.tok.Offset(obj.Pos())
	return Location{URI: uriFromPath(f.path), Range: f.rangeOf(off, off+len(obj.Name()))}, true
}

// packageLocation returns the package clause of a package's first file.
func (w *workspace) packageLocation(p *types2.Package) (Location, bool) {
	if p == nil {
		return Location{}, false
	}
	for _, f := range w.files {
		if f.pkg.types == p {
			best := f
			for _, g := range f.pkg.files {
				if filepath.Base(g.path) < filepath.Base(best.path) {
					best = g
				}
			}
			off := best.tok.Offset(best.ast.Name.Pos())
			return Location{URI: uriFromPath(best.path), Range: best.rangeOf(off, off+len(best.ast.Name.Name))}, true
		}
	}
	return Location{}, false
}

// docOf returns the doc comment of the declaration of obj: the comment
// above it, or the comment after it on its line for a field or member.
func (w *workspace) docOf(obj types2.Object) *ast.CommentGroup {
	f := w.srcFileAt(obj.Pos())
	if f == nil {
		return nil
	}
	pos := obj.Pos()
	var doc *ast.CommentGroup
	ast.Inspect(f.ast, func(n ast.Node) bool {
		if doc != nil || n == nil || n.Pos() > pos || n.End() < pos {
			return false
		}
		switch n := n.(type) {
		case *ast.FuncDecl:
			if n.Name.Pos() == pos {
				doc = n.Doc
				return false
			}
		case *ast.GenDecl:
			for _, spec := range n.Specs {
				var specDoc, specComment *ast.CommentGroup
				match := false
				switch s := spec.(type) {
				case *ast.TypeSpec:
					match = s.Name.Pos() == pos
					specDoc, specComment = s.Doc, s.Comment
				case *ast.ValueSpec:
					for _, name := range s.Names {
						match = match || name.Pos() == pos
					}
					specDoc, specComment = s.Doc, s.Comment
				}
				if match {
					doc = firstComment(specDoc, groupDoc(n), specComment)
					return false
				}
			}
		case *ast.Field:
			for _, name := range n.Names {
				if name.Pos() == pos {
					doc = firstComment(n.Doc, n.Comment)
					return false
				}
			}
		case *ast.EnumMember:
			if n.Name.Pos() == pos {
				doc = firstComment(n.Doc, n.Comment)
				return false
			}
		}
		return true
	})
	return doc
}

// groupDoc returns the doc comment of a declaration group with one spec,
// which documents the spec.
func groupDoc(d *ast.GenDecl) *ast.CommentGroup {
	if len(d.Specs) == 1 {
		return d.Doc
	}
	return nil
}

func firstComment(groups ...*ast.CommentGroup) *ast.CommentGroup {
	for _, g := range groups {
		if g != nil {
			return g
		}
	}
	return nil
}

// qualifier qualifies names of other packages by package name.
func qualifier(cur *types2.Package) types2.Qualifier {
	return func(p *types2.Package) string {
		if p == cur || p == nil {
			return ""
		}
		return p.Name()
	}
}
