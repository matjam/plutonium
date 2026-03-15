package mono_test

import (
	"testing"

	"github.com/matjam/plutonium/internal/ast"
	"github.com/matjam/plutonium/internal/mono"
	"github.com/matjam/plutonium/internal/parser"
	"github.com/matjam/plutonium/internal/token"
	"github.com/matjam/plutonium/internal/types"
)

func monoOK(t *testing.T, src string) *mono.Result {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.pu", []byte(src), 0)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	pkg, err := types.Check("test", fset, []*ast.File{f}, nil, nil)
	if err != nil {
		t.Fatalf("type error: %v", err)
	}
	result, err := mono.Monomorphize(pkg, fset)
	if err != nil {
		t.Fatalf("mono error: %v", err)
	}
	return result
}

func TestNonGenericPassthrough(t *testing.T) {
	result := monoOK(t, `
package main
func add(a, b int32) int32 { return a + b }
`)
	// Should have exactly 1 function: add
	if len(result.Functions) != 1 {
		t.Errorf("got %d functions, want 1", len(result.Functions))
	}
	if result.Functions[0].Name != "add" {
		t.Errorf("got function %q, want %q", result.Functions[0].Name, "add")
	}
}

func TestGenericFuncMonomorphization(t *testing.T) {
	result := monoOK(t, `
package main

func Max[T int32 | int64](a, b T) T {
	if a > b { return a }
	return b
}

func useMax() {
	_ = Max[int32](1, 2)
	_ = Max[int64](3, 4)
}
`)
	// Should have: Max__int32, Max__int64, useMax
	names := make(map[string]bool)
	for _, fn := range result.Functions {
		names[fn.Name] = true
	}
	if !names["Max__int32"] {
		t.Error("expected Max__int32 in output")
	}
	if !names["Max__int64"] {
		t.Error("expected Max__int64 in output")
	}
	if !names["useMax"] {
		t.Error("expected useMax in output")
	}
	// The original generic Max should NOT be in output
	if names["Max"] {
		t.Error("original generic Max should not be in output")
	}
}

func TestMethodQualifiedName(t *testing.T) {
	result := monoOK(t, `
package main

type Counter struct {
	Value int32
}

func (c *Counter) Inc() {
	c.Value = c.Value + 1
}

func main() {
	c := Counter{Value: 0}
	_ = c
}
`)
	names := make(map[string]bool)
	for _, fn := range result.Functions {
		names[fn.Name] = true
	}
	if !names["Counter.Inc"] {
		t.Errorf("expected Counter.Inc in output, got functions: %v", names)
	}
	// The bare name "Inc" should NOT be present
	if names["Inc"] {
		t.Error("method should be named Counter.Inc, not Inc")
	}
}

func TestNoTypeParamsAfterMono(t *testing.T) {
	result := monoOK(t, `
package main
func Identity[T int32 | int64](x T) T { return x }
func use() { _ = Identity[int32](42) }
`)
	for _, fn := range result.Functions {
		if fn.Decl.Type.TypeParams != nil && len(fn.Decl.Type.TypeParams.List) > 0 {
			t.Errorf("function %q still has type params after monomorphization", fn.Name)
		}
	}
}

func TestConstGenericParam(t *testing.T) {
	result := monoOK(t, `
package main
func fixed[N int]() int64 { return N }
func use() { _ = fixed[8]() }
`)
	// Should produce fixed__8 and use
	names := make(map[string]bool)
	for _, fn := range result.Functions {
		names[fn.Name] = true
	}
	if !names["fixed__8"] {
		t.Errorf("expected fixed__8 in output, got functions: %v", names)
	}
	if !names["use"] {
		t.Error("expected use in output")
	}
	// The original generic fixed should NOT be in output
	if names["fixed"] {
		t.Error("original generic fixed should not be in output")
	}
}

func TestConstGenericBodySubstitution(t *testing.T) {
	result := monoOK(t, `
package main
func fixed[N int]() int64 { return N }
func use() { _ = fixed[8]() }
`)
	// Find the fixed__8 function
	var fixedDecl *ast.FuncDecl
	for _, fn := range result.Functions {
		if fn.Name == "fixed__8" {
			fixedDecl = fn.Decl
			break
		}
	}
	if fixedDecl == nil {
		t.Fatal("fixed__8 not found in output")
	}
	// The body should contain a return statement with BasicLit "8", not Ident "N"
	if fixedDecl.Body == nil || len(fixedDecl.Body.List) == 0 {
		t.Fatal("fixed__8 has no body statements")
	}
	retStmt, ok := fixedDecl.Body.List[0].(*ast.ReturnStmt)
	if !ok {
		t.Fatalf("expected ReturnStmt, got %T", fixedDecl.Body.List[0])
	}
	if len(retStmt.Results) != 1 {
		t.Fatalf("expected 1 return result, got %d", len(retStmt.Results))
	}
	lit, ok := retStmt.Results[0].(*ast.BasicLit)
	if !ok {
		t.Fatalf("expected return value to be *ast.BasicLit, got %T", retStmt.Results[0])
	}
	if lit.Value != "8" {
		t.Errorf("expected return value %q, got %q", "8", lit.Value)
	}
	if lit.Kind != token.INT {
		t.Errorf("expected token.INT, got %v", lit.Kind)
	}
}
