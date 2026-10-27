// Ported from internal/lsp/query_test.go at the go-cutover tag.

package lsp

import (
	"pluto/testing"

	"compiler/compat"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
)

func TestPackageLocationFileOrder(t *testing.T) {
	fset := token.NewFileSet()
	p := pu.New(pkg{types: types2.NewPackage("example", "example")})
	root := "/example"
	makeFile := func(name string) *srcFile {
		path := root + "/" + name
		src := compat.Bytes("package example\n")
		tok := fset.AddFile(path, -1, len(src))
		file := pu.New(ast.File{Name: pu.New(ast.Ident{Name: "example", NamePos: tok.Pos(8)})})
		return pu.New(srcFile{path: path, text: newText(src), tok: tok, pkg: p, ast: file})
	}
	b, a := makeFile("b.p"), makeFile("a.p")
	// A package's declarations can be supplied before every source is
	// indexed. The location still belongs to its first file by name.
	p.files = compat.Append[*srcFile](nil, b, a)
	w := pu.New(workspace{files: map[*token.File]*srcFile{b.tok: b}})
	got, ok := w.packageLocation(p.types)
	want := Location{URI: uriFromPath(a.path), Range: a.rangeOf(8, 15)}
	if !ok || got != want {
		t.Fatalf("package location = %s, %v; want %s", got.URI, ok, want.URI)
	}
}
