package x86_64_test

import (
	"testing"

	x86 "github.com/matjam/plutonium/internal/codegen/x86_64"
	"github.com/matjam/plutonium/internal/ir"
)

func TestCompileSimpleAdd(t *testing.T) {
	fn := ir.NewFunction("add", nil, ir.I32)
	entry := fn.NewBlock("entry")
	a := fn.NewParam("a", ir.I32)
	b := fn.NewParam("b", ir.I32)
	sum := entry.BinOp(ir.OpAdd, ir.I32, a, b)
	entry.Ret(ir.I32, sum)

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	if len(mf.Code) == 0 {
		t.Error("expected non-empty code")
	}
	// Verify function ends with RET (0xC3)
	if mf.Code[len(mf.Code)-1] != 0xC3 {
		t.Errorf("expected RET (0xC3) at end, got 0x%02X", mf.Code[len(mf.Code)-1])
	}
}

func TestCompileRetVoid(t *testing.T) {
	fn := ir.NewFunction("noop", nil, ir.Void)
	entry := fn.NewBlock("entry")
	entry.RetVoid()

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	if mf.Code[len(mf.Code)-1] != 0xC3 {
		t.Errorf("expected RET, got 0x%02X", mf.Code[len(mf.Code)-1])
	}
}

func TestCompileIfBranch(t *testing.T) {
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

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	if len(mf.Code) == 0 {
		t.Error("expected non-empty code for branching function")
	}
}

func TestCompileDiv(t *testing.T) {
	fn := ir.NewFunction("div", nil, ir.I64)
	entry := fn.NewBlock("entry")
	a := fn.NewParam("a", ir.I64)
	b := fn.NewParam("b", ir.I64)
	q := entry.BinOp(ir.OpDiv, ir.I64, a, b)
	entry.Ret(ir.I64, q)

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(mf.Code) == 0 {
		t.Error("expected non-empty code")
	}
}

func TestCompileShift(t *testing.T) {
	fn := ir.NewFunction("shl", nil, ir.I64)
	entry := fn.NewBlock("entry")
	a := fn.NewParam("a", ir.I64)
	b := fn.NewParam("b", ir.I64)
	r := entry.BinOp(ir.OpShl, ir.I64, a, b)
	entry.Ret(ir.I64, r)

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(mf.Code) == 0 {
		t.Error("expected non-empty code")
	}
}

func TestCompileGEP(t *testing.T) {
	fn := ir.NewFunction("gep_test", nil, ir.I64)
	entry := fn.NewBlock("entry")
	base := fn.NewParam("base", ir.PtrTo(ir.I64))
	idx := fn.NewParam("idx", ir.I64)
	ptr := entry.GEP(base, []*ir.Value{idx})
	val := entry.Load(ir.I64, ptr)
	entry.Ret(ir.I64, val)

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(mf.Code) == 0 {
		t.Error("expected non-empty code")
	}
}

func TestBackendMetadata(t *testing.T) {
	b := x86.New()
	if b.Name() != "x86_64" {
		t.Errorf("got name %q, want x86_64", b.Name())
	}
	if b.Triple() != "x86_64-linux" {
		t.Errorf("got triple %q", b.Triple())
	}
	if b.WordSize() != 8 {
		t.Errorf("got word size %d, want 8", b.WordSize())
	}
	if b.Endianness() == nil {
		t.Error("Endianness() returned nil")
	}
}

// --- Unsigned division and remainder ---

func TestCompileUnsignedDiv(t *testing.T) {
	fn := ir.NewFunction("udiv", nil, ir.U64)
	entry := fn.NewBlock("entry")
	a := fn.NewParam("a", ir.U64)
	b := fn.NewParam("b", ir.U64)
	q := entry.BinOp(ir.OpDiv, ir.U64, a, b)
	entry.Ret(ir.U64, q)

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(mf.Code) == 0 {
		t.Error("expected non-empty code")
	}
	// Unsigned division should use DIV (not IDIV)
	// DIV RCX is encoded as 48 F7 F1
	foundDIV := false
	for i := 0; i+2 < len(mf.Code); i++ {
		if mf.Code[i] == 0x48 && mf.Code[i+1] == 0xF7 && mf.Code[i+2] == 0xF1 {
			foundDIV = true
			break
		}
	}
	if !foundDIV {
		t.Error("expected DIV RCX (48 F7 F1) for unsigned division")
	}
}

