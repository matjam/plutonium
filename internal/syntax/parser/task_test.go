package parser

import (
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/scanner"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/testguard"
)

// TestParseTaskStmt checks the rule for the contextual keyword task
// (language spec 4.6): at the start of a statement, task followed by an
// identifier or func begins a task statement, and in every other position
// it is an identifier.
func TestParseTaskStmt(t *testing.T) {
	testguard.Parallel(t)
	src := `package p

func f(q *queue, ch chan int) {
	task worker(jobs)
	task func() { work() }()
	task q.Serve()
	task Pool[int](n)
	task := q.Pop()
	task.Run()
	task = next
	task[i] = v
	task++
	task <- 1
	task(1, 2)
	task.f(3)
	task, ok := <-ch
	task +=
		2
	task:
	for {
		break task
	}
	var x = task
	_ = x
}
`
	fset := token.NewFileSet()
	file, err := ParseFile(fset, "task.p", src, SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	body := file.Decls[0].(*ast.FuncDecl).Body.List
	type kind func(ast.Stmt) bool
	isTask := func(s ast.Stmt) bool { ts, ok := s.(*ast.TaskStmt); return ok && !ts.Go }
	isExpr := func(s ast.Stmt) bool { _, ok := s.(*ast.ExprStmt); return ok }
	isAssign := func(s ast.Stmt) bool { _, ok := s.(*ast.AssignStmt); return ok }
	isIncDec := func(s ast.Stmt) bool { _, ok := s.(*ast.IncDecStmt); return ok }
	isSend := func(s ast.Stmt) bool { _, ok := s.(*ast.SendStmt); return ok }
	isLabel := func(s ast.Stmt) bool { _, ok := s.(*ast.LabeledStmt); return ok }
	want := []kind{
		isTask, isTask, isTask, isTask, // the four task statements
		isAssign, isExpr, isAssign, isAssign, isIncDec, isSend, isExpr, isExpr, isAssign, isAssign, isLabel,
	}
	if len(body) < len(want) {
		t.Fatalf("got %d statements, want at least %d", len(body), len(want))
	}
	for i, k := range want {
		if !k(body[i]) {
			t.Errorf("statement %d is a %T, not the kind expected", i, body[i])
		}
	}
	// A task statement's call is the rest of the statement.
	if c := body[0].(*ast.TaskStmt).Call; c.Fun.(*ast.Ident).Name != "worker" {
		t.Errorf("first task statement calls %v, want worker", c.Fun)
	}
	if _, ok := body[1].(*ast.TaskStmt).Call.Fun.(*ast.FuncLit); !ok {
		t.Errorf("second task statement does not call a function literal")
	}
	// The identifier keeps its position and name.
	as := body[4].(*ast.AssignStmt)
	if id, ok := as.Lhs[0].(*ast.Ident); !ok || id.Name != "task" || fset.Position(id.Pos()).Line != 8 {
		t.Errorf("task := q.Pop(): left side is %v", as.Lhs[0])
	}
}

// TestParseGoStmt checks that a go statement is an error that names task,
// and that it still parses as a task statement so that nothing else is
// reported for it.
func TestParseGoStmt(t *testing.T) {
	testguard.Parallel(t)
	src := "package p\n\nfunc f() {\n\tgo worker(jobs)\n\tgo func() {\n\t}()\n}\n"
	file, err := ParseFile(token.NewFileSet(), "go.p", src, SkipObjectResolution)
	if err == nil {
		t.Fatal("go statement accepted")
	}
	var got []string
	for _, e := range err.(scanner.ErrorList) {
		got = append(got, e.Error())
	}
	want := []string{
		"go.p:4:2: go starts no task here; write: task worker(jobs)",
		"go.p:5:2: go starts no task here; write task in place of go",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("errors:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if n := len(file.Decls[0].(*ast.FuncDecl).Body.List); n != 2 {
		t.Errorf("got %d statements, want 2", n)
	}
	// Tools that read Go source ask for the statements to be accepted.
	file, err = ParseFile(token.NewFileSet(), "go.p", src, SkipObjectResolution|AllowGoStmt)
	if err != nil {
		t.Fatal(err)
	}
	if g, ok := file.Decls[0].(*ast.FuncDecl).Body.List[0].(*ast.TaskStmt); !ok || !g.Go {
		t.Errorf("with AllowGoStmt the statement is %T, want a TaskStmt with Go set", file.Decls[0].(*ast.FuncDecl).Body.List[0])
	}
	// go cannot name a variable.
	if _, err := ParseFile(token.NewFileSet(), "v.p", "package p\n\nfunc f() {\n\tgo := 1\n}\n", SkipObjectResolution); err == nil {
		t.Error("go accepted as a variable name")
	}
}
