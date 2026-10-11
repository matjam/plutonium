package lower

import (
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/parser"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// TestBlockingOps checks which channel operations and go statements the
// interrupt-context check finds in a function body. Lowering does not
// support channels and go statements yet, so this checks the scan
// directly on type-checked source.
func TestBlockingOps(t *testing.T) {
	const src = `package main

var work = make(chan int, 4)
var done chan struct{}

func send()     { work <- 1 }
func recv()     { <-work }
func recvOK()   { v, ok := <-work; _, _ = v, ok }
func selWait()  { select { case work <- 1: case <-done: } }
func selNoWait() { select { case work <- 1: default: } }
func selBody()  { select { case v := <-work: work <- v; default: } }
func rangeCh()  { for v := range work { _ = v } }
func rangeArr() { for v := range [3]int{} { _ = v } }
func spawn()    { task send() }
func literal()  { f := func() { work <- 1 }; _ = f }
func plain()    { println(len(work)) }

func main() {}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.p", []byte(src), 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types2.Info{Types: map[ast.Expr]types2.TypeAndValue{}}
	// The checker reports channels and go statements until puroutines
	// exist; it records their types all the same.
	conf := types2.Config{Error: func(error) {}}
	_, _ = conf.Check("main", fset, []*ast.File{file}, info)
	want := map[string]string{
		"send":      "sends on work",
		"recv":      "receives from work",
		"recvOK":    "receives from work",
		"selWait":   "waits in a select with no default case",
		"selNoWait": "",
		"selBody":   "sends on work",
		"rangeCh":   "ranges over channel work",
		"rangeArr":  "",
		"spawn":     "starts a puroutine",
		"literal":   "",
		"plain":     "",
	}
	for _, f := range []*ast.File{file} {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Name.Name == "main" {
				continue
			}
			ops := blockingOps(funcBody{body: fd.Body, info: info})
			var got []string
			for _, op := range ops {
				got = append(got, op.what)
				if (op.kind == opSpawn) != (op.what == "starts a puroutine") {
					t.Errorf("%s: %q has kind %d", fd.Name.Name, op.what, op.kind)
				}
			}
			if w := want[fd.Name.Name]; strings.Join(got, "; ") != w {
				t.Errorf("%s: got %q, want %q", fd.Name.Name, got, w)
			}
		}
	}
}
