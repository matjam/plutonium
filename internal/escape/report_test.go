package escape

import (
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// TestHandOffMessages checks the messages for views passed to a go
// statement or sent on a channel. The type checker rejects both until the
// scheduler lands, so they are built here directly.
func TestHandOffMessages(t *testing.T) {
	fset := token.NewFileSet()
	f := fset.AddFile("srv.p", -1, 1000)
	for i := 0; i < 40; i++ {
		f.AddLine(i * 20)
	}
	pos := func(line int) token.Pos { return f.LineStart(line) }
	pkg := types2.NewPackage("main", "main")
	req := types2.NewVar(pos(24), pkg, "req", types2.Typ[types2.Int])
	a := &analyzer{fset: fset, types: map[ast.Expr]types2.TypeAndValue{}}
	l := &loc{kind: lkVar, v: req, fn: "serve", pos: req.Pos()}
	addr := &ast.UnaryExpr{OpPos: pos(30), Op: token.AND, X: &ast.Ident{NamePos: pos(30), Name: "req"}}
	call := &ast.CallExpr{Fun: &ast.Ident{Name: "handle"}, Args: []ast.Expr{addr}}

	full := func(d *diag.Diagnostic) string {
		return strings.Join(strings.Fields(d.Msg+" "+diag.Explanation(d)), " ")
	}
	goErr := a.viewDiag(&viewErr{sink: sinkGo, pos: pos(30), expr: addr, l: l, fn: "serve", call: call, qual: pkg})
	for _, want := range []string{
		"cannot pass &req to a task statement: the new task may outlive req",
		"req is a local variable of serve (declared at srv.p:24)",
		"may still be running after serve returns",
		"Pass the value itself. This copies it, or moves it if it is an owner: task handle(req)",
		"E3004",
	} {
		if got := full(goErr); !strings.Contains(got, want) {
			t.Errorf("task statement: %q not in\n%s", want, got)
		}
	}
	sendErr := a.viewDiag(&viewErr{sink: sinkSend, pos: pos(30), expr: addr, l: l, fn: "serve", ch: &ast.Ident{Name: "ch"}, qual: pkg})
	for _, want := range []string{
		"cannot send &req on ch: the receiver may outlive req",
		"may be received after serve returns",
		"ch <- req",
		"E3005",
	} {
		if got := full(sendErr); !strings.Contains(got, want) {
			t.Errorf("send: %q not in\n%s", want, got)
		}
	}
	// A view that is not &x gets advice without code.
	other := a.viewDiag(&viewErr{sink: sinkSend, pos: pos(30), expr: &ast.Ident{Name: "p"}, l: l, fn: "serve", ch: &ast.Ident{Name: "ch"}, qual: pkg})
	if got := full(other); !strings.Contains(got, "p points into req.") || !strings.Contains(got, "Pass a value, which is copied") {
		t.Errorf("send of p: %s", got)
	}
}
