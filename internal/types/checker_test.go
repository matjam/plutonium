package types_test

import (
	"testing"

	"github.com/matjam/plutonium/internal/ast"
	"github.com/matjam/plutonium/internal/parser"
	"github.com/matjam/plutonium/internal/token"
	"github.com/matjam/plutonium/internal/types"
)

func parse(t *testing.T, src string) (*token.FileSet, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.pu", []byte(src), 0)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	return fset, f
}

func checkOK(t *testing.T, src string) *types.Package {
	t.Helper()
	fset, f := parse(t, src)
	pkg, err := types.Check("test", fset, []*ast.File{f}, nil)
	if err != nil {
		t.Fatalf("unexpected type error: %v", err)
	}
	return pkg
}

func checkErr(t *testing.T, src string) string {
	t.Helper()
	fset, f := parse(t, src)
	_, err := types.Check("test", fset, []*ast.File{f}, nil)
	if err == nil {
		t.Fatal("expected type error, got none")
	}
	return err.Error()
}

func TestBasicArithmetic(t *testing.T) {
	checkOK(t, `
package main
func add(a, b int32) int32 { return a + b }
`)
}

func TestBasicTypes(t *testing.T) {
	checkOK(t, `
package main
var a int8
var b int16
var c int32
var d int64
var e uint8
var f uint16
var g uint32
var h uint64
var i float32
var j float64
var k bool
var l byte
var m rune
var n uintptr
`)
}

func TestPointerTypes(t *testing.T) {
	checkOK(t, `
package main
func inc(p *int32) { *p = *p + 1 }
`)
}

func TestStructType(t *testing.T) {
	checkOK(t, `
package main
type Point struct { X, Y int32 }
func newPoint(x, y int32) Point {
	return Point{X: x, Y: y}
}
`)
}

func TestSliceType(t *testing.T) {
	checkOK(t, `
package main
func head(xs []int32) int32 { return xs[0] }
`)
}

func TestMultipleReturnValues(t *testing.T) {
	checkOK(t, `
package main
func swap(a, b int32) (int32, int32) { return b, a }
`)
}

func TestIfStatement(t *testing.T) {
	checkOK(t, `
package main
func abs(n int32) int32 {
	if n < 0 { return -n }
	return n
}
`)
}

func TestForStatement(t *testing.T) {
	checkOK(t, `
package main
func sum(n int32) int32 {
	s := int32(0)
	for i := int32(0); i < n; i++ { s += i }
	return s
}
`)
}

func TestConstDecl(t *testing.T) {
	checkOK(t, `
package main
const MaxSize = int32(1024)
const Pi = 3
`)
}

func TestTypeConversion(t *testing.T) {
	checkOK(t, `
package main
func conv(x int64) int32 { return int32(x) }
`)
}

func TestImport(t *testing.T) {
	checkOK(t, `
package main
import _ "pluto/os"
`)
}

// --- Kernel-safety rejection tests ---

func TestRejectNew(t *testing.T) {
	msg := checkErr(t, `
package main
func foo() *int32 { return new(int32) }
`)
	if msg == "" {
		t.Error("expected error for new()")
	}
}

func TestRejectMakeSlice(t *testing.T) {
	msg := checkErr(t, `
package main
func foo() []int32 { return make([]int32, 10) }
`)
	if msg == "" {
		t.Error("expected error for make([]T, n)")
	}
}

func TestRejectAny(t *testing.T) {
	checkErr(t, `
package main
var x any
`)
}

func TestRejectInterfaceEmpty(t *testing.T) {
	checkErr(t, `
package main
var x interface{}
`)
}

func TestRejectPanic(t *testing.T) {
	checkErr(t, `
package main
func foo() { panic("oops") }
`)
}

func TestRejectRecover(t *testing.T) {
	checkErr(t, `
package main
func foo() { recover() }
`)
}

func TestRejectReflectImport(t *testing.T) {
	checkErr(t, `
package main
import "reflect"
func foo() { _ = reflect.TypeOf(nil) }
`)
}
