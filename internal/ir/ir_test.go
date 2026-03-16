package ir_test

import (
	"testing"

	"github.com/matjam/plutonium/internal/ir"
)

func TestBasicBlock(t *testing.T) {
	fn := ir.NewFunction("add", nil, ir.I32)
	entry := fn.NewBlock("entry")
	a := fn.NewParam("a", ir.I32)
	b := fn.NewParam("b", ir.I32)
	sum := entry.BinOp(ir.OpAdd, ir.I32, a, b)
	entry.Ret(ir.I32, sum)

	if fn.Name != "add" {
		t.Errorf("got name %q, want %q", fn.Name, "add")
	}
	if len(fn.Blocks) != 1 {
		t.Errorf("got %d blocks, want 1", len(fn.Blocks))
	}
	if len(entry.Instrs) != 1 {
		t.Errorf("got %d instructions, want 1", len(entry.Instrs))
	}
	if entry.Term == nil {
		t.Error("expected terminator")
	}
}

func TestAlloca(t *testing.T) {
	fn := ir.NewFunction("foo", nil, ir.Void)
	entry := fn.NewBlock("entry")
	slot := entry.Alloca(ir.I32)
	if slot == nil {
		t.Error("expected non-nil alloca value")
	}
	if slot.Type.Kind != ir.TyI32 {
		t.Errorf("expected alloca type I32, got %v", slot.Type.Kind)
	}
	entry.RetVoid()
}

func TestLoadStore(t *testing.T) {
	fn := ir.NewFunction("inc", nil, ir.Void)
	entry := fn.NewBlock("entry")
	p := fn.NewParam("p", &ir.Type{Kind: ir.TyPtr, Elem: ir.I32})
	val := entry.Load(ir.I32, p)
	one := ir.ConstInt(ir.I32, 1)
	sum := entry.BinOp(ir.OpAdd, ir.I32, val, one)
	entry.Store(ir.I32, sum, p)
	entry.RetVoid()

	if len(entry.Instrs) != 3 { // load, add, store
		t.Errorf("got %d instrs, want 3", len(entry.Instrs))
	}
}

func TestBranch(t *testing.T) {
	fn := ir.NewFunction("abs", nil, ir.I32)
	entry := fn.NewBlock("entry")
	n := fn.NewParam("n", ir.I32)
	zero := ir.ConstInt(ir.I32, 0)
	cond := entry.Cmp(ir.CmpLT, ir.I32, n, zero)

	negBlock := fn.NewBlock("neg")
	posBlock := fn.NewBlock("pos")
	entry.CondBr(cond, negBlock, posBlock)

	neg := negBlock.BinOp(ir.OpSub, ir.I32, zero, n)
	negBlock.Ret(ir.I32, neg)
	posBlock.Ret(ir.I32, n)

	if len(fn.Blocks) != 3 {
		t.Errorf("got %d blocks, want 3", len(fn.Blocks))
	}
}

func TestCall(t *testing.T) {
	fn := ir.NewFunction("caller", nil, ir.I32)
	callee := ir.NewFunction("callee", nil, ir.I32)
	entry := fn.NewBlock("entry")
	result := entry.Call(callee, ir.I32)
	entry.Ret(ir.I32, result)
	_ = callee
}

func TestGEP(t *testing.T) {
	fn := ir.NewFunction("gep_test", nil, ir.Void)
	entry := fn.NewBlock("entry")
	sliceTy := &ir.Type{Kind: ir.TySlice, Elem: ir.I32}
	slicePtr := fn.NewParam("s", &ir.Type{Kind: ir.TyPtr, Elem: sliceTy})
	// Get pointer to the data field (index 0)
	dataPtr := entry.GEP(slicePtr, []*ir.Value{ir.ConstInt(ir.I64, 0), ir.ConstInt(ir.I32, 0)})
	_ = dataPtr
	entry.RetVoid()
}

// --- Type.String() tests ---