func TestCompileSignedDiv(t *testing.T) {
	fn := ir.NewFunction("sdiv", nil, ir.I64)
	entry := fn.NewBlock("entry")
	a := fn.NewParam("a", ir.I64)
	b := fn.NewParam("b", ir.I64)
	q := entry.BinOp(ir.OpDiv, ir.I64, a, b)
	entry.Ret(ir.I64, q)

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// Signed division should use IDIV (not DIV)
	// IDIV RCX is encoded as 48 F7 F9
	foundIDIV := false
	for i := 0; i+2 < len(mf.Code); i++ {
		if mf.Code[i] == 0x48 && mf.Code[i+1] == 0xF7 && mf.Code[i+2] == 0xF9 {
			foundIDIV = true
			break
		}
	}
	if !foundIDIV {
		t.Error("expected IDIV RCX (48 F7 F9) for signed division")
	}
}

func TestCompileUnsignedRem(t *testing.T) {
	fn := ir.NewFunction("urem", nil, ir.U64)
	entry := fn.NewBlock("entry")
	a := fn.NewParam("a", ir.U64)
	b := fn.NewParam("b", ir.U64)
	r := entry.BinOp(ir.OpRem, ir.U64, a, b)
	entry.Ret(ir.U64, r)

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(mf.Code) == 0 {
		t.Error("expected non-empty code")
	}
	// Unsigned remainder: result is in RDX after DIV
	// DIV RCX = 48 F7 F1
	foundDIV := false
	for i := 0; i+2 < len(mf.Code); i++ {
		if mf.Code[i] == 0x48 && mf.Code[i+1] == 0xF7 && mf.Code[i+2] == 0xF1 {
			foundDIV = true
			break
		}
	}
	if !foundDIV {
		t.Error("expected DIV for unsigned remainder")
	}
}

func TestCompileSignedRem(t *testing.T) {
	fn := ir.NewFunction("srem", nil, ir.I64)
	entry := fn.NewBlock("entry")
	a := fn.NewParam("a", ir.I64)
	b := fn.NewParam("b", ir.I64)
	r := entry.BinOp(ir.OpRem, ir.I64, a, b)
	entry.Ret(ir.I64, r)

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// Signed remainder: CQO (48 99) then IDIV RCX (48 F7 F9)
	foundCQO := false
	for i := 0; i+1 < len(mf.Code); i++ {
		if mf.Code[i] == 0x48 && mf.Code[i+1] == 0x99 {
			foundCQO = true
			break
		}
	}
	if !foundCQO {
		t.Error("expected CQO (48 99) for signed remainder")
	}
}

// --- Shift operations ---

func TestCompileShr(t *testing.T) {
	fn := ir.NewFunction("shr", nil, ir.I64)
	entry := fn.NewBlock("entry")
	a := fn.NewParam("a", ir.I64)
	b := fn.NewParam("b", ir.I64)
	r := entry.BinOp(ir.OpShr, ir.I64, a, b)
	entry.Ret(ir.I64, r)

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(mf.Code) == 0 {
		t.Error("expected non-empty code for SHR")
	}
}

// --- Comparison operations ---

