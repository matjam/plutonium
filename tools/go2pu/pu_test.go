package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// TestPuMirrorsLibrary checks that internal/pu and the Plutonium library
// match name for name: every exported name of internal/pu has a puNames
// entry, every entry names something internal/pu exports, and every method
// of a pu type exists on the library type it translates to, so that
// go2pu's translation of code using internal/pu refers only to names the
// library has.
func TestPuMirrorsLibrary(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "../../internal/pu/pu.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	methods := map[string][]string{} // type -> methods
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if !d.Name.IsExported() {
				continue
			}
			if d.Recv == nil {
				names[d.Name.Name] = true
				continue
			}
			recv := d.Recv.List[0].Type
			if s, ok := recv.(*ast.StarExpr); ok {
				recv = s.X
			}
			switch r := recv.(type) {
			case *ast.IndexExpr:
				recv = r.X
			case *ast.IndexListExpr:
				recv = r.X
			}
			typ := recv.(*ast.Ident).Name
			methods[typ] = append(methods[typ], d.Name.Name)
		case *ast.GenDecl:
			for _, s := range d.Specs {
				if ts, ok := s.(*ast.TypeSpec); ok && ts.Name.IsExported() {
					names[ts.Name.Name] = true
				}
			}
		}
	}
	for name := range names {
		if _, ok := puNames[name]; !ok {
			t.Errorf("pu.%s has no entry in puNames", name)
		}
	}
	exports := plutoExports("../../lib")
	for name, r := range puNames {
		if !names[name] {
			t.Errorf("puNames has %s, which internal/pu does not export", name)
		}
		if !strings.HasPrefix(r.pkg, "pluto/") {
			continue
		}
		ex := exports[r.pkg]
		if !ex[r.name] {
			t.Errorf("pu.%s translates to %s.%s, which the library does not have", name, r.pkg, r.name)
		}
		for _, m := range methods[name] {
			if !ex[r.name+"."+m] {
				t.Errorf("pu.%s.%s: %s.%s has no method %s", name, m, r.pkg, r.name, m)
			}
		}
	}
	if len(methods["Vec"]) == 0 || len(methods["Map"]) == 0 || len(methods["Buf"]) == 0 {
		t.Fatalf("found no methods: %v", methods)
	}
}

// TestPuTranslation checks the translation of each kind of internal/pu
// name, and the note for a name that has no counterpart.
func TestPuTranslation(t *testing.T) {
	res, err := TranslateDir(testConfig(), "testdata/pudemo")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Notes) != 0 {
		t.Errorf("notes: %v", res.Notes)
	}
	src := string(res.Files[0].Src)
	for _, want := range []string{
		"vec.Vec[*node]", "hash.Map[string, int]", "hash.Set[int]", "*strings.Builder", "vec.New[int]()",
		"vec.WithCapacity[string](4)", "hash.NewMap[int, int](8)", "hash.NewSet[string](4)",
		"pu.New(node{name: name})", "pu.Sprintf(", `"selfhost/pu"`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("translation lacks %q", want)
		}
	}
	if strings.Contains(src, `internal/pu"`) {
		t.Error("translation still imports internal/pu")
	}
}
