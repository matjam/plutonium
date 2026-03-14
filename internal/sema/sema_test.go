package sema_test

import (
	"testing"

	"github.com/matjam/plutonium/internal/ast"
	"github.com/matjam/plutonium/internal/parser"
	"github.com/matjam/plutonium/internal/sema"
	"github.com/matjam/plutonium/internal/token"
	"github.com/matjam/plutonium/internal/types"
)

func validate(t *testing.T, src string) error {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.pu", []byte(src), 0)
	if err != nil {
		// parser already caught it
		return err
	}
	pkg, err := types.Check("test", fset, []*ast.File{f}, nil)
	if err != nil {
		return err
	}
	return sema.Check(pkg, fset)
}

func validateOK(t *testing.T, src string) {
	t.Helper()
	if err := validate(t, src); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func validateErr(t *testing.T, src string) {
	t.Helper()
	if err := validate(t, src); err == nil {
		t.Fatal("expected error, got none")
	}
}

func TestValidBasicProgram(t *testing.T) {
	validateOK(t, `
package main
func add(a, b int32) int32 { return a + b }
`)
}

func TestValidPointers(t *testing.T) {
	validateOK(t, `
package main
func inc(p *int32) { *p = *p + 1 }
`)
}

func TestValidSlice(t *testing.T) {
	validateOK(t, `
package main
func head(xs []int32) int32 { return xs[0] }
`)
}

func TestValidDefer(t *testing.T) {
	validateOK(t, `
package main
func foo() { defer bar() }
func bar() {}
`)
}

func TestRejectGoStmt(t *testing.T) {
	// Parser catches this, sema should also catch it
	// The parse error means we test via validate() directly
	err := validate(t, `
package main
func foo() { go bar() }
func bar() {}
`)
	if err == nil {
		t.Error("expected error for go statement")
	}
}

func TestRejectSelectStmt(t *testing.T) {
	err := validate(t, `
package main
func foo() { select {} }
`)
	if err == nil {
		t.Error("expected error for select statement")
	}
}

func TestRejectPanic(t *testing.T) {
	validateErr(t, `
package main
func foo() { panic("bad") }
`)
}

func TestRejectNew(t *testing.T) {
	validateErr(t, `
package main
func foo() *int32 { return new(int32) }
`)
}

func TestRejectMake(t *testing.T) {
	validateErr(t, `
package main
func foo() []int32 { return make([]int32, 10) }
`)
}

func TestRejectAny(t *testing.T) {
	validateErr(t, `
package main
var x any
`)
}

func TestRejectEmptyInterface(t *testing.T) {
	validateErr(t, `
package main
var x interface{}
`)
}
