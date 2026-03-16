package ir_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/ast"
	"github.com/matjam/plutonium/internal/ir"
	"github.com/matjam/plutonium/internal/mono"
	"github.com/matjam/plutonium/internal/parser"
	"github.com/matjam/plutonium/internal/token"
	"github.com/matjam/plutonium/internal/types"
)

func lowerOK(t *testing.T, src string) *ir.Program {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.pu", []byte(src), 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pkg, err := types.Check("test", fset, []*ast.File{f}, nil, nil)
	if err != nil {
		t.Fatalf("types: %v", err)
	}
	result, err := mono.Monomorphize(pkg, fset)
	if err != nil {
		t.Fatalf("mono: %v", err)
	}
	prog, err := ir.Lower(result, fset)
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	return prog
}

// lowerWithImports is like lowerOK but provides a LoadConfig so that
// stdlib imports (like "pluto/alloc") can be resolved.
func lowerWithImports(t *testing.T, src string) *ir.Program {
	t.Helper()
	root := testStdlibRoot()
	if root == "" {
		t.Skip("pluto stdlib not found")
	}
	t.Cleanup(types.ResetPackageCache)

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.pu", []byte(src), 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	loadCfg := &types.LoadConfig{
		StdlibRoot: root,
		Tags:       map[string]bool{"amd64": true, "linux": true},
		Fset:       fset,
	}
	pkg, err := types.Check("test", fset, []*ast.File{f}, nil, loadCfg)
	if err != nil {
		t.Fatalf("types: %v", err)
	}
	result, err := mono.Monomorphize(pkg, fset)
	if err != nil {
		t.Fatalf("mono: %v", err)
	}
	prog, err := ir.Lower(result, fset)
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	return prog
}