func TestCompileCmpEQ(t *testing.T) {
	fn := ir.NewFunction("eq", nil, ir.Bool)
	entry := fn.NewBlock("entry")
	a := fn.NewParam("a", ir.I64)
	b := fn.NewParam("b", ir.I64)
	r := entry.Cmp(ir.CmpEQ, ir.I64, a, b)
	entry.Ret(ir.Bool, r)

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// Should contain SETE (0F 94 C0)
	foundSETE := false
	for i := 0; i+2 < len(mf.Code); i++ {
		if mf.Code[i] == 0x0F && mf.Code[i+1] == 0x94 && mf.Code[i+2] == 0xC0 {
			foundSETE = true
			break
		}
	}
	if !foundSETE {
		t.Error("expected SETE (0F 94 C0) for CmpEQ")
	}
}

func TestCompileCmpNE(t *testing.T) {
	fn := ir.NewFunction("ne", nil, ir.Bool)
	entry := fn.NewBlock("entry")
	a := fn.NewParam("a", ir.I64)
	b := fn.NewParam("b", ir.I64)
	r := entry.Cmp(ir.CmpNE, ir.I64, a, b)
	entry.Ret(ir.Bool, r)

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// Should contain SETNE (0F 95 C0)
	foundSETNE := false
	for i := 0; i+2 < len(mf.Code); i++ {
		if mf.Code[i] == 0x0F && mf.Code[i+1] == 0x95 && mf.Code[i+2] == 0xC0 {
			foundSETNE = true
			break
		}
	}
	if !foundSETNE {
		t.Error("expected SETNE (0F 95 C0) for CmpNE")
	}
}

func TestCompileCmpLE(t *testing.T) {
	fn := ir.NewFunction("le", nil, ir.Bool)
	entry := fn.NewBlock("entry")
	a := fn.NewParam("a", ir.I64)
	b := fn.NewParam("b", ir.I64)
	r := entry.Cmp(ir.CmpLE, ir.I64, a, b)
	entry.Ret(ir.Bool, r)

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// Should contain SETLE (0F 9E C0)
	foundSETLE := false
	for i := 0; i+2 < len(mf.Code); i++ {
		if mf.Code[i] == 0x0F && mf.Code[i+1] == 0x9E && mf.Code[i+2] == 0xC0 {
			foundSETLE = true
			break
		}
	}
	if !foundSETLE {
		t.Error("expected SETLE (0F 9E C0) for CmpLE")
	}
}

func TestCompileCmpGT(t *testing.T) {
	fn := ir.NewFunction("gt", nil, ir.Bool)
	entry := fn.NewBlock("entry")
	a := fn.NewParam("a", ir.I64)
	b := fn.NewParam("b", ir.I64)
	r := entry.Cmp(ir.CmpGT, ir.I64, a, b)
	entry.Ret(ir.Bool, r)

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// Should contain SETG (0F 9F C0)
	foundSETG := false
	for i := 0; i+2 < len(mf.Code); i++ {
		if mf.Code[i] == 0x0F && mf.Code[i+1] == 0x9F && mf.Code[i+2] == 0xC0 {
			foundSETG = true
			break
		}
	}
	if !foundSETG {
		t.Error("expected SETG (0F 9F C0) for CmpGT")
	}
}

func TestCompileCmpGE(t *testing.T) {
	fn := ir.NewFunction("ge", nil, ir.Bool)
	entry := fn.NewBlock("entry")
	a := fn.NewParam("a", ir.I64)
	b := fn.NewParam("b", ir.I64)
	r := entry.Cmp(ir.CmpGE, ir.I64, a, b)
	entry.Ret(ir.Bool, r)

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// Should contain SETGE (0F 9D C0)
	foundSETGE := false
	for i := 0; i+2 < len(mf.Code); i++ {
		if mf.Code[i] == 0x0F && mf.Code[i+1] == 0x9D && mf.Code[i+2] == 0xC0 {
			foundSETGE = true
			break
		}
	}
	if !foundSETGE {
		t.Error("expected SETGE (0F 9D C0) for CmpGE")
	}
}

// --- Unsigned comparison operations ---

