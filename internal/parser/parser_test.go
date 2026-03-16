package parser_test

import (
	"testing"

	"github.com/matjam/plutonium/internal/parser"
	"github.com/matjam/plutonium/internal/token"
)

func parseOK(t *testing.T, src string) {
	t.Helper()
	fset := token.NewFileSet()
	_, err := parser.ParseFile(fset, "test.pu", []byte(src), 0)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
}

func parseErr(t *testing.T, src string) {
	t.Helper()
	fset := token.NewFileSet()
	_, err := parser.ParseFile(fset, "test.pu", []byte(src), 0)
	if err == nil {
		t.Fatal("expected parse error, got none")
	}
}

func TestParseEmptyPackage(t *testing.T) {
	parseOK(t, `package main`)
}

func TestParseFuncDecl(t *testing.T) {
	parseOK(t, `
package main

func add(a, b int32) int32 {
	return a + b
}
`)
}

func TestParseMultipleReturnValues(t *testing.T) {
	parseOK(t, `
package main

func divmod(a, b int32) (int32, int32) {
	return a / b, a % b
}
`)
}

func TestParseNamedReturnValues(t *testing.T) {
	parseOK(t, `
package main

func divmod(a, b int32) (q, r int32) {
	q = a / b
	r = a % b
	return
}
`)
}

func TestParseMethod(t *testing.T) {
	parseOK(t, `
package main

type Point struct {
	X, Y int32
}

func (p *Point) Add(other Point) Point {
	return Point{X: p.X + other.X, Y: p.Y + other.Y}
}
`)
}

func TestParseVarDecl(t *testing.T) {
	parseOK(t, `
package main

var x int64
var y = int32(42)
var a, b int32 = 1, 2
`)
}

func TestParseConstDecl(t *testing.T) {
	parseOK(t, `
package main

const Pi = 3
const (
	A = iota
	B
	C
)
`)
}

func TestParseTypeDecl(t *testing.T) {
	parseOK(t, `
package main

type MyInt int32
type Point struct {
	X, Y int32
}
type Handler interface {
	Handle(n int32) int32
}
`)
}

func TestParseImport(t *testing.T) {
	parseOK(t, `
package main

import "pluto/os"
import (
	"pluto/io"
	"pluto/fmt"
)
`)
}

func TestParseIfElse(t *testing.T) {
	parseOK(t, `
package main

func abs(n int32) int32 {
	if n < 0 {
		return -n
	} else {
		return n
	}
}
`)
}

func TestParseForLoop(t *testing.T) {
	parseOK(t, `
package main

func sum(n int32) int32 {
	s := int32(0)
	for i := int32(0); i < n; i++ {
		s += i
	}
	return s
}
`)
}

func TestParseRangeLoop(t *testing.T) {
	parseOK(t, `
package main

func sumSlice(xs []int32) int32 {
	s := int32(0)
	for _, x := range xs {
		s += x
	}
	return s
}
`)
}

func TestParseInfiniteLoop(t *testing.T) {
	parseOK(t, `
package main

func spin() {
	for {
	}
}
`)
}

func TestParseSwitch(t *testing.T) {
	parseOK(t, `
package main

func classify(n int32) int32 {
	switch {
	case n < 0:
		return -1
	case n == 0:
		return 0
	default:
		return 1
	}
}
`)
}

func TestParseDefer(t *testing.T) {
	parseOK(t, `
package main

func foo() {
	defer cleanup()
}
`)
}

func TestParsePointerOps(t *testing.T) {
	parseOK(t, `
package main

func inc(p *int32) {
	*p = *p + 1
}
`)
}

func TestParseSlice(t *testing.T) {
	parseOK(t, `
package main

func head(xs []int32) int32 {
	return xs[0]
}
`)
}

func TestParseGenericFunc(t *testing.T) {
	parseOK(t, `
package main

func Max[T int32 | int64](a, b T) T {
	if a > b {
		return a
	}
	return b
}
`)
}

func TestParseGenericStruct(t *testing.T) {
	parseOK(t, `
package main

type Stack[T any] struct {
	data []T
	top  int32
}
`)
}

func TestParseAssemblyStub(t *testing.T) {
	parseOK(t, `
package main

func cpuid(leaf uint32) (eax, ebx, ecx, edx uint32)
`)
}

func TestParseUnsafe(t *testing.T) {
	parseOK(t, `
package main

import "unsafe"

func sizeOf() uintptr {
	return unsafe.Sizeof(int32(0))
}
`)
}

func TestParseDirective(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.pu", []byte(`
//pu:build amd64
package main
`), 0)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(f.Directives) == 0 {
		t.Error("expected directives on file")
	}
	if f.Directives[0].Text != "build amd64" {
		t.Errorf("got directive %q, want %q", f.Directives[0].Text, "build amd64")
	}
}

// --- Forbidden constructs ---

func TestForbiddenGo(t *testing.T) {
	parseErr(t, `
package main
func foo() { go bar() }
`)
}

func TestForbiddenChan(t *testing.T) {
	parseErr(t, `
package main
var c chan int32
`)
}

func TestForbiddenSelect(t *testing.T) {
	parseErr(t, `
package main
func foo() { select {} }
`)
}

func TestForbiddenMapType(t *testing.T) {
	parseErr(t, `
package main
var m map[string]int32
`)
}

// --- Packed types ---

func TestParsePackedType(t *testing.T) {
	parseOK(t, `
package main

type ControlReg packed uint32 {
	mode     bits[uint, 4]
	enable   bits[bool, 1]
	priority bits[uint, 3]
}
`)
}

func TestParsePackedTypeUint8(t *testing.T) {
	parseOK(t, `
package main

type Flags packed uint8 {
	a bits[bool, 1]
	b bits[bool, 1]
	c bits[uint, 6]
}
`)
}

func TestParsePackedTypeUint64(t *testing.T) {
	parseOK(t, `
package main

type BigReg packed uint64 {
	low  bits[uint, 32]
	high bits[uint, 32]
}
`)
}

func TestParsePackedVarDecl(t *testing.T) {
	parseOK(t, `
package main

type R packed uint32 {
	x bits[uint, 8]
	y bits[uint, 8]
}

func main() int64 {
	var r R
	r.x = 42
	r.y = 99
	return 0
}
`)
}

func TestParseRingType(t *testing.T) {
	parseOK(t, `
package main

func main() int64 {
	var r ring[int64, 8]
	r.Push(10)
	r.Push(20)
	val := r.Pop()
	n := r.Len()
	_ = val
	_ = n
	return 0
}
`)
}

func TestParseBitsLensType(t *testing.T) {
	parseOK(t, `
package main

func main() int64 {
	var flags uint64
	var b bits[flags]
	b.Set(0)
	b.Set(3)
	b.Clear(3)
	t := b.Test(7)
	b.Toggle(0)
	_ = t
	return 0
}
`)
}
