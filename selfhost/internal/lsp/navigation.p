// Translated by go2pu from internal/lsp/navigation.go; edited by hand since.

package lsp

import (
	"pluto/cmp"
	"pluto/slices"
	"selfhost/compat"
	"selfhost/internal/syntax/ast"
	"selfhost/internal/syntax/token"
	"selfhost/internal/types2"
	"selfhost/pu"
)

func (s *server) definition(p *TextDocumentPositionParams) ([]Location, *rpcError) {
	f, off, rerr := s.fileAt(p.TextDocument.URI, p.Position)
	if f == nil {
		return nil, rerr
	}
	_, obj := objectAt(f, off)
	if obj == nil {
		return nil, nil
	}
	loc, ok := s.ws.locationOf(obj)
	if !ok {
		return nil, nil
	}
	return compat.Clone([]Location{loc}), nil
}

func (s *server) references(p *ReferenceParams) ([]Location, *rpcError) {
	f, off, rerr := s.fileAt(p.TextDocument.URI, p.Position)
	if f == nil {
		return nil, rerr
	}
	_, obj := objectAt(f, off)
	if obj == nil {
		return nil, nil
	}
	refs := s.ws.references(f.pkg, obj, p.Context.IncludeDeclaration)
	locs := compat.Make[Location](len(refs))
	for i, r := range refs {
		locs[i] = r.location()
	}
	return locs, nil
}

// A reference is an identifier that denotes an object.
type reference struct {
	file *srcFile
	off  int
	name string
}

func (r reference) location() Location {
	return Location{URI: uriFromPath(r.file.path), Range: r.file.rangeOf(r.off, r.off+len(r.name))}
}

// references returns the identifiers in the workspace that refer to obj,
// and its declaration if decl is set, in file order. The package the
// request came from is searched too, so that references work inside a
// standard library file opened through go to definition.
func (w *workspace) references(from *pkg, obj types2.Object, decl bool) []reference {
	key, ok := w.keyOf(obj)
	if !ok {
		return nil
	}
	pkgs := w.workspacePackages()
	if !slices.Contains(pkgs, from) {
		pkgs = compat.Append(pkgs, from)
	}
	c := refCollector{w: w, key: key, seen: map[refAt]bool{}}
	for _, p := range pkgs {
		for id, o := range p.info.Uses {
			c.add(id, o)
		}
		if decl {
			for id, o := range p.info.Defs {
				c.add(id, o)
			}
		}
	}
	refs := c.refs
	slices.SortFunc(refs, func(a, b reference) int {
		return cmp.Or(cmp.Compare(a.file.path, b.file.path), cmp.Compare(a.off, b.off))
	})
	return refs
}

// A refAt is where a reference is.
type refAt struct {
	file string
	off  int
}

// A refCollector gathers the references to the object with key.
type refCollector struct {
	w    *workspace
	key  objKey
	seen map[refAt]bool
	refs []reference
}

func (c *refCollector) add(id *ast.Ident, o types2.Object) {
	if o == nil || o.Name() != c.key.name {
		return
	}
	if k, ok := c.w.keyOf(o); !ok || k != c.key {
		return
	}
	f := c.w.srcFileAt(id.Pos())
	if f == nil {
		return
	}
	r := reference{file: f, off: f.tok.Offset(id.Pos()), name: id.Name}
	if a := (refAt{f.path, r.off}); !c.seen[a] {
		c.seen[a] = true
		c.refs = compat.Append(c.refs, r)
	}
}

// renameTarget returns the object to rename at a position and the
// identifier there, or an error saying why it cannot be renamed.
func (s *server) renameTarget(p *TextDocumentPositionParams) (*srcFile, *ast.Ident, types2.Object, *rpcError) {
	f, off, rerr := s.fileAt(p.TextDocument.URI, p.Position)
	if f == nil {
		if rerr == nil {
			rerr = newError(codeRequestFailed, "no package for this file")
		}
		return nil, nil, nil, rerr
	}
	id, obj := objectAt(f, off)
	switch {
	case id == nil || obj == nil:
		return nil, nil, nil, newError(codeRequestFailed, "no identifier to rename here")
	case obj.Pkg() == nil:
		return nil, nil, nil, newError(codeRequestFailed, compat.Concat(obj.Name(), " is predeclared and cannot be renamed"))
	}
	if _, ok := obj.(*types2.PkgName); ok {
		return nil, nil, nil, newError(codeRequestFailed, "renaming an imported package is not supported")
	}
	decl := s.ws.srcFileAt(obj.Pos())
	if decl == nil || !s.ws.inWorkspace(decl.path) {
		return nil, nil, nil, newError(codeRequestFailed, compat.Concat(obj.Name(), " is declared outside the workspace"))
	}
	return f, id, obj, nil
}

func (s *server) prepareRename(p *TextDocumentPositionParams) (*PrepareRenameResult, *rpcError) {
	f, id, _, rerr := s.renameTarget(p)
	if rerr != nil {
		return nil, rerr
	}
	start := f.tok.Offset(id.Pos())
	return pu.New(PrepareRenameResult{Range: f.rangeOf(start, start+len(id.Name)), Placeholder: id.Name}), nil
}

func (s *server) rename(p *RenameParams) (*WorkspaceEdit, *rpcError) {
	if !token.IsIdentifier(p.NewName) {
		return nil, newError(codeInvalidParams, compat.Sprintfb("%q is not a valid identifier", compat.ArgS(p.NewName)))
	}
	f, _, obj, rerr := s.renameTarget(&p.TextDocumentPositionParams)
	if rerr != nil {
		return nil, rerr
	}
	if p.NewName == obj.Name() {
		return pu.New(WorkspaceEdit{}), nil
	}
	if conflict := s.renameConflict(f, obj, p.NewName); conflict != "" {
		return nil, newError(codeRequestFailed, conflict)
	}
	edit := pu.New(WorkspaceEdit{})
	for _, r := range s.ws.references(f.pkg, obj, true) {
		uri := uriFromPath(r.file.path)
		if doc := s.docs[r.file.path]; doc != nil {
			uri = doc.uri
		}
		edit.add(uri, TextEdit{Range: r.location().Range, NewText: p.NewName})
	}
	return edit, nil
}

// renameConflict reports a declaration the new name would collide with in
// the scope of obj, or "".
func (s *server) renameConflict(f *srcFile, obj types2.Object, name string) string {
	scope := obj.Parent()
	if fn, ok := obj.(*types2.Func); ok && scope == nil {
		if recv := fn.Signature().Recv(); recv != nil {
			if other, _, _ := types2.LookupFieldOrMethod(recv.Type(), true, obj.Pkg(), name); other != nil {
				return compat.Concat("renaming ", obj.Name(), " to ", name, " conflicts with ", types2.ObjectString(other, qualifier(f.pkg.types)))
			}
		}
		return ""
	}
	if scope == nil {
		return "" // a field: its struct is not known here
	}
	if other := scope.Lookup(name); other != nil {
		return compat.Concat("renaming ", obj.Name(), " to ", name, " conflicts with ", types2.ObjectString(other, qualifier(f.pkg.types)), " in the same scope")
	}
	return ""
}