func TestCompileCmpUnsigned(t *testing.T) {
	tests := []struct {
		name string
		op   ir.CmpOp
		set  byte // expected SETcc opcode byte
	}{
		{"ULT", ir.CmpULT, 0x92},
		{"ULE", ir.CmpULE, 0x96},
		{"UGT", ir.CmpUGT, 0x97},
		{"UGE", ir.CmpUGE, 0x93},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fn := ir.NewFunction("cmp_"+tt.name, nil, ir.Bool)
			entry := fn.NewBlock("entry")
			a := fn.NewParam("a", ir.U64)
			b := fn.NewParam("b", ir.U64)
			r := entry.Cmp(tt.op, ir.U64, a, b)
			entry.Ret(ir.Bool, r)

			backend := x86.New()
			mf, err := backend.CompileFunction(fn)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			// Should contain SETcc (0F xx C0) with expected opcode
			found := false
			for i := 0; i+2 < len(mf.Code); i++ {
				if mf.Code[i] == 0x0F && mf.Code[i+1] == tt.set && mf.Code[i+2] == 0xC0 {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected SETcc (0F %02X C0) for %s", tt.set, tt.name)
			}
		})
	}
}

// --- ALU operations ---

func TestCompileSub(t *testing.T) {
	fn := ir.NewFunction("sub", nil, ir.I64)
	entry := fn.NewBlock("entry")
	a := fn.NewParam("a", ir.I64)
	b := fn.NewParam("b", ir.I64)
	r := entry.BinOp(ir.OpSub, ir.I64, a, b)
	entry.Ret(ir.I64, r)

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(mf.Code) == 0 {
		t.Error("expected non-empty code")
	}
}

func TestCompileAnd(t *testing.T) {
	fn := ir.NewFunction("and", nil, ir.I64)
	entry := fn.NewBlock("entry")
	a := fn.NewParam("a", ir.I64)
	b := fn.NewParam("b", ir.I64)
	r := entry.BinOp(ir.OpAnd, ir.I64, a, b)
	entry.Ret(ir.I64, r)

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(mf.Code) == 0 {
		t.Error("expected non-empty code")
	}
}

func TestCompileOr(t *testing.T) {
	fn := ir.NewFunction("or", nil, ir.I64)
	entry := fn.NewBlock("entry")
	a := fn.NewParam("a", ir.I64)
	b := fn.NewParam("b", ir.I64)
	r := entry.BinOp(ir.OpOr, ir.I64, a, b)
	entry.Ret(ir.I64, r)

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(mf.Code) == 0 {
		t.Error("expected non-empty code")
	}
}

func TestCompileXor(t *testing.T) {
	fn := ir.NewFunction("xor", nil, ir.I64)
	entry := fn.NewBlock("entry")
	a := fn.NewParam("a", ir.I64)
	b := fn.NewParam("b", ir.I64)
	r := entry.BinOp(ir.OpXor, ir.I64, a, b)
	entry.Ret(ir.I64, r)

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(mf.Code) == 0 {
		t.Error("expected non-empty code")
	}
}

func TestCompileMul(t *testing.T) {
	fn := ir.NewFunction("mul", nil, ir.I64)
	entry := fn.NewBlock("entry")
	a := fn.NewParam("a", ir.I64)
	b := fn.NewParam("b", ir.I64)
	r := entry.BinOp(ir.OpMul, ir.I64, a, b)
	entry.Ret(ir.I64, r)

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(mf.Code) == 0 {
		t.Error("expected non-empty code")
	}
}

// --- Memory operations ---

func TestCompileLoadStore(t *testing.T) {
	fn := ir.NewFunction("load_store", nil, ir.Void)
	entry := fn.NewBlock("entry")
	slot := entry.Alloca(ir.I64)
	val := ir.ConstInt(ir.I64, 42)
	entry.Store(ir.I64, val, slot)
	loaded := entry.Load(ir.I64, slot)
	_ = loaded
	entry.RetVoid()

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if mf.Code[len(mf.Code)-1] != 0xC3 {
		t.Errorf("expected RET at end, got 0x%02X", mf.Code[len(mf.Code)-1])
	}
}

