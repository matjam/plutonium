package ast_test

import (
	"testing"

	"github.com/matjam/plutonium/internal/ast"
	"github.com/matjam/plutonium/internal/token"
)

func TestNodeInterfaces(t *testing.T) {
	var _ ast.Node = (*ast.File)(nil)
	var _ ast.Node = (*ast.FuncDecl)(nil)
	var _ ast.Node = (*ast.BasicLit)(nil)
	var _ ast.Node = (*ast.Ident)(nil)
	var _ ast.Expr = (*ast.BinaryExpr)(nil)
	var _ ast.Expr = (*ast.UnaryExpr)(nil)
	var _ ast.Expr = (*ast.CallExpr)(nil)
	var _ ast.Expr = (*ast.IndexExpr)(nil)
	var _ ast.Expr = (*ast.SliceExpr)(nil)
	var _ ast.Expr = (*ast.SelectorExpr)(nil)
	var _ ast.Expr = (*ast.StarExpr)(nil)
	var _ ast.Expr = (*ast.CompositeLit)(nil)
	var _ ast.Expr = (*ast.FuncLit)(nil)
	var _ ast.Expr = (*ast.ParenExpr)(nil)
	var _ ast.Expr = (*ast.TypeAssertExpr)(nil)
	var _ ast.Expr = (*ast.KeyValueExpr)(nil)
	var _ ast.Expr = (*ast.ArrayType)(nil)
	var _ ast.Expr = (*ast.StructType)(nil)
	var _ ast.Expr = (*ast.FuncType)(nil)
	var _ ast.Expr = (*ast.InterfaceType)(nil)
	var _ ast.Expr = (*ast.PointerType)(nil)
	var _ ast.Expr = (*ast.PackedType)(nil)
	var _ ast.Expr = (*ast.BitsType)(nil)
	var _ ast.Stmt = (*ast.ReturnStmt)(nil)
	var _ ast.Stmt = (*ast.BlockStmt)(nil)
	var _ ast.Stmt = (*ast.IfStmt)(nil)
	var _ ast.Stmt = (*ast.ForStmt)(nil)
	var _ ast.Stmt = (*ast.RangeStmt)(nil)
	var _ ast.Stmt = (*ast.AssignStmt)(nil)
	var _ ast.Stmt = (*ast.ExprStmt)(nil)
	var _ ast.Stmt = (*ast.IncDecStmt)(nil)
	var _ ast.Stmt = (*ast.DeferStmt)(nil)
	var _ ast.Stmt = (*ast.BranchStmt)(nil)
	var _ ast.Stmt = (*ast.SwitchStmt)(nil)
	var _ ast.Stmt = (*ast.CaseClause)(nil)
	var _ ast.Stmt = (*ast.LabeledStmt)(nil)
	var _ ast.Stmt = (*ast.EmptyStmt)(nil)
	var _ ast.Stmt = (*ast.DeclStmt)(nil)
	var _ ast.Stmt = (*ast.BadStmt)(nil)
	// Forbidden but parsed
	var _ ast.Stmt = (*ast.GoStmt)(nil)
	var _ ast.Stmt = (*ast.SelectStmt)(nil)
	var _ ast.Stmt = (*ast.SendStmt)(nil)
	var _ ast.Decl = (*ast.FuncDecl)(nil)
	var _ ast.Decl = (*ast.GenDecl)(nil)
	var _ ast.Decl = (*ast.BadDecl)(nil)
}

func TestIdentCreation(t *testing.T) {
	id := &ast.Ident{NamePos: token.Pos(1), Name: "myVar"}
	if id.Name != "myVar" {
		t.Errorf("got %q, want %q", id.Name, "myVar")
	}
	if id.Pos() != token.Pos(1) {
		t.Errorf("got pos %v, want 1", id.Pos())
	}
	if id.End() != token.Pos(1+len("myVar")) {
		t.Errorf("got end %v, want %v", id.End(), token.Pos(1+len("myVar")))
	}
}

func TestFieldListNumFields(t *testing.T) {
	fl := &ast.FieldList{
		List: []*ast.Field{
			{Names: []*ast.Ident{{Name: "a"}, {Name: "b"}}, Type: &ast.Ident{Name: "int32"}},
			{Names: nil, Type: &ast.Ident{Name: "MyEmbed"}}, // anonymous
		},
	}
	if got := fl.NumFields(); got != 3 {
		t.Errorf("NumFields: got %d, want 3", got)
	}
}

func TestScopeInsertLookup(t *testing.T) {
	s := ast.NewScope(nil)
	obj := &ast.Object{Kind: ast.Var, Name: "x"}
	if alt := s.Insert(obj); alt != nil {
		t.Error("expected nil for first insert")
	}
	if got := s.Lookup("x"); got != obj {
		t.Error("Lookup did not return inserted object")
	}
	// duplicate insert returns the existing one
	obj2 := &ast.Object{Kind: ast.Var, Name: "x"}
	if alt := s.Insert(obj2); alt != obj {
		t.Error("expected existing obj on duplicate insert")
	}
}

func TestWalkVisitsNodes(t *testing.T) {
	var visited []string
	v := &testVisitor{visited: &visited}

	file := &ast.File{
		Package: token.Pos(1),
		Name:    &ast.Ident{NamePos: token.Pos(9), Name: "main"},
		Decls:   []ast.Decl{},
	}
	ast.Walk(v, file)

	// Should have visited the File and then the Ident
	found := false
	for _, name := range visited {
		if name == "main" {
			found = true
		}
	}
	if !found {
		t.Errorf("Walk did not visit Ident 'main'; visited: %v", visited)
	}
}

type testVisitor struct {
	visited *[]string
}

func (v *testVisitor) Visit(node ast.Node) ast.Visitor {
	if node == nil {
		return nil
	}
	if id, ok := node.(*ast.Ident); ok {
		*v.visited = append(*v.visited, id.Name)
	}
	return v
}