// testStdlibRoot locates the pluto stdlib directory relative to the test.
func testStdlibRoot() string {
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

func TestLowerSimpleFunc(t *testing.T) {
	prog := lowerOK(t, `
package main
func add(a, b int32) int32 { return a + b }
`)
	if len(prog.Packages) == 0 {
		t.Fatal("no packages in program")
	}
	pkg := prog.Packages[0]
	if len(pkg.Functions) == 0 {
		t.Fatal("no functions in package")
	}
	fn := pkg.Functions[0]
	if fn.Name != "add" {
		t.Errorf("got function %q, want %q", fn.Name, "add")
	}
	if len(fn.Blocks) == 0 {
		t.Fatal("no basic blocks")
	}
	if fn.Blocks[0].Term == nil {
		t.Error("entry block has no terminator")
	}
}

func TestLowerIfStmt(t *testing.T) {
	prog := lowerOK(t, `
package main
func abs(n int32) int32 {
	if n < 0 { return -n }
	return n
}
`)
	fn := prog.Packages[0].Functions[0]
	// Should have at least 3 blocks: entry, then, merge/else
	if len(fn.Blocks) < 2 {
		t.Errorf("got %d blocks, want at least 2", len(fn.Blocks))
	}
}

func TestLowerForLoop(t *testing.T) {
	prog := lowerOK(t, `
package main
func sum(n int32) int32 {
	s := int32(0)
	for i := int32(0); i < n; i++ {
		s += i
	}
	return s
}
`)
	fn := prog.Packages[0].Functions[0]
	// loop needs: init block, condition block, body block, exit block
	if len(fn.Blocks) < 3 {
		t.Errorf("got %d blocks, want at least 3", len(fn.Blocks))
	}
}

func TestLowerVarDecl(t *testing.T) {
	prog := lowerOK(t, `
package main
func foo() int32 {
	x := int32(42)
	return x
}
`)
	fn := prog.Packages[0].Functions[0]
	// Should have at least one alloca for x
	found := false
	for _, instr := range fn.Blocks[0].Instrs {
		if instr.Op == ir.OpAlloca {
			found = true
		}
	}
	if !found {
		t.Error("expected alloca for local variable")
	}
}

func TestLowerMultipleReturns(t *testing.T) {
	prog := lowerOK(t, `
package main
func swap(a, b int32) (int32, int32) { return b, a }
`)
	fn := prog.Packages[0].Functions[0]
	if fn == nil || len(fn.Blocks) == 0 {
		t.Fatal("no function or blocks")
	}
}

func TestLowerStringLiteral(t *testing.T) {
	prog := lowerOK(t, `
package main
func foo() {
	msg := "hello\n"
	_ = msg
}
`)
	pkg := prog.Packages[0]
	if len(pkg.Strings) == 0 {
		t.Error("expected at least one string literal in .rodata")
	}
	if len(pkg.Strings) > 0 {
		if string(pkg.Strings[0].Data) != "hello\n" {
			t.Errorf("got string data %q, want %q", string(pkg.Strings[0].Data), "hello\n")
		}
	}
}

func TestLowerCrossPackageCall(t *testing.T) {
	// A function that calls another function by name
	prog := lowerOK(t, `
package main

func foo() int32 {
    return bar()
}

func bar() int32 {
    return 42
}
`)
	// Check that the call was emitted in foo
	fn := prog.Packages[0].Functions[0] // foo
	foundCall := false
	for _, bb := range fn.Blocks {
		for _, instr := range bb.Instrs {
			if instr.Op == ir.OpCall {
				foundCall = true
			}
		}
	}
	if !foundCall {
		t.Error("expected Call instruction in foo()")
	}
}

func TestLowerStructFieldAccess(t *testing.T) {
	prog := lowerOK(t, `
package main

type Point struct {
	X int32
	Y int32
	Z int32
}

func getY() int32 {
	p := Point{X: 10, Y: 20, Z: 30}
	return p.Y
}

func getZ() int32 {
	p := Point{X: 10, Y: 20, Z: 30}
	return p.Z
}
`)
	pkg := prog.Packages[0]

	// Find getY function
	var getYFn, getZFn *ir.Function
	for _, fn := range pkg.Functions {
		switch fn.Name {
		case "getY":
			getYFn = fn
		case "getZ":
			getZFn = fn
		}
	}
	if getYFn == nil {
		t.Fatal("getY function not found")
	}
	if getZFn == nil {
		t.Fatal("getZ function not found")
	}

	// Check that getY produces an ExtractField with FieldIdx=1 (Y is the second field)
	foundExtractY := false
	for _, bb := range getYFn.Blocks {
		for _, instr := range bb.Instrs {
			if instr.Op == ir.OpExtractField {
				if instr.FieldIdx == 1 {
					foundExtractY = true
				} else {
					t.Errorf("getY: ExtractField has FieldIdx=%d, want 1", instr.FieldIdx)
				}
			}
		}
	}
	if !foundExtractY {
		t.Error("getY: expected ExtractField instruction with FieldIdx=1")
	}

	// Check that getZ produces an ExtractField with FieldIdx=2 (Z is the third field)
	foundExtractZ := false
	for _, bb := range getZFn.Blocks {
		for _, instr := range bb.Instrs {
			if instr.Op == ir.OpExtractField {
				if instr.FieldIdx == 2 {
					foundExtractZ = true
				} else {
					t.Errorf("getZ: ExtractField has FieldIdx=%d, want 2", instr.FieldIdx)
				}
			}
		}
	}
	if !foundExtractZ {
		t.Error("getZ: expected ExtractField instruction with FieldIdx=2")
	}
}

func TestLowerStructFieldAccessRegularAssign(t *testing.T) {
	prog := lowerOK(t, `
package main

type Point struct {
	X int32
	Y int32
}

func getY() int32 {
	var p Point
	p = Point{X: 1, Y: 2}
	return p.Y
}
`)
	pkg := prog.Packages[0]

	var getYFn *ir.Function
	for _, fn := range pkg.Functions {
		if fn.Name == "getY" {
			getYFn = fn
			break
		}
	}
	if getYFn == nil {
		t.Fatal("getY function not found")
	}

	// Check that getY produces an ExtractField with FieldIdx=1 (Y is the second field).
	// This tests that struct types are tracked through regular assignment (=),
	// not just short variable declaration (:=).
	foundExtractY := false
	for _, bb := range getYFn.Blocks {
		for _, instr := range bb.Instrs {
			if instr.Op == ir.OpExtractField {
				if instr.FieldIdx == 1 {
					foundExtractY = true
				} else {
					t.Errorf("getY: ExtractField has FieldIdx=%d, want 1", instr.FieldIdx)
				}
			}
		}
	}
	if !foundExtractY {
		t.Error("getY: expected ExtractField instruction with FieldIdx=1")
	}
}

func TestLowerMethodReceiverAsParam(t *testing.T) {
	prog := lowerOK(t, `
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
	pkg := prog.Packages[0]

	// Find the Counter.Inc function
	var incFn *ir.Function
	for _, fn := range pkg.Functions {
		if fn.Name == "Counter.Inc" {
			incFn = fn
			break
		}
	}
	if incFn == nil {
		var names []string
		for _, fn := range pkg.Functions {
			names = append(names, fn.Name)
		}
		t.Fatalf("Counter.Inc function not found, got: %v", names)
	}

	// The receiver should be the first parameter (pointer to Counter)
	if len(incFn.Params) == 0 {
		t.Fatal("Counter.Inc has no params, expected receiver as first param")
	}
	recvParam := incFn.Params[0]
	if recvParam.Name != "c" {
		t.Errorf("first param name = %q, want %q", recvParam.Name, "c")
	}
	if recvParam.Type == nil || recvParam.Type.Kind != ir.TyPtr {
		t.Errorf("first param should be a pointer type (receiver), got %v", recvParam.Type)
	}
}

func TestLowerMethodReceiverFieldAccess(t *testing.T) {
	// Verify that within a method body, the receiver's struct fields can be accessed
	prog := lowerOK(t, `
package main

type Counter struct {
	Value int32
}

func (c *Counter) GetValue() int32 {
	return c.Value
}

func main() {
	c := Counter{Value: 42}
	_ = c
}
`)
	pkg := prog.Packages[0]

	// Find the Counter.GetValue function
	var getFn *ir.Function
	for _, fn := range pkg.Functions {
		if fn.Name == "Counter.GetValue" {
			getFn = fn
			break
		}
	}
	if getFn == nil {
		var names []string
		for _, fn := range pkg.Functions {
			names = append(names, fn.Name)
		}
		t.Fatalf("Counter.GetValue function not found, got: %v", names)
	}

	// The method body should contain an ExtractField (or Load + GEP) to access c.Value
	// Since c is a pointer receiver, we expect the body to dereference c and extract field 0
	foundFieldAccess := false
	for _, bb := range getFn.Blocks {
		for _, instr := range bb.Instrs {
			if instr.Op == ir.OpExtractField {
				foundFieldAccess = true
			}
		}
	}
	if !foundFieldAccess {
		t.Error("Counter.GetValue: expected field access instruction for c.Value")
	}
}

func TestLowerMethodValueReceiver(t *testing.T) {
	prog := lowerOK(t, `
package main

type Point struct {
	X int32
	Y int32
}

func (p Point) Sum() int32 {
	return p.X + p.Y
}

func main() {
	p := Point{X: 3, Y: 4}
	_ = p
}
`)
	pkg := prog.Packages[0]

	// Find the Point.Sum function
	var sumFn *ir.Function
	for _, fn := range pkg.Functions {
		if fn.Name == "Point.Sum" {
			sumFn = fn
			break
		}
	}
	if sumFn == nil {
		var names []string
		for _, fn := range pkg.Functions {
			names = append(names, fn.Name)
		}
		t.Fatalf("Point.Sum function not found, got: %v", names)
	}

	// The receiver should be the first parameter
	if len(sumFn.Params) == 0 {
		t.Fatal("Point.Sum has no params, expected receiver as first param")
	}
	recvParam := sumFn.Params[0]
	if recvParam.Name != "p" {
		t.Errorf("first param name = %q, want %q", recvParam.Name, "p")
	}
}

func TestLowerMethodCall(t *testing.T) {
	prog := lowerOK(t, `
package main

type Counter struct {
	Value int32
}

func (c *Counter) Inc() {
	c.Value = c.Value + 1
}

func main() {
	c := Counter{Value: 0}
	c.Inc()
}
`)
	pkg := prog.Packages[0]

	// Find the main function
	var mainFn *ir.Function
	for _, fn := range pkg.Functions {
		if fn.Name == "main" {
			mainFn = fn
			break
		}
	}
	if mainFn == nil {
		t.Fatal("main function not found")
	}

	// Check that main contains a call to Counter.Inc
	foundCall := false
	for _, bb := range mainFn.Blocks {
		for _, instr := range bb.Instrs {
			if instr.Op == ir.OpCall && instr.Callee != nil && instr.Callee.Name == "Counter.Inc" {
				foundCall = true
				// The call should have at least one arg (the receiver)
				if len(instr.Args) == 0 {
					t.Error("method call to Counter.Inc should have receiver as first arg")
				}
			}
		}
	}
	if !foundCall {
		t.Error("expected call to Counter.Inc in main()")
	}
}

func TestLowerPackedType(t *testing.T) {
	prog := lowerOK(t, `
package main

type R packed uint32 {
	x bits[uint, 4]
	y bits[uint, 4]
}

func main() int64 {
	var r R
	r.x = 5
	r.y = 10
	a := r.x
	b := r.y
	_ = a
	_ = b
	return 0
}
`)
	fn := prog.Packages[0].Functions[0] // main
	if fn.Name != "main" {
		t.Fatalf("expected main, got %s", fn.Name)
	}

	// The packed type should generate shift/mask operations, not ExtractField/InsertField
	for _, bb := range fn.Blocks {
		for _, instr := range bb.Instrs {
			if instr.Op == ir.OpExtractField || instr.Op == ir.OpInsertField {
				t.Error("packed type should use shift/mask, not ExtractField/InsertField")
			}
		}
	}

	// Verify shift and mask operations are present
	foundShr := false
	foundAnd := false
	foundOr := false
	for _, bb := range fn.Blocks {
		for _, instr := range bb.Instrs {
			switch instr.Op {
			case ir.OpShr:
				foundShr = true
			case ir.OpAnd:
				foundAnd = true
			case ir.OpOr:
				foundOr = true
			}
		}
	}
	if !foundAnd {
		t.Error("expected AND instruction for bitfield masking")
	}
	if !foundOr {
		t.Error("expected OR instruction for bitfield insertion")
	}
	// SHR is optional (offset=0 fields don't need shift for read)
	// But y has offset=4, so it should use SHR
	if !foundShr {
		t.Error("expected SHR instruction for bitfield extraction of y at offset 4")
	}
}

func TestLowerRingType(t *testing.T) {
	prog := lowerOK(t, `
package main

func main() int64 {
	var r ring[int64, 8]
	r.Push(10)
	r.Push(20)
	n := r.Len()
	_ = n
	val := r.Pop()
	_ = val
	empty := r.Empty()
	_ = empty
	full := r.Full()
	_ = full
	r.Reset()
	return 0
}
`)
	fn := prog.Packages[0].Functions[0]
	if fn.Name != "main" {
		t.Fatalf("expected main, got %s", fn.Name)
	}

	// ring[T, N] should generate:
	// - Alloca for the ring struct
	// - InsertField for head/tail initialization
	// - Sub operations for Len (head - tail)
	// - Cmp operations for Empty/Full checks
	// - GEP + Store for Push (data array access)
	// - GEP + Load for Pop (data array access)
	foundAlloca := false
	foundSub := false
	foundCmp := false
	foundGEP := false
	for _, bb := range fn.Blocks {
		for _, instr := range bb.Instrs {
			switch instr.Op {
			case ir.OpAlloca:
				foundAlloca = true
			case ir.OpSub:
				foundSub = true
			case ir.OpCmp:
				foundCmp = true
			case ir.OpGEP:
				foundGEP = true
			}
		}
	}
	if !foundAlloca {
		t.Error("expected Alloca for ring buffer struct")
	}
	if !foundSub {
		t.Error("expected Sub for ring Len/Full (head - tail)")
	}
	if !foundCmp {
		t.Error("expected Cmp for ring Empty/Full checks")
	}
	if !foundGEP {
		t.Error("expected GEP for ring Push/Pop data array access")
	}
}

func TestLowerRingPushPop(t *testing.T) {
	prog := lowerOK(t, `
package main

func main() int64 {
	var r ring[int64, 4]
	r.Push(42)
	val := r.Pop()
	_ = val
	return 0
}
`)
	fn := prog.Packages[0].Functions[0]

	// Should have conditional branches for Push (full check) and Pop (empty check)
	branchCount := 0
	for _, bb := range fn.Blocks {
		if bb.Term != nil && bb.Term.Kind == ir.TermCondBr {
			branchCount++
		}
	}
	if branchCount < 2 {
		t.Errorf("expected at least 2 conditional branches (push full-check + pop empty-check), got %d", branchCount)
	}
}

func TestLowerBitsLens(t *testing.T) {
	prog := lowerOK(t, `
package main

func main() int64 {
	var flags uint64
	var b bits[flags]
	b.Set(0)
	b.Set(3)
	b.Clear(3)
	t := b.Test(7)
	_ = t
	b.Toggle(0)
	return 0
}
`)
	fn := prog.Packages[0].Functions[0]
	if fn.Name != "main" {
		t.Fatalf("expected main, got %s", fn.Name)
	}

	// bits[V] should generate:
	// - Shl operations (1 << n for mask creation)
	// - Or operations (Set: V |= mask)
	// - And operations (Clear: V &= ^mask, Test: (V>>n) & 1)
	// - Xor operations (Clear's ^mask, Toggle: V ^= mask)
	// - Shr operations (Test: V >> n)
	foundShl := false
	foundOr := false
	foundAnd := false
	foundXor := false
	foundShr := false
	for _, bb := range fn.Blocks {
		for _, instr := range bb.Instrs {
			switch instr.Op {
			case ir.OpShl:
				foundShl = true
			case ir.OpOr:
				foundOr = true
			case ir.OpAnd:
				foundAnd = true
			case ir.OpXor:
				foundXor = true
			case ir.OpShr:
				foundShr = true
			}
		}
	}
	if !foundShl {
		t.Error("expected SHL for bit mask creation (1 << n)")
	}
	if !foundOr {
		t.Error("expected OR for Set operation")
	}
	if !foundAnd {
		t.Error("expected AND for Clear/Test operations")
	}
	if !foundXor {
		t.Error("expected XOR for Clear's ^mask or Toggle")
	}
	if !foundShr {
		t.Error("expected SHR for Test operation (V >> n)")
	}
}

func TestLowerBitsPopCount(t *testing.T) {
	prog := lowerOK(t, `
package main

func main() int64 {
	var flags uint64
	var b bits[flags]
	b.Set(0)
	b.Set(1)
	b.Set(2)
	n := b.PopCount()
	_ = n
	return 0
}
`)
	fn := prog.Packages[0].Functions[0]

	// PopCount generates a loop (Brian Kernighan's algorithm),
	// which means we should see at least one conditional branch for the loop.
	foundCondBr := false
	for _, bb := range fn.Blocks {
		if bb.Term != nil && bb.Term.Kind == ir.TermCondBr {
			foundCondBr = true
		}
	}
	if !foundCondBr {
		t.Error("expected conditional branch for PopCount loop")
	}
}

func TestLowerRAIIAutoFree(t *testing.T) {
	// alloc.New[int64]() should emit an automatic decRef call at function exit
	prog := lowerWithImports(t, `
package main
import "pluto/alloc"

func useAndFree() {
	ptr := alloc.New[int64]()
	*ptr = 42
}
`)
	pkg := prog.Packages[0]

	var fn *ir.Function
	for _, f := range pkg.Functions {
		if f.Name == "useAndFree" {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatal("useAndFree function not found")
	}

	// Should contain a call to alloc.decRef (auto-inserted by RAII refcounting)
	foundDecRef := false
	for _, bb := range fn.Blocks {
		for _, instr := range bb.Instrs {
			if instr.Op == ir.OpCall && instr.Callee != nil && instr.Callee.Name == "alloc.decRef" {
				foundDecRef = true
			}
		}
	}
	if !foundDecRef {
		t.Error("expected auto-inserted call to alloc.decRef for heap-owned variable")
	}
}

func TestLowerRAIINoFreeOnReturn(t *testing.T) {
	// When a heap-owned variable is returned, it should NOT be freed (ownership transfer)
	prog := lowerWithImports(t, `
package main
import "pluto/alloc"

func createAndReturn() *int64 {
	ptr := alloc.New[int64]()
	*ptr = 99
	return ptr
}
`)
	pkg := prog.Packages[0]

	var fn *ir.Function
	for _, f := range pkg.Functions {
		if f.Name == "createAndReturn" {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatal("createAndReturn function not found")
	}

	// Should NOT contain a call to alloc.decRef (ownership transfers via return)
	for _, bb := range fn.Blocks {
		for _, instr := range bb.Instrs {
			if instr.Op == ir.OpCall && instr.Callee != nil && instr.Callee.Name == "alloc.decRef" {
				t.Error("should NOT insert decRef when variable is returned (ownership transfer)")
			}
		}
	}
}

func TestLowerRAIIMultipleAllocs(t *testing.T) {
	// Multiple heap allocations in the same scope should all be freed
	prog := lowerWithImports(t, `
package main
import "pluto/alloc"

func multiAlloc() {
	a := alloc.New[int64]()
	b := alloc.New[int64]()
	*a = 10
	*b = 20
}
`)
	pkg := prog.Packages[0]

	var fn *ir.Function
	for _, f := range pkg.Functions {
		if f.Name == "multiAlloc" {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatal("multiAlloc function not found")
	}

	// Should contain exactly 2 calls to alloc.decRef (one for each allocation)
	decRefCount := 0
	for _, bb := range fn.Blocks {
		for _, instr := range bb.Instrs {
			if instr.Op == ir.OpCall && instr.Callee != nil && instr.Callee.Name == "alloc.decRef" {
				decRefCount++
			}
		}
	}
	if decRefCount != 2 {
		t.Errorf("expected 2 auto-inserted decRef calls, got %d", decRefCount)
	}
}

func TestLowerRAIIEarlyReturn(t *testing.T) {
	// An early return should free heap-owned variables that aren't returned
	prog := lowerWithImports(t, `
package main
import "pluto/alloc"

func earlyReturn(flag int64) int64 {
	ptr := alloc.New[int64]()
	*ptr = 42
	if flag > 0 {
		return *ptr
	}
	return 0
}
`)
	pkg := prog.Packages[0]

	var fn *ir.Function
	for _, f := range pkg.Functions {
		if f.Name == "earlyReturn" {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatal("earlyReturn function not found")
	}

	// Should contain decRef calls on all return paths
	decRefCount := 0
	for _, bb := range fn.Blocks {
		for _, instr := range bb.Instrs {
			if instr.Op == ir.OpCall && instr.Callee != nil && instr.Callee.Name == "alloc.decRef" {
				decRefCount++
			}
		}
	}
	// There are at least 2 return paths (if-then and fallthrough), each should decRef
	if decRefCount < 2 {
		t.Errorf("expected at least 2 decRef calls (one per return path), got %d", decRefCount)
	}
}

func TestLowerRefCountIncOnCopy(t *testing.T) {
	// When a heap-owned pointer is copied to another variable (b := a),
	// the compiler should emit alloc.incRef and track b as heap-owned too.
	prog := lowerWithImports(t, `
package main
import "pluto/alloc"

func shared() {
	a := alloc.New[int64]()
	*a = 42
	b := a
	_ = *b
}
`)
	pkg := prog.Packages[0]

	var fn *ir.Function
	for _, f := range pkg.Functions {
		if f.Name == "shared" {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatal("shared function not found")
	}

	// Should contain a call to alloc.incRef (refcount increment on copy)
	foundIncRef := false
	for _, bb := range fn.Blocks {
		for _, instr := range bb.Instrs {
			if instr.Op == ir.OpCall && instr.Callee != nil && instr.Callee.Name == "alloc.incRef" {
				foundIncRef = true
			}
		}
	}
	if !foundIncRef {
		t.Error("expected call to alloc.incRef when heap-owned pointer is copied")
	}

	// Should contain exactly 2 decRef calls (one for a, one for b)
	decRefCount := 0
	for _, bb := range fn.Blocks {
		for _, instr := range bb.Instrs {
			if instr.Op == ir.OpCall && instr.Callee != nil && instr.Callee.Name == "alloc.decRef" {
				decRefCount++
			}
		}
	}
	if decRefCount != 2 {
		t.Errorf("expected 2 decRef calls (one for a, one for b), got %d", decRefCount)
	}
}

func TestLowerRefCountExplicitFree(t *testing.T) {
	// Explicit alloc.Free should emit decRef instead of freeRaw
	prog := lowerWithImports(t, `
package main
import "pluto/alloc"

func explicitFree() {
	ptr := alloc.New[int64]()
	*ptr = 42
	alloc.Free(ptr)
}
`)
	pkg := prog.Packages[0]

	var fn *ir.Function
	for _, f := range pkg.Functions {
		if f.Name == "explicitFree" {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatal("explicitFree function not found")
	}

	// Should contain a call to alloc.decRef (not alloc.freeRaw)
	foundDecRef := false
	foundFreeRaw := false
	for _, bb := range fn.Blocks {
		for _, instr := range bb.Instrs {
			if instr.Op == ir.OpCall && instr.Callee != nil {
				if instr.Callee.Name == "alloc.decRef" {
					foundDecRef = true
				}
				if instr.Callee.Name == "alloc.freeRaw" {
					foundFreeRaw = true
				}
			}
		}
	}
	if !foundDecRef {
		t.Error("expected alloc.Free to emit alloc.decRef")
	}
	if foundFreeRaw {
		t.Error("alloc.Free should NOT emit alloc.freeRaw (should use decRef)")
	}
}

func TestLowerBreakContinue(t *testing.T) {
	prog := lowerOK(t, `
package main

func breakLoop() int32 {
	s := int32(0)
	for i := int32(0); i < 100; i++ {
		if i > 5 {
			break
		}
		s += i
	}
	return s
}

func continueLoop() int32 {
	s := int32(0)
	for i := int32(0); i < 10; i++ {
		if i == 3 {
			continue
		}
		s += i
	}
	return s
}
`)
	pkg := prog.Packages[0]

	// Find the breakLoop function — break should generate a Br to the exit block
	var breakFn *ir.Function
	for _, fn := range pkg.Functions {
		if fn.Name == "breakLoop" {
			breakFn = fn
			break
		}
	}
	if breakFn == nil {
		t.Fatal("breakLoop function not found")
	}

	// There should be a block that branches to for.exit (the break target)
	// Look for a Br terminator pointing to a block with label starting with "for.exit"
	foundBreakBr := false
	for _, bb := range breakFn.Blocks {
		if bb.Term != nil && bb.Term.Kind == ir.TermBr && bb.Term.Then != nil {
			if strings.HasPrefix(bb.Term.Then.Label, "for.exit") &&
				!strings.HasPrefix(bb.Label, "for.post") &&
				!strings.HasPrefix(bb.Label, "for.cond") {
				foundBreakBr = true
			}
		}
	}
	if !foundBreakBr {
		t.Error("breakLoop: expected a branch to for.exit from the break statement")
	}

	// Find the continueLoop function — continue should generate a Br to the post block
	var contFn *ir.Function
	for _, fn := range pkg.Functions {
		if fn.Name == "continueLoop" {
			contFn = fn
			break
		}
	}
	if contFn == nil {
		t.Fatal("continueLoop function not found")
	}

	// There should be a block that branches to for.post (the continue target)
	foundContBr := false
	for _, bb := range contFn.Blocks {
		if bb.Term != nil && bb.Term.Kind == ir.TermBr && bb.Term.Then != nil {
			if strings.HasPrefix(bb.Term.Then.Label, "for.post") &&
				!strings.HasPrefix(bb.Label, "for.body") &&
				!strings.HasPrefix(bb.Label, "for.cond") {
				foundContBr = true
			}
		}
	}
	if !foundContBr {
		t.Error("continueLoop: expected a branch to for.post from the continue statement")
	}
}

func TestLowerSwitch(t *testing.T) {
	prog := lowerOK(t, `
package main

func classify(x int32) int32 {
	switch x {
	case 1:
		return 10
	case 2:
		return 20
	case 3:
		return 30
	default:
		return 0
	}
}
`)
	pkg := prog.Packages[0]

	var fn *ir.Function
	for _, f := range pkg.Functions {
		if f.Name == "classify" {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatal("classify function not found")
	}

	// Switch should produce CmpEQ comparisons for each case
	cmpCount := 0
	for _, bb := range fn.Blocks {
		for _, instr := range bb.Instrs {
			if instr.Op == ir.OpCmp {
				cmpCount++
			}
		}
	}
	if cmpCount < 3 {
		t.Errorf("expected at least 3 CmpEQ instructions for switch cases, got %d", cmpCount)
	}

	// Should have blocks labeled switch.case.N and switch.merge.N
	foundCaseBlock := false
	foundMergeBlock := false
	for _, bb := range fn.Blocks {
		if strings.HasPrefix(bb.Label, "switch.case") {
			foundCaseBlock = true
		}
		if strings.HasPrefix(bb.Label, "switch.merge") {
			foundMergeBlock = true
		}
	}
	if !foundCaseBlock {
		t.Error("expected switch.case block")
	}
	if !foundMergeBlock {
		t.Error("expected switch.merge block")
	}
}

func TestLowerDefer(t *testing.T) {
	prog := lowerOK(t, `
package main

func cleanup() {}

func useDefer() int32 {
	defer cleanup()
	return 42
}
`)
	pkg := prog.Packages[0]

	var fn *ir.Function
	for _, f := range pkg.Functions {
		if f.Name == "useDefer" {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatal("useDefer function not found")
	}

	// The deferred cleanup() call should appear before the return
	foundDeferCall := false
	for _, bb := range fn.Blocks {
		for _, instr := range bb.Instrs {
			if instr.Op == ir.OpCall && instr.Callee != nil && instr.Callee.Name == "cleanup" {
				foundDeferCall = true
			}
		}
	}
	if !foundDeferCall {
		t.Error("expected deferred call to cleanup() to be emitted before return")
	}
}

func TestLowerDeferLIFO(t *testing.T) {
	prog := lowerOK(t, `
package main

func first() {}
func second() {}

func useTwoDefers() {
	defer first()
	defer second()
}
`)
	pkg := prog.Packages[0]

	var fn *ir.Function
	for _, f := range pkg.Functions {
		if f.Name == "useTwoDefers" {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatal("useTwoDefers function not found")
	}

	// Both deferred calls should be present, and second() should come
	// before first() (LIFO order). Collect call order from the block.
	var callOrder []string
	for _, bb := range fn.Blocks {
		for _, instr := range bb.Instrs {
			if instr.Op == ir.OpCall && instr.Callee != nil {
				callOrder = append(callOrder, instr.Callee.Name)
			}
		}
	}
	if len(callOrder) < 2 {
		t.Fatalf("expected at least 2 deferred calls, got %d: %v", len(callOrder), callOrder)
	}
	// LIFO: second should be called before first
	secondIdx := -1
	firstIdx := -1
	for i, name := range callOrder {
		if name == "second" && secondIdx == -1 {
			secondIdx = i
		}
		if name == "first" && firstIdx == -1 {
			firstIdx = i
		}
	}
	if secondIdx == -1 || firstIdx == -1 {
		t.Fatalf("expected both first and second calls, got: %v", callOrder)
	}
	if secondIdx > firstIdx {
		t.Errorf("defer LIFO order violated: second() at index %d, first() at index %d; expected second before first", secondIdx, firstIdx)
	}
}

// --- Additional coverage tests ---

func TestLowerSwitchWithoutDefault(t *testing.T) {
	prog := lowerOK(t, `
package main

func classify(x int32) int32 {
	switch x {
	case 1:
		return 10
	case 2:
		return 20
	}
	return 0
}
`)
	pkg := prog.Packages[0]
	var fn *ir.Function
	for _, f := range pkg.Functions {
		if f.Name == "classify" {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatal("classify function not found")
	}
	// Should have CmpEQ comparisons for each case
	cmpCount := 0
	for _, bb := range fn.Blocks {
		for _, instr := range bb.Instrs {
			if instr.Op == ir.OpCmp {
				cmpCount++
			}
		}
	}
	if cmpCount < 2 {
		t.Errorf("expected at least 2 CmpEQ instructions, got %d", cmpCount)
	}
}

func TestLowerSwitchMultiValue(t *testing.T) {
	// A switch case with multiple values: case 1, 2, 3:
	prog := lowerOK(t, `
package main

func classify(x int32) int32 {
	switch x {
	case 1, 2, 3:
		return 100
	default:
		return 0
	}
}
`)
	pkg := prog.Packages[0]
	var fn *ir.Function
	for _, f := range pkg.Functions {
		if f.Name == "classify" {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatal("classify function not found")
	}
	// Should have at least 3 CmpEQ comparisons (one for each value in the case list)
	cmpCount := 0
	for _, bb := range fn.Blocks {
		for _, instr := range bb.Instrs {
			if instr.Op == ir.OpCmp {
				cmpCount++
			}
		}
	}
	if cmpCount < 3 {
		t.Errorf("expected at least 3 CmpEQ instructions for multi-value case, got %d", cmpCount)
	}
}

func TestLowerSwitchWithInit(t *testing.T) {
	prog := lowerOK(t, `
package main

func classify() int32 {
	switch x := int32(5); x {
	case 5:
		return 50
	default:
		return 0
	}
}
`)
	pkg := prog.Packages[0]
	var fn *ir.Function
	for _, f := range pkg.Functions {
		if f.Name == "classify" {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatal("classify function not found")
	}
	// Should have at least one CmpEQ
	foundCmp := false
	for _, bb := range fn.Blocks {
		for _, instr := range bb.Instrs {
			if instr.Op == ir.OpCmp {
				foundCmp = true
			}
		}
	}
	if !foundCmp {
		t.Error("expected CmpEQ for switch with init")
	}
}

func TestLowerSwitchEmptyBody(t *testing.T) {
	prog := lowerOK(t, `
package main

func classify(x int32) int32 {
	switch x {
	}
	return 0
}
`)
	pkg := prog.Packages[0]
	var fn *ir.Function
	for _, f := range pkg.Functions {
		if f.Name == "classify" {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatal("classify function not found")
	}
	// Empty switch should still compile without error
	if len(fn.Blocks) == 0 {
		t.Error("expected at least one block")
	}
}

func TestLowerRange(t *testing.T) {
	prog := lowerOK(t, `
package main

func rangeLoop() int64 {
	msg := "hello"
	_ = msg
	for range msg {
	}
	return 0
}
`)
	pkg := prog.Packages[0]
	var fn *ir.Function
	for _, f := range pkg.Functions {
		if f.Name == "rangeLoop" {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatal("rangeLoop function not found")
	}

	// Range should create range.cond, range.body, range.post, range.exit blocks
	foundCond := false
	foundBody := false
	foundPost := false
	foundExit := false
	for _, bb := range fn.Blocks {
		if strings.HasPrefix(bb.Label, "range.cond") {
			foundCond = true
		}
		if strings.HasPrefix(bb.Label, "range.body") {
			foundBody = true
		}
		if strings.HasPrefix(bb.Label, "range.post") {
			foundPost = true
		}
		if strings.HasPrefix(bb.Label, "range.exit") {
			foundExit = true
		}
	}
	if !foundCond {
		t.Error("expected range.cond block")
	}
	if !foundBody {
		t.Error("expected range.body block")
	}
	if !foundPost {
		t.Error("expected range.post block")
	}
	if !foundExit {
		t.Error("expected range.exit block")
	}
}

func TestLowerShortCircuitAnd(t *testing.T) {
	prog := lowerOK(t, `
package main

func both(a, b int32) int32 {
	if a > 0 && b > 0 {
		return 1
	}
	return 0
}
`)
	pkg := prog.Packages[0]
	var fn *ir.Function
	for _, f := range pkg.Functions {
		if f.Name == "both" {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatal("both function not found")
	}

	// Short-circuit && should generate sc.evalb and sc.merge blocks
	foundEvalB := false
	foundMerge := false
	for _, bb := range fn.Blocks {
		if strings.HasPrefix(bb.Label, "sc.evalb") {
			foundEvalB = true
		}
		if strings.HasPrefix(bb.Label, "sc.merge") {
			foundMerge = true
		}
	}
	if !foundEvalB {
		t.Error("expected sc.evalb block for short-circuit &&")
	}
	if !foundMerge {
		t.Error("expected sc.merge block for short-circuit &&")
	}
}

func TestLowerShortCircuitOr(t *testing.T) {
	prog := lowerOK(t, `
package main

func either(a, b int32) int32 {
	if a > 0 || b > 0 {
		return 1
	}
	return 0
}
`)
	pkg := prog.Packages[0]
	var fn *ir.Function
	for _, f := range pkg.Functions {
		if f.Name == "either" {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatal("either function not found")
	}

	// Short-circuit || should also have sc.evalb and sc.merge blocks
	foundEvalB := false
	foundMerge := false
	for _, bb := range fn.Blocks {
		if strings.HasPrefix(bb.Label, "sc.evalb") {
			foundEvalB = true
		}
		if strings.HasPrefix(bb.Label, "sc.merge") {
			foundMerge = true
		}
	}
	if !foundEvalB {
		t.Error("expected sc.evalb block for short-circuit ||")
	}
	if !foundMerge {
		t.Error("expected sc.merge block for short-circuit ||")
	}
}

func TestLowerUnaryNot(t *testing.T) {
	prog := lowerOK(t, `
package main

func invert(x int32) int32 {
	if !true {
		return 0
	}
	return 1
}
`)
	_ = prog // just ensure it compiles
}

func TestLowerUnaryBitwiseNot(t *testing.T) {
	prog := lowerOK(t, `
package main

func complement(x uint32) uint32 {
	return ^x
}
`)
	pkg := prog.Packages[0]
	var fn *ir.Function
	for _, f := range pkg.Functions {
		if f.Name == "complement" {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatal("complement function not found")
	}
	// Bitwise NOT generates XOR with -1
	foundXor := false
	for _, bb := range fn.Blocks {
		for _, instr := range bb.Instrs {
			if instr.Op == ir.OpXor {
				foundXor = true
			}
		}
	}
	if !foundXor {
		t.Error("expected XOR for bitwise NOT (^x)")
	}
}

func TestLowerAndNot(t *testing.T) {
	prog := lowerOK(t, `
package main

func andnot(a, b uint32) uint32 {
	return a &^ b
}
`)
	pkg := prog.Packages[0]
	var fn *ir.Function
	for _, f := range pkg.Functions {
		if f.Name == "andnot" {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatal("andnot function not found")
	}
	// &^ generates XOR(b, -1) then AND(a, result)
	foundXor := false
	foundAnd := false
	for _, bb := range fn.Blocks {
		for _, instr := range bb.Instrs {
			if instr.Op == ir.OpXor {
				foundXor = true
			}
			if instr.Op == ir.OpAnd {
				foundAnd = true
			}
		}
	}
	if !foundXor {
		t.Error("expected XOR for &^ (complement of b)")
	}
	if !foundAnd {
		t.Error("expected AND for &^ (a & ^b)")
	}
}

func TestLowerBinaryOps(t *testing.T) {
	// Test all arithmetic and bitwise operators
	prog := lowerOK(t, `
package main

func arith(a, b int32) int32 {
	c := a + b
	d := a - b
	e := a * b
	f := a / b
	g := a % b
	h := a & b
	i := a | b
	j := a ^ b
	k := a << uint32(b)
	l := a >> uint32(b)
	_ = c; _ = d; _ = e; _ = f; _ = g
	_ = h; _ = i; _ = j; _ = k; _ = l
	return 0
}
`)
	_ = prog // ensure it compiles
}

func TestLowerCompareOps(t *testing.T) {
	// Test comparison operators including unsigned
	prog := lowerOK(t, `
package main

func cmpSigned(a, b int32) int32 {
	if a == b { return 1 }
	if a != b { return 2 }
	if a < b  { return 3 }
	if a <= b { return 4 }
	if a > b  { return 5 }
	if a >= b { return 6 }
	return 0
}

func cmpUnsigned(a, b uint32) uint32 {
	if a < b  { return 1 }
	if a <= b { return 2 }
	if a > b  { return 3 }
	if a >= b { return 4 }
	return 0
}
`)
	pkg := prog.Packages[0]

	// Find cmpUnsigned — it should produce unsigned comparison ops
	var fn *ir.Function
	for _, f := range pkg.Functions {
		if f.Name == "cmpUnsigned" {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatal("cmpUnsigned function not found")
	}
	cmpCount := 0
	for _, bb := range fn.Blocks {
		for _, instr := range bb.Instrs {
			if instr.Op == ir.OpCmp {
				cmpCount++
			}
		}
	}
	if cmpCount < 4 {
		t.Errorf("expected at least 4 comparison instructions in cmpUnsigned, got %d", cmpCount)
	}
}

func TestLowerCompoundAssign(t *testing.T) {
	prog := lowerOK(t, `
package main

func compound() int32 {
	x := int32(10)
	x += 5
	x -= 2
	x *= 3
	x /= 2
	x %= 7
	x &= 0xFF
	x |= 0x10
	x ^= 0x01
	x <<= 1
	x >>= 1
	return x
}
`)
	_ = prog // ensure it compiles without error
}

func TestLowerIncDec(t *testing.T) {
	prog := lowerOK(t, `
package main

func incDec() int32 {
	x := int32(0)
	x++
	x++
	x--
	return x
}
`)
	pkg := prog.Packages[0]
	var fn *ir.Function
	for _, f := range pkg.Functions {
		if f.Name == "incDec" {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatal("incDec function not found")
	}
	// Should have Add and Sub operations for inc/dec
	addCount := 0
	subCount := 0
	for _, bb := range fn.Blocks {
		for _, instr := range bb.Instrs {
			if instr.Op == ir.OpAdd {
				addCount++
			}
			if instr.Op == ir.OpSub {
				subCount++
			}
		}
	}
	if addCount < 2 {
		t.Errorf("expected at least 2 Add for x++, got %d", addCount)
	}
	if subCount < 1 {
		t.Errorf("expected at least 1 Sub for x--, got %d", subCount)
	}
}

func TestLowerCharLiterals(t *testing.T) {
	prog := lowerOK(t, `
package main

func getChars() int32 {
	a := 'A'
	b := '\n'
	c := '\t'
	d := '\r'
	_ = a; _ = b; _ = c; _ = d
	return 0
}
`)
	_ = prog // ensure all char escape sequences compile
}

func TestLowerFloatLiteral(t *testing.T) {
	prog := lowerOK(t, `
package main

func getFloat() float64 {
	x := 3.14
	_ = x
	return 0.0
}
`)
	_ = prog
}

func TestLowerTypeConversions(t *testing.T) {
	prog := lowerOK(t, `
package main

func convert(x int32) int64 {
	return int64(x)
}
`)
	pkg := prog.Packages[0]
	var fn *ir.Function
	for _, f := range pkg.Functions {
		if f.Name == "convert" {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatal("convert function not found")
	}
	// Should have a Bitcast/Convert instruction
	foundConvert := false
	for _, bb := range fn.Blocks {
		for _, instr := range bb.Instrs {
			if instr.Op == ir.OpBitcast || instr.Op == ir.OpExtend || instr.Op == ir.OpTruncate {
				foundConvert = true
			}
		}
	}
	if !foundConvert {
		t.Error("expected conversion instruction for int64(x)")
	}
}

func TestLowerIfWithInit(t *testing.T) {
	prog := lowerOK(t, `
package main

func check() int32 {
	if x := int32(10); x > 5 {
		return x
	}
	return 0
}
`)
	pkg := prog.Packages[0]
	var fn *ir.Function
	for _, f := range pkg.Functions {
		if f.Name == "check" {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatal("check function not found")
	}
	// Should have if.then and if.merge blocks
	foundThen := false
	foundMerge := false
	for _, bb := range fn.Blocks {
		if strings.HasPrefix(bb.Label, "if.then") {
			foundThen = true
		}
		if strings.HasPrefix(bb.Label, "if.merge") {
			foundMerge = true
		}
	}
	if !foundThen {
		t.Error("expected if.then block")
	}
	if !foundMerge {
		t.Error("expected if.merge block")
	}
}

func TestLowerIfElse(t *testing.T) {
	prog := lowerOK(t, `
package main

func maxVal(a, b int32) int32 {
	if a > b {
		return a
	} else {
		return b
	}
}
`)
	pkg := prog.Packages[0]
	var fn *ir.Function
	for _, f := range pkg.Functions {
		if f.Name == "maxVal" {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatal("maxVal function not found")
	}
	// Should have if.else block
	foundElse := false
	for _, bb := range fn.Blocks {
		if strings.HasPrefix(bb.Label, "if.else") {
			foundElse = true
		}
	}
	if !foundElse {
		t.Error("expected if.else block")
	}
}

func TestLowerNegate(t *testing.T) {
	prog := lowerOK(t, `
package main

func negate(x int32) int32 {
	return -x
}
`)
	pkg := prog.Packages[0]
	var fn *ir.Function
	for _, f := range pkg.Functions {
		if f.Name == "negate" {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatal("negate function not found")
	}
	// Negate is implemented as 0 - x
	foundSub := false
	for _, bb := range fn.Blocks {
		for _, instr := range bb.Instrs {
			if instr.Op == ir.OpSub {
				foundSub = true
			}
		}
	}
	if !foundSub {
		t.Error("expected Sub for unary negation")
	}
}

func TestLowerAddressOf(t *testing.T) {
	prog := lowerOK(t, `
package main

func addr() int64 {
	x := int64(42)
	p := &x
	_ = p
	return x
}
`)
	_ = prog
}

func TestLowerDereference(t *testing.T) {
	prog := lowerOK(t, `
package main

func deref(p *int64) int64 {
	return *p
}
`)
	pkg := prog.Packages[0]
	var fn *ir.Function
	for _, f := range pkg.Functions {
		if f.Name == "deref" {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatal("deref function not found")
	}
	// Dereference generates a Load
	foundLoad := false
	for _, bb := range fn.Blocks {
		for _, instr := range bb.Instrs {
			if instr.Op == ir.OpLoad {
				foundLoad = true
			}
		}
	}
	if !foundLoad {
		t.Error("expected Load for dereference")
	}
}

func TestLowerBooleanConstants(t *testing.T) {
	prog := lowerOK(t, `
package main

func booleans() int32 {
	t := true
	f := false
	_ = t; _ = f
	return 0
}
`)
	_ = prog
}

func TestLowerForInfiniteLoop(t *testing.T) {
	// Test a for loop with no condition (infinite loop with break)
	prog := lowerOK(t, `
package main

func loop() int32 {
	x := int32(0)
	for {
		x++
		if x > 10 {
			break
		}
	}
	return x
}
`)
	pkg := prog.Packages[0]
	var fn *ir.Function
	for _, f := range pkg.Functions {
		if f.Name == "loop" {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatal("loop function not found")
	}
	// Infinite loop: for.cond should have unconditional Br to for.body
	for _, bb := range fn.Blocks {
		if strings.HasPrefix(bb.Label, "for.cond") {
			if bb.Term != nil && bb.Term.Kind == ir.TermBr {
				// OK — unconditional branch
				break
			}
		}
	}
}

func TestLowerStructAssignField(t *testing.T) {
	prog := lowerOK(t, `
package main

type Point struct {
	X int32
	Y int32
}

func setField() int32 {
	var p Point
	p.X = 10
	p.Y = 20
	return p.X
}
`)
	pkg := prog.Packages[0]
	var fn *ir.Function
	for _, f := range pkg.Functions {
		if f.Name == "setField" {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatal("setField function not found")
	}
	// Should have InsertField instructions for field assignment
	insertCount := 0
	for _, bb := range fn.Blocks {
		for _, instr := range bb.Instrs {
			if instr.Op == ir.OpInsertField {
				insertCount++
			}
		}
	}
	if insertCount < 2 {
		t.Errorf("expected at least 2 InsertField for p.X and p.Y, got %d", insertCount)
	}
}

func TestLowerDerefAssign(t *testing.T) {
	prog := lowerOK(t, `
package main

func storeToPtr(p *int64) {
	*p = 42
}
`)
	pkg := prog.Packages[0]
	var fn *ir.Function
	for _, f := range pkg.Functions {
		if f.Name == "storeToPtr" {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatal("storeToPtr function not found")
	}
	// Should have a Store to a loaded pointer
	foundStore := false
	for _, bb := range fn.Blocks {
		for _, instr := range bb.Instrs {
			if instr.Op == ir.OpStore {
				foundStore = true
			}
		}
	}
	if !foundStore {
		t.Error("expected Store for *p = 42")
	}
}

func TestLowerRingPeek(t *testing.T) {
	prog := lowerOK(t, `
package main

func main() int64 {
	var r ring[int64, 4]
	r.Push(42)
	val := r.Peek()
	_ = val
	return 0
}
`)
	fn := prog.Packages[0].Functions[0]
	// Peek should generate a GEP + Load without incrementing tail
	foundGEP := false
	for _, bb := range fn.Blocks {
		for _, instr := range bb.Instrs {
			if instr.Op == ir.OpGEP {
				foundGEP = true
			}
		}
	}
	if !foundGEP {
		t.Error("expected GEP for ring Peek")
	}
}

func TestLowerAndNotAssign(t *testing.T) {
	prog := lowerOK(t, `
package main

func andNotAssign() uint32 {
	x := uint32(0xFF)
	x &^= 0x0F
	return x
}
`)
	pkg := prog.Packages[0]
	var fn *ir.Function
	for _, f := range pkg.Functions {
		if f.Name == "andNotAssign" {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatal("andNotAssign function not found")
	}
	// &^= generates XOR then AND
	foundXor := false
	foundAnd := false
	for _, bb := range fn.Blocks {
		for _, instr := range bb.Instrs {
			if instr.Op == ir.OpXor {
				foundXor = true
			}
			if instr.Op == ir.OpAnd {
				foundAnd = true
			}
		}
	}
	if !foundXor {
		t.Error("expected XOR for &^= operator")
	}
	if !foundAnd {
		t.Error("expected AND for &^= operator")
	}
}

func TestLowerParenExpr(t *testing.T) {
	prog := lowerOK(t, `
package main

func paren(a, b int32) int32 {
	return (a + b)
}
`)
	_ = prog
}

func TestLowerBlankAssign(t *testing.T) {
	prog := lowerOK(t, `
package main

func blank() {
	_ = int32(42)
}
`)
	_ = prog
}

func TestLowerEmptySwitch(t *testing.T) {
	// Switch with nil Body should still compile
	prog := lowerOK(t, `
package main

func noSwitch(x int32) int32 {
	switch x {
	}
	return x
}
`)
	_ = prog
}

func TestLowerMultipleReturnValues(t *testing.T) {
	prog := lowerOK(t, `
package main

func swap(a, b int32) (int32, int32) {
	return b, a
}
`)
	pkg := prog.Packages[0]
	var fn *ir.Function
	for _, f := range pkg.Functions {
		if f.Name == "swap" {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatal("swap function not found")
	}
	// Multiple returns should allocate return slots
	allocaCount := 0
	for _, bb := range fn.Blocks {
		for _, instr := range bb.Instrs {
			if instr.Op == ir.OpAlloca {
				allocaCount++
			}
		}
	}
	if allocaCount < 2 {
		t.Errorf("expected at least 2 alloca for return slots, got %d", allocaCount)
	}
}

func TestLowerVoidReturn(t *testing.T) {
	prog := lowerOK(t, `
package main

func doNothing() {
	return
}
`)
	pkg := prog.Packages[0]
	var fn *ir.Function
	for _, f := range pkg.Functions {
		if f.Name == "doNothing" {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatal("doNothing function not found")
	}
	// Should end with RetVoid
	foundRetVoid := false
	for _, bb := range fn.Blocks {
		if bb.Term != nil && bb.Term.Kind == ir.TermRetVoid {
			foundRetVoid = true
		}
	}
	if !foundRetVoid {
		t.Error("expected RetVoid terminator")
	}
}

func TestLowerPackedTypeWithBigEndian(t *testing.T) {
	prog := lowerOK(t, `
package main

type R packed be uint32 {
	x bits[uint, 8]
	y bits[uint, 8]
}

func main() int64 {
	var r R
	r.x = 0xAB
	val := r.x
	_ = val
	return 0
}
`)
	_ = prog // ensure big-endian packed compiles
}

func TestLowerPackedGroupFields(t *testing.T) {
	prog := lowerOK(t, `
package main

type Flags packed uint32 {
	status bits {
		READY
		ERROR
		DONE
	}
}

func main() int64 {
	var f Flags
	f.status.READY = 1
	f.status.ERROR = 0
	val := f.status.DONE
	_ = val
	return 0
}
`)
	_ = prog // ensure packed group read/write compiles

	fn := prog.Packages[0].Functions[0]
	// Should have shift/mask operations for group field access
	foundShl := false
	foundAnd := false
	for _, bb := range fn.Blocks {
		for _, instr := range bb.Instrs {
			if instr.Op == ir.OpShl {
				foundShl = true
			}
			if instr.Op == ir.OpAnd {
				foundAnd = true
			}
		}
	}
	if !foundAnd {
		t.Error("expected AND for packed group field access")
	}
	// SHL may or may not be present depending on field offset
	_ = foundShl
}

func TestLowerVolatileVar(t *testing.T) {
	prog := lowerOK(t, `
package main

func useVolatile() int32 {
	var x volatile[int32]
	x = 42
	return x
}
`)
	_ = prog // ensure volatile type compiles

	pkg := prog.Packages[0]
	var fn *ir.Function
	for _, f := range pkg.Functions {
		if f.Name == "useVolatile" {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatal("useVolatile function not found")
	}
	// Should have volatile load and store instructions
	foundVolatileLoad := false
	foundVolatileStore := false
	for _, bb := range fn.Blocks {
		for _, instr := range bb.Instrs {
			if instr.Op == ir.OpLoad && instr.IsVolatile {
				foundVolatileLoad = true
			}
			if instr.Op == ir.OpStore && instr.IsVolatile {
				foundVolatileStore = true
			}
		}
	}
	if !foundVolatileLoad {
		t.Error("expected volatile load for volatile variable read")
	}
	if !foundVolatileStore {
		t.Error("expected volatile store for volatile variable write")
	}
}

func TestLowerAlignedType(t *testing.T) {
	prog := lowerOK(t, `
package main

func useAligned() int64 {
	var x aligned[int64, 4096]
	x = 42
	return x
}
`)
	_ = prog // ensure aligned type compiles

	pkg := prog.Packages[0]
	var fn *ir.Function
	for _, f := range pkg.Functions {
		if f.Name == "useAligned" {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatal("useAligned function not found")
	}
	// Should have an AllocaAligned with alignment 4096
	foundAligned := false
	for _, bb := range fn.Blocks {
		for _, instr := range bb.Instrs {
			if instr.Op == ir.OpAlloca && instr.AllocAlign == 4096 {
				foundAligned = true
			}
		}
	}
	if !foundAligned {
		t.Error("expected aligned alloca with alignment 4096")
	}
}

func TestLowerStringInternDedup(t *testing.T) {
	// Two identical string literals should be interned (deduplicated)
	prog := lowerOK(t, `
package main

func twoStrings() {
	a := "hello"
	b := "hello"
	_ = a; _ = b
}
`)
	pkg := prog.Packages[0]
	// Only one string entry for "hello"
	count := 0
	for _, s := range pkg.Strings {
		if string(s.Data) == "hello" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 interned string for 'hello', got %d", count)
	}
}

func TestLowerEscapeSequences(t *testing.T) {
	prog := lowerOK(t, `
package main

func escapes() {
	a := "tab\there"
	b := "cr\rhere"
	c := "backslash\\here"
	d := "null\x00here"
	_ = a; _ = b; _ = c; _ = d
}
`)
	pkg := prog.Packages[0]
	// Check tab escape
	found := false
	for _, s := range pkg.Strings {
		if string(s.Data) == "tab\there" {
			found = true
		}
	}
	if !found {
		t.Error("expected interned string with tab escape")
	}
}