func TestCompileStoreToPointer(t *testing.T) {
	// Store through a pointer (not stack slot) — uses PUSH/POP staging
	fn := ir.NewFunction("store_ptr", nil, ir.Void)
	entry := fn.NewBlock("entry")
	ptr := fn.NewParam("ptr", ir.PtrTo(ir.I64))
	val := ir.ConstInt(ir.I64, 99)
	entry.Store(ir.I64, val, ptr)
	entry.RetVoid()

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(mf.Code) == 0 {
		t.Error("expected non-empty code")
	}
}

func TestCompileLoadWithBSwap(t *testing.T) {
	fn := ir.NewFunction("bswap_load", nil, ir.I32)
	entry := fn.NewBlock("entry")
	slot := entry.Alloca(ir.I32)
	val := entry.LoadBSwap(ir.I32, slot, true)
	entry.Ret(ir.I32, val)

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(mf.Code) == 0 {
		t.Error("expected non-empty code")
	}
}

func TestCompileStoreBSwap(t *testing.T) {
	fn := ir.NewFunction("bswap_store", nil, ir.Void)
	entry := fn.NewBlock("entry")
	slot := entry.Alloca(ir.I32)
	val := ir.ConstInt(ir.I32, 42)
	entry.StoreBSwap(ir.I32, val, slot, true)
	entry.RetVoid()

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(mf.Code) == 0 {
		t.Error("expected non-empty code")
	}
}

// --- BSwap instruction ---

func TestCompileBSwap(t *testing.T) {
	fn := ir.NewFunction("bswap", nil, ir.I64)
	entry := fn.NewBlock("entry")
	a := fn.NewParam("a", ir.I64)
	r := entry.BSwap(ir.I64, a)
	entry.Ret(ir.I64, r)

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(mf.Code) == 0 {
		t.Error("expected non-empty code")
	}
}

// --- GEP with non-standard element sizes ---

func TestCompileGEPSized(t *testing.T) {
	// GEP with non-8 element size (e.g., 1 for byte arrays)
	fn := ir.NewFunction("gep_byte", nil, ir.I64)
	entry := fn.NewBlock("entry")
	base := fn.NewParam("base", ir.PtrTo(ir.U8))
	idx := fn.NewParam("idx", ir.I64)
	ptr := entry.GEPSized(base, []*ir.Value{idx}, 1)
	val := entry.Load(ir.U8, ptr)
	_ = val
	entry.Ret(ir.I64, ir.ConstInt(ir.I64, 0))

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(mf.Code) == 0 {
		t.Error("expected non-empty code")
	}
}

func TestCompileGEPNonStandard(t *testing.T) {
	// GEP with element size not in {1,2,4,8} — should use explicit multiply
	fn := ir.NewFunction("gep_custom", nil, ir.I64)
	entry := fn.NewBlock("entry")
	base := fn.NewParam("base", ir.PtrTo(ir.I64))
	idx := fn.NewParam("idx", ir.I64)
	ptr := entry.GEPSized(base, []*ir.Value{idx}, 12) // 12-byte elements
	val := entry.Load(ir.I64, ptr)
	entry.Ret(ir.I64, val)

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(mf.Code) == 0 {
		t.Error("expected non-empty code for non-standard GEP size")
	}
}

// --- Struct operations ---

func TestCompileExtractInsertField(t *testing.T) {
	fn := ir.NewFunction("struct_ops", nil, ir.I64)
	entry := fn.NewBlock("entry")
	// Create a struct with 3 fields
	sType := &ir.Type{Kind: ir.TyStruct, Fields: []*ir.Type{ir.I64, ir.I64, ir.I64}, FieldNames: []string{"a", "b", "c"}}
	slot := entry.Alloca(sType)
	val := ir.ConstInt(ir.I64, 42)
	entry.InsertField(ir.I64, slot, 1, val)  // store to field 1
	r := entry.ExtractField(ir.I64, slot, 1) // load from field 1
	entry.Ret(ir.I64, r)

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(mf.Code) == 0 {
		t.Error("expected non-empty code")
	}
}