func TestTypeString(t *testing.T) {
	tests := []struct {
		name string
		typ  *ir.Type
		want string
	}{
		{"nil", nil, "void"},
		{"bool", ir.Bool, "bool"},
		{"i8", ir.I8, "i8"},
		{"i16", ir.I16, "i16"},
		{"i32", ir.I32, "i32"},
		{"i64", ir.I64, "i64"},
		{"u8", ir.U8, "u8"},
		{"u16", ir.U16, "u16"},
		{"u32", ir.U32, "u32"},
		{"u64", ir.U64, "u64"},
		{"f32", ir.F32, "f32"},
		{"f64", ir.F64, "f64"},
		{"void", ir.Void, "void"},
		{"any", ir.Any, "any"},
		{"string", ir.Str, "string"},
		{"ptr", ir.PtrTo(ir.I32), "*i32"},
		{"array", ir.ArrayOf(10, ir.I64), "[10]i64"},
		{"slice", ir.SliceOf(ir.U8), "[]u8"},
		{"struct", &ir.Type{Kind: ir.TyStruct}, "struct"},
		{"volatile", &ir.Type{Kind: ir.TyVolatile, Elem: ir.U32}, "volatile[u32]"},
		{"unknown", &ir.Type{Kind: ir.TypeKind(99)}, "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.typ.String()
			if got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

// --- AnyTypeIDFor tests ---

func TestAnyTypeIDFor(t *testing.T) {
	tests := []struct {
		name string
		typ  *ir.Type
		want int64
	}{
		{"nil", nil, ir.AnyTypeIDNil},
		{"bool", ir.Bool, ir.AnyTypeIDBool},
		{"i8", ir.I8, ir.AnyTypeIDInt8},
		{"i16", ir.I16, ir.AnyTypeIDInt16},
		{"i32", ir.I32, ir.AnyTypeIDInt32},
		{"i64", ir.I64, ir.AnyTypeIDInt64},
		{"u8", ir.U8, ir.AnyTypeIDUint8},
		{"u16", ir.U16, ir.AnyTypeIDUint16},
		{"u32", ir.U32, ir.AnyTypeIDUint32},
		{"u64", ir.U64, ir.AnyTypeIDUint64},
		{"f32", ir.F32, ir.AnyTypeIDFloat32},
		{"f64", ir.F64, ir.AnyTypeIDFloat64},
		{"ptr", ir.PtrTo(ir.I32), ir.AnyTypeIDPointer},
		{"string", ir.Str, ir.AnyTypeIDString},
		{"void_fallback", ir.Void, ir.AnyTypeIDNil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ir.AnyTypeIDFor(tt.typ)
			if got != tt.want {
				t.Errorf("AnyTypeIDFor() = %d, want %d", got, tt.want)
			}
		})
	}
}

// --- SizeOf tests ---

func TestSizeOf(t *testing.T) {
	tests := []struct {
		name string
		typ  *ir.Type
		want int64
	}{
		{"nil", nil, 0},
		{"bool", ir.Bool, 1},
		{"i8", ir.I8, 1},
		{"u8", ir.U8, 1},
		{"i16", ir.I16, 2},
		{"u16", ir.U16, 2},
		{"i32", ir.I32, 4},
		{"u32", ir.U32, 4},
		{"f32", ir.F32, 4},
		{"i64", ir.I64, 8},
		{"u64", ir.U64, 8},
		{"f64", ir.F64, 8},
		{"ptr", ir.PtrTo(ir.I32), 8},
		{"any", ir.Any, 16},
		{"string", ir.Str, 16},
		{"slice", ir.SliceOf(ir.I32), 24},
		{"array", ir.ArrayOf(4, ir.I64), 32},
		{"array_nil_elem", &ir.Type{Kind: ir.TyArray, Len: 4}, 0},
		{"struct_2fields", &ir.Type{Kind: ir.TyStruct, Fields: []*ir.Type{ir.I64, ir.I32}}, 16},
		{"volatile", &ir.Type{Kind: ir.TyVolatile, Elem: ir.I32}, 4},
		{"volatile_nil_elem", &ir.Type{Kind: ir.TyVolatile}, 8},
		{"default", ir.Void, 8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ir.SizeOf(tt.typ)
			if got != tt.want {
				t.Errorf("SizeOf() = %d, want %d", got, tt.want)
			}
		})
	}
}

// --- SliceOf test ---

func TestSliceOf(t *testing.T) {
	s := ir.SliceOf(ir.I32)
	if s.Kind != ir.TySlice {
		t.Errorf("SliceOf kind = %v, want TySlice", s.Kind)
	}
	if s.Elem != ir.I32 {
		t.Error("SliceOf elem should be I32")
	}
}

// --- ConstBool test ---

func TestConstBool(t *testing.T) {
	trueVal := ir.ConstBool(true)
	if trueVal.IntVal != 1 {
		t.Errorf("ConstBool(true).IntVal = %d, want 1", trueVal.IntVal)
	}
	if !trueVal.BoolVal {
		t.Error("ConstBool(true).BoolVal should be true")
	}

	falseVal := ir.ConstBool(false)
	if falseVal.IntVal != 0 {
		t.Errorf("ConstBool(false).IntVal = %d, want 0", falseVal.IntVal)
	}
	if falseVal.BoolVal {
		t.Error("ConstBool(false).BoolVal should be false")
	}
}

// --- Value.String() test ---

func TestValueString(t *testing.T) {
	// Named value
	v1 := &ir.Value{ID: 1, Kind: ir.ValInstr, Name: "foo"}
	if got := v1.String(); got != "%foo" {
		t.Errorf("named value String() = %q, want %%foo", got)
	}

	// Const without name
	v2 := &ir.Value{ID: 2, Kind: ir.ValConst, IntVal: 42}
	if got := v2.String(); got != "42" {
		t.Errorf("const value String() = %q, want 42", got)
	}

	// Unnamed instruction value
	v3 := &ir.Value{ID: 3, Kind: ir.ValInstr}
	if got := v3.String(); got != "%v3" {
		t.Errorf("unnamed instr String() = %q, want %%v3", got)
	}
}

// --- Builder: LoadVolatile/StoreVolatile/BSwap/AllocaAligned ---

func TestLoadVolatile(t *testing.T) {
	fn := ir.NewFunction("vol_load", nil, ir.Void)
	entry := fn.NewBlock("entry")
	slot := entry.Alloca(ir.U32)
	v := entry.LoadVolatile(ir.U32, slot)
	if v == nil {
		t.Fatal("LoadVolatile returned nil")
	}
	if len(entry.Instrs) != 2 { // alloca, load
		t.Errorf("got %d instrs, want 2", len(entry.Instrs))
	}
	// Check the load instruction has IsVolatile set
	loadInstr := entry.Instrs[1]
	if !loadInstr.IsVolatile {
		t.Error("expected IsVolatile=true on LoadVolatile instruction")
	}
	entry.RetVoid()
}

func TestStoreVolatile(t *testing.T) {
	fn := ir.NewFunction("vol_store", nil, ir.Void)
	entry := fn.NewBlock("entry")
	slot := entry.Alloca(ir.U32)
	val := ir.ConstInt(ir.U32, 42)
	entry.StoreVolatile(ir.U32, val, slot)
	if len(entry.Instrs) != 2 { // alloca, store
		t.Errorf("got %d instrs, want 2", len(entry.Instrs))
	}
	storeInstr := entry.Instrs[1]
	if !storeInstr.IsVolatile {
		t.Error("expected IsVolatile=true on StoreVolatile instruction")
	}
	entry.RetVoid()
}

func TestBSwapInstr(t *testing.T) {
	fn := ir.NewFunction("bswap_test", nil, ir.Void)
	entry := fn.NewBlock("entry")
	val := ir.ConstInt(ir.U32, 0x12345678)
	result := entry.BSwap(ir.U32, val)
	if result == nil {
		t.Fatal("BSwap returned nil")
	}
	if len(entry.Instrs) != 1 {
		t.Errorf("got %d instrs, want 1", len(entry.Instrs))
	}
	if entry.Instrs[0].Op != ir.OpBSwap {
		t.Error("expected OpBSwap instruction")
	}
	entry.RetVoid()
}

func TestAllocaAligned(t *testing.T) {
	fn := ir.NewFunction("aligned_test", nil, ir.Void)
	entry := fn.NewBlock("entry")
	slot := entry.AllocaAligned(ir.I64, 4096)
	if slot == nil {
		t.Fatal("AllocaAligned returned nil")
	}
	if len(entry.Instrs) != 1 {
		t.Errorf("got %d instrs, want 1", len(entry.Instrs))
	}
	if entry.Instrs[0].AllocAlign != 4096 {
		t.Errorf("AllocAlign = %d, want 4096", entry.Instrs[0].AllocAlign)
	}
	entry.RetVoid()
}
