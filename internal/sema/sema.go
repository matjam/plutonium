// Package sema implements a kernel-safety validation pass for Plutonium.
// This is a distinct, auditable pass separate from the type checker so that
// kernel-safety rules are easy to find, test, and audit in isolation.
package sema

import (
	"fmt"

	"github.com/matjam/plutonium/internal/ast"
	"github.com/matjam/plutonium/internal/token"
	"github.com/matjam/plutonium/internal/types"
)

// Check performs kernel-safety validation on a type-checked package.
// It walks all AST files and applies the kernel-safety rules as an
// auditable, explicit pass separate from the type checker.
func Check(pkg *types.Package, fset *token.FileSet) error {
	v := &validator{fset: fset, errors: nil}
	for _, file := range pkg.Files() {
		ast.Walk(v, file)
	}
	if len(v.errors) > 0 {
		return v.errors
	}
	return nil
}

type validator struct {
	fset   *token.FileSet
	errors errorList
}

func (v *validator) errorf(pos token.Pos, format string, args ...interface{}) {
	v.errors.add(v.fset.Position(pos), fmt.Sprintf(format, args...))
}

func (v *validator) Visit(node ast.Node) ast.Visitor {
	if node == nil {
		return nil
	}
	switch n := node.(type) {
	case *ast.GoStmt:
		v.errorf(n.Go, "goroutines are not permitted in Plutonium ('go' statement is forbidden)")
		return nil // don't recurse further

	case *ast.SelectStmt:
		v.errorf(n.Select, "select statements are not permitted in Plutonium (requires runtime scheduler)")
		return nil

	case *ast.SendStmt:
		v.errorf(n.Arrow, "channel send is not permitted in Plutonium (channels are forbidden)")
		return nil

	case *ast.UnaryExpr:
		if n.Op == token.ARROW {
			v.errorf(n.OpPos, "channel receive is not permitted in Plutonium (channels are forbidden)")
		}

	case *ast.CallExpr:
		v.checkCall(n)

	case *ast.InterfaceType:
		// Empty interface{} is forbidden
		if n.Methods == nil || len(n.Methods.List) == 0 {
			v.errorf(n.Interface, "empty interface{} is not permitted in Plutonium (no runtime type metadata)")
		}

	case *ast.TypeAssertExpr:
		v.errorf(n.X.Pos(), "type assertions are not permitted in Plutonium (requires runtime type metadata)")

	case *ast.ImportSpec:
		path := n.Path.Value
		if len(path) >= 2 {
			path = path[1 : len(path)-1]
		}
		if path == "reflect" {
			v.errorf(n.Path.Pos(), "import of 'reflect' is forbidden in Plutonium (requires runtime metadata)")
		}
	}
	return v
}

func (v *validator) checkCall(e *ast.CallExpr) {
	ident, ok := e.Fun.(*ast.Ident)
	if !ok {
		return
	}
	switch ident.Name {
	case "new":
		v.errorf(e.Pos(), "'new' is forbidden in Plutonium (no implicit heap allocation); use stack allocation or an explicit allocator")
	case "make":
		v.errorf(e.Pos(), "'make' is forbidden in Plutonium (no implicit heap allocation); use explicit allocators from pluto/mem")
	case "panic":
		v.errorf(e.Pos(), "'panic' is forbidden in Plutonium (no runtime unwinder)")
	case "recover":
		v.errorf(e.Pos(), "'recover' is forbidden in Plutonium (no runtime unwinder)")
	case "delete":
		v.errorf(e.Pos(), "'delete' is forbidden in Plutonium (no built-in maps; use pluto/hash)")
	case "close":
		v.errorf(e.Pos(), "'close' is forbidden in Plutonium (channels are not supported)")
	}
}

type errorList []semaError

type semaError struct {
	pos token.Position
	msg string
}

func (el errorList) Error() string {
	if len(el) == 0 {
		return "no errors"
	}
	if len(el) == 1 {
		return fmt.Sprintf("%s: %s", el[0].pos, el[0].msg)
	}
	return fmt.Sprintf("%s: %s (and %d more errors)", el[0].pos, el[0].msg, len(el)-1)
}

func (el *errorList) add(pos token.Position, msg string) {
	*el = append(*el, semaError{pos: pos, msg: msg})
}