// --- Call ---

func TestCompileCall(t *testing.T) {
	callee := ir.NewFunction("target", nil, ir.I64)
	fn := ir.NewFunction("caller", nil, ir.I64)
	entry := fn.NewBlock("entry")
	a := fn.NewParam("a", ir.I64)
	b := fn.NewParam("b", ir.I64)
	r := entry.Call(callee, ir.I64, a, b)
	entry.Ret(ir.I64, r)

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// Should have a CALL relocation
	if len(mf.Relocs) == 0 {
		t.Error("expected at least one relocation for the CALL")
	}
	foundCallReloc := false
	for _, r := range mf.Relocs {
		if r.Symbol == "target" {
			foundCallReloc = true
		}
	}
	if !foundCallReloc {
		t.Error("expected relocation targeting 'target' function")
	}
}

func TestCompileCallMultipleArgs(t *testing.T) {
	callee := ir.NewFunction("multi_target", nil, ir.I64)
	fn := ir.NewFunction("multi_caller", nil, ir.I64)
	entry := fn.NewBlock("entry")
	a := fn.NewParam("a", ir.I64)
	b := fn.NewParam("b", ir.I64)
	c := fn.NewParam("c", ir.I64)
	d := fn.NewParam("d", ir.I64)
	r := entry.Call(callee, ir.I64, a, b, c, d)
	entry.Ret(ir.I64, r)

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(mf.Code) == 0 {
		t.Error("expected non-empty code")
	}
}

func TestCompileCallVoid(t *testing.T) {
	callee := ir.NewFunction("void_target", nil, ir.Void)
	fn := ir.NewFunction("void_caller", nil, ir.Void)
	entry := fn.NewBlock("entry")
	entry.Call(callee, ir.Void)
	entry.RetVoid()

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if mf.Code[len(mf.Code)-1] != 0xC3 {
		t.Errorf("expected RET at end, got 0x%02X", mf.Code[len(mf.Code)-1])
	}
}

// --- Conversion operations ---

func TestCompileBitcast(t *testing.T) {
	fn := ir.NewFunction("cast", nil, ir.I64)
	entry := fn.NewBlock("entry")
	a := fn.NewParam("a", ir.I32)
	r := entry.Convert(ir.OpBitcast, ir.I32, ir.I64, a)
	entry.Ret(ir.I64, r)

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(mf.Code) == 0 {
		t.Error("expected non-empty code")
	}
}

func TestCompileBitcastConst(t *testing.T) {
	// Bitcast a constant — exercises the constant-direct-to-register path
	fn := ir.NewFunction("cast_const", nil, ir.I64)
	entry := fn.NewBlock("entry")
	val := ir.ConstInt(ir.I32, 42)
	r := entry.Convert(ir.OpBitcast, ir.I32, ir.I64, val)
	entry.Ret(ir.I64, r)

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(mf.Code) == 0 {
		t.Error("expected non-empty code")
	}
}

// --- Unreachable terminator ---

func TestCompileUnreachable(t *testing.T) {
	fn := ir.NewFunction("unreachable", nil, ir.Void)
	entry := fn.NewBlock("entry")
	entry.Unreachable()

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// Should contain UD2 (0F 0B)
	foundUD2 := false
	for i := 0; i+1 < len(mf.Code); i++ {
		if mf.Code[i] == 0x0F && mf.Code[i+1] == 0x0B {
			foundUD2 = true
			break
		}
	}
	if !foundUD2 {
		t.Error("expected UD2 (0F 0B) for unreachable")
	}
}

// --- Unconditional branch ---

func TestCompileBranch(t *testing.T) {
	fn := ir.NewFunction("branch_test", nil, ir.Void)
	entry := fn.NewBlock("entry")
	target := fn.NewBlock("target")
	entry.Br(target)
	target.RetVoid()

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if mf.Code[len(mf.Code)-1] != 0xC3 {
		t.Errorf("expected RET at end")
	}
}

