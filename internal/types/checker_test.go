package types_test

import (
	"os"
	"path/filepath"
	"strings"
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
	pkg, err := types.Check("test", fset, []*ast.File{f}, nil, nil)
	if err != nil {
		t.Fatalf("unexpected type error: %v", err)
	}
	return pkg
}

func checkErr(t *testing.T, src string) string {
	t.Helper()
	fset, f := parse(t, src)
	_, err := types.Check("test", fset, []*ast.File{f}, nil, nil)
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

func TestUnsafePointer(t *testing.T) {
	checkOK(t, `
package main

import "unsafe"

func ptrToUintptr(p *int32) uintptr {
	return uintptr(unsafe.Pointer(p))
}
`)
}

func TestUnsafeSizeof(t *testing.T) {
	checkOK(t, `
package main

import "unsafe"

func sizeOfInt32() uintptr {
	return unsafe.Sizeof(int32(0))
}
`)
}

// --- Import resolution tests ---

// stdlibRoot returns the path to the pluto stdlib, or "" if not found.
func stdlibRoot() string {
	// From the test file at internal/types/, go up 3 levels to repo root
	candidates := []string{
		filepath.Join("..", "..", "pluto"),
		filepath.Join("..", "..", "..", "pluto"),
	}
	for _, c := range candidates {
		abs, err := filepath.Abs(c)
		if err != nil {
			continue
		}
		if info, err := os.Stat(abs); err == nil && info.IsDir() {
			return abs
		}
	}
	return ""
}

func TestImportResolution_SyscallPackage(t *testing.T) {
	root := stdlibRoot()
	if root == "" {
		t.Skip("pluto stdlib not found")
	}
	t.Cleanup(types.ResetPackageCache)

	fset := token.NewFileSet()
	src := `
package main

import "pluto/syscall"

func callExit() {
	syscall.Syscall(syscall.SYS_EXIT_GROUP, 0, 0, 0)
}
`
	f, err := parser.ParseFile(fset, "test.pu", []byte(src), 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	loadCfg := &types.LoadConfig{
		StdlibRoot: root,
		Tags:       map[string]bool{"amd64": true, "linux": true},
		Fset:       fset,
	}

	_, err = types.Check("main", fset, []*ast.File{f}, nil, loadCfg)
	if err != nil {
		t.Fatalf("type check with import: %v", err)
	}
}

func TestImportResolution_SyscallConstAccess(t *testing.T) {
	root := stdlibRoot()
	if root == "" {
		t.Skip("pluto stdlib not found")
	}
	t.Cleanup(types.ResetPackageCache)

	fset := token.NewFileSet()
	src := `
package main

import "pluto/syscall"

func getSysWrite() uintptr {
	return syscall.SYS_WRITE
}
`
	f, err := parser.ParseFile(fset, "test.pu", []byte(src), 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	loadCfg := &types.LoadConfig{
		StdlibRoot: root,
		Tags:       map[string]bool{"amd64": true, "linux": true},
		Fset:       fset,
	}

	info := types.NewInfo()
	_, err = types.Check("main", fset, []*ast.File{f}, info, loadCfg)
	if err != nil {
		t.Fatalf("type check with import: %v", err)
	}

	// Verify that the selector expr resolved to an object
	found := false
	for ident, obj := range info.Uses {
		if ident.Name == "SYS_WRITE" && obj != nil {
			found = true
		}
	}
	if !found {
		t.Error("expected SYS_WRITE to be resolved in Uses map")
	}
}

func TestImportResolution_SyscallTypeConversion(t *testing.T) {
	root := stdlibRoot()
	if root == "" {
		t.Skip("pluto stdlib not found")
	}
	t.Cleanup(types.ResetPackageCache)

	fset := token.NewFileSet()
	// Test calling a type conversion on an imported named type
	// The parser supports pkg.Type(expr) as a call expression with SelectorExpr
	src := `
package main

import "pluto/syscall"

func makeErrno() {
	e := syscall.Errno(42)
	_ = e
}
`
	f, err := parser.ParseFile(fset, "test.pu", []byte(src), 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	loadCfg := &types.LoadConfig{
		StdlibRoot: root,
		Tags:       map[string]bool{"amd64": true, "linux": true},
		Fset:       fset,
	}

	_, err = types.Check("main", fset, []*ast.File{f}, nil, loadCfg)
	if err != nil {
		t.Fatalf("type check with import: %v", err)
	}
}

func TestImportResolution_UnknownPackage(t *testing.T) {
	root := stdlibRoot()
	if root == "" {
		t.Skip("pluto stdlib not found")
	}
	t.Cleanup(types.ResetPackageCache)

	fset := token.NewFileSet()
	src := `
package main

import "pluto/nonexistent"

func foo() {}
`
	f, err := parser.ParseFile(fset, "test.pu", []byte(src), 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	loadCfg := &types.LoadConfig{
		StdlibRoot: root,
		Tags:       map[string]bool{"amd64": true, "linux": true},
		Fset:       fset,
	}

	_, err = types.Check("main", fset, []*ast.File{f}, nil, loadCfg)
	if err == nil {
		t.Fatal("expected error for unknown package")
	}
	if !strings.Contains(err.Error(), "cannot import") {
		t.Errorf("expected 'cannot import' error, got: %v", err)
	}
}

func TestImportResolution_UndefinedSymbol(t *testing.T) {
	root := stdlibRoot()
	if root == "" {
		t.Skip("pluto stdlib not found")
	}
	t.Cleanup(types.ResetPackageCache)

	fset := token.NewFileSet()
	src := `
package main

import "pluto/syscall"

func foo() {
	_ = syscall.NONEXISTENT
}
`
	f, err := parser.ParseFile(fset, "test.pu", []byte(src), 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	loadCfg := &types.LoadConfig{
		StdlibRoot: root,
		Tags:       map[string]bool{"amd64": true, "linux": true},
		Fset:       fset,
	}

	_, err = types.Check("main", fset, []*ast.File{f}, nil, loadCfg)
	if err == nil {
		t.Fatal("expected error for undefined symbol in package")
	}
	if !strings.Contains(err.Error(), "not found in package") {
		t.Errorf("expected 'not found in package' error, got: %v", err)
	}
}

func TestImportResolution_BlankImportNoLoad(t *testing.T) {
	// Blank import should not try to load the package
	fset := token.NewFileSet()
	src := `
package main

import _ "pluto/os"

func foo() {}
`
	f, err := parser.ParseFile(fset, "test.pu", []byte(src), 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Even with a loadCfg that has an invalid stdlib root,
	// blank imports should be skipped
	loadCfg := &types.LoadConfig{
		StdlibRoot: "/nonexistent",
		Tags:       map[string]bool{"amd64": true, "linux": true},
		Fset:       fset,
	}

	_, err = types.Check("main", fset, []*ast.File{f}, nil, loadCfg)
	if err != nil {
		t.Fatalf("blank import should not fail: %v", err)
	}
}

func TestImportResolution_NilLoadCfgIgnoresImports(t *testing.T) {
	// When loadCfg is nil, imports should be silently ignored (current behavior)
	fset := token.NewFileSet()
	src := `
package main

import "pluto/os"

func foo() {}
`
	f, err := parser.ParseFile(fset, "test.pu", []byte(src), 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	_, err = types.Check("main", fset, []*ast.File{f}, nil, nil)
	if err != nil {
		t.Fatalf("nil loadCfg should ignore imports: %v", err)
	}
}

func TestImportResolution_AliasedImport(t *testing.T) {
	root := stdlibRoot()
	if root == "" {
		t.Skip("pluto stdlib not found")
	}
	t.Cleanup(types.ResetPackageCache)

	fset := token.NewFileSet()
	src := `
package main

import sc "pluto/syscall"

func callExit() {
	sc.Syscall(sc.SYS_EXIT_GROUP, 0, 0, 0)
}
`
	f, err := parser.ParseFile(fset, "test.pu", []byte(src), 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	loadCfg := &types.LoadConfig{
		StdlibRoot: root,
		Tags:       map[string]bool{"amd64": true, "linux": true},
		Fset:       fset,
	}

	_, err = types.Check("main", fset, []*ast.File{f}, nil, loadCfg)
	if err != nil {
		t.Fatalf("type check with aliased import: %v", err)
	}
}

func TestImportResolution_PackageCache(t *testing.T) {
	root := stdlibRoot()
	if root == "" {
		t.Skip("pluto stdlib not found")
	}
	t.Cleanup(types.ResetPackageCache)

	fset := token.NewFileSet()
	loadCfg := &types.LoadConfig{
		StdlibRoot: root,
		Tags:       map[string]bool{"amd64": true, "linux": true},
		Fset:       fset,
	}

	// Load syscall package twice — second time should come from cache
	pkg1, err := types.LoadPackage("pluto/syscall", loadCfg)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	pkg2, err := types.LoadPackage("pluto/syscall", loadCfg)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if pkg1 != pkg2 {
		t.Error("expected same package instance from cache")
	}
}
