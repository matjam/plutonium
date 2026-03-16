package ir_test

import (
	"os"
	"path/filepath"
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