// --- Aligned alloca ---

func TestCompileAllocaAligned(t *testing.T) {
	fn := ir.NewFunction("aligned", nil, ir.Void)
	entry := fn.NewBlock("entry")
	// 4096-byte aligned alloca
	slot := entry.AllocaAligned(ir.I64, 4096)
	entry.Store(ir.I64, ir.ConstInt(ir.I64, 1), slot)
	entry.RetVoid()

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(mf.Code) == 0 {
		t.Error("expected non-empty code")
	}
}

// --- Large stack frame ---

func TestCompileLargeStackFrame(t *testing.T) {
	fn := ir.NewFunction("many_locals", nil, ir.I64)
	entry := fn.NewBlock("entry")
	// Create many alloca slots to test large frame layout
	var lastSlot *ir.Value
	for i := 0; i < 20; i++ {
		slot := entry.Alloca(ir.I64)
		entry.Store(ir.I64, ir.ConstInt(ir.I64, int64(i)), slot)
		lastSlot = slot
	}
	r := entry.Load(ir.I64, lastSlot)
	entry.Ret(ir.I64, r)

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(mf.Code) == 0 {
		t.Error("expected non-empty code")
	}
}

// --- Sub-word load zero-extension ---

func TestCompileLoadByteWidth(t *testing.T) {
	fn := ir.NewFunction("load_byte", nil, ir.I64)
	entry := fn.NewBlock("entry")
	ptr := fn.NewParam("ptr", ir.PtrTo(ir.U8))
	val := entry.Load(ir.U8, ptr) // sub-64-bit load should zero-extend
	_ = val
	entry.Ret(ir.I64, ir.ConstInt(ir.I64, 0))

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(mf.Code) == 0 {
		t.Error("expected non-empty code")
	}
}

// --- Global symbol reference ---

func TestCompileGlobalRef(t *testing.T) {
	fn := ir.NewFunction("use_global", nil, ir.I64)
	entry := fn.NewBlock("entry")
	// Create a global value reference (like a string literal)
	global := &ir.Value{
		ID:     999,
		Kind:   ir.ValGlobal,
		Type:   ir.PtrTo(ir.U8),
		Name:   ".str.0",
		IntVal: 5,
	}
	slot := entry.Alloca(ir.PtrTo(ir.U8))
	entry.Store(ir.PtrTo(ir.U8), global, slot)
	entry.Ret(ir.I64, ir.ConstInt(ir.I64, 0))

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// Should have a relocation for the global reference
	foundReloc := false
	for _, r := range mf.Relocs {
		if r.Symbol == ".str.0" {
			foundReloc = true
		}
	}
	if !foundReloc {
		t.Error("expected relocation for global symbol .str.0")
	}
}

// --- Complex control flow ---

func TestCompileMultiBlock(t *testing.T) {
	fn := ir.NewFunction("multi_block", nil, ir.I64)
	entry := fn.NewBlock("entry")
	a := fn.NewParam("a", ir.I64)
	zero := ir.ConstInt(ir.I64, 0)
	cond := entry.Cmp(ir.CmpGT, ir.I64, a, zero)

	thenB := fn.NewBlock("then")
	elseB := fn.NewBlock("else")
	mergeB := fn.NewBlock("merge")

	entry.CondBr(cond, thenB, elseB)

	one := ir.ConstInt(ir.I64, 1)
	two := ir.ConstInt(ir.I64, 2)

	thenSlot := thenB.Alloca(ir.I64)
	thenB.Store(ir.I64, one, thenSlot)
	thenB.Br(mergeB)

	elseSlot := elseB.Alloca(ir.I64)
	elseB.Store(ir.I64, two, elseSlot)
	elseB.Br(mergeB)

	mergeB.Ret(ir.I64, zero)

	backend := x86.New()
	mf, err := backend.CompileFunction(fn)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(mf.Code) == 0 {
		t.Error("expected non-empty code")
	}
}
