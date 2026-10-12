package lsp

import (
	"path/filepath"
	"testing"

	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

func TestPackageLocationFileOrder(t *testing.T) {
	fset := token.NewFileSet()
	p := &pkg{types: types2.NewPackage("example", "example")}
	root := t.TempDir()
	makeFile := func(name string) *srcFile {
		path := filepath.Join(root, name)
		src := []byte("package example\n")
		tok := fset.AddFile(path, -1, len(src))
		return &srcFile{path: path, text: newText(src), tok: tok, pkg: p,
			ast: &ast.File{Name: &ast.Ident{Name: "example", NamePos: tok.Pos(8)}}}
	}
	b, a := makeFile("b.p"), makeFile("a.p")
	// A package's declarations can be supplied before every source is
	// indexed. The location still belongs to its first file by name.
	p.files = []*srcFile{b, a}
	w := &workspace{files: map[*token.File]*srcFile{b.tok: b}}
	got, ok := w.packageLocation(p.types)
	want := Location{URI: uriFromPath(a.path), Range: a.rangeOf(8, 15)}
	if !ok || got != want {
		t.Fatalf("package location = %+v, %v; want %+v", got, ok, want)
	}
}
