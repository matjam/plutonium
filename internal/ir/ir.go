package ir

import "fmt"

// TypeKind identifies the kind of an IR type.
type TypeKind int

const (
	TyInvalid TypeKind = iota
	TyBool
	TyI8
	TyI16
	TyI32
	TyI64
	TyU8
	TyU16
	TyU32
	TyU64
	TyF32
	TyF64
	TyPtr      // pointer to Elem
	TyArray    // [Len]Elem
	TySlice    // { ptr *Elem, len i64, cap i64 }
	TyStruct   // fields
	TyFunc     // function type
	TyVoid     // no value
	TyAny      // tagged union { typeID uint64, data uint64 } = 16 bytes
	TyString   // fat pointer { ptr *byte, len int64 } = 16 bytes
	TyVolatile // volatile wrapper — loads/stores must not be elided/reordered
)

// Type represents an IR type.
type Type struct {
	Kind       TypeKind
	Elem       *Type    // for TyPtr, TyArray, TySlice
	Len        int64    // for TyArray
	Fields     []*Type  // for TyStruct
	FieldNames []string // for TyStruct: parallel to Fields
	Params     []*Type  // for TyFunc
	Return     *Type    // for TyFunc
}

func (t *Type) String() string {
	if t == nil {
		return "void"
	}
	switch t.Kind {
	case TyBool:
		return "bool"
	case TyI8:
		return "i8"
	case TyI16:
		return "i16"
	case TyI32:
		return "i32"
	case TyI64:
		return "i64"
	case TyU8:
		return "u8"
	case TyU16:
		return "u16"
	case TyU32:
		return "u32"
	case TyU64:
		return "u64"
	case TyF32:
		return "f32"
	case TyF64:
		return "f64"
	case TyPtr:
		return "*" + t.Elem.String()
	case TyArray:
		return fmt.Sprintf("[%d]%s", t.Len, t.Elem.String())
	case TySlice:
		return "[]" + t.Elem.String()
	case TyVoid:
		return "void"
	case TyAny:
		return "any"
	case TyString:
		return "string"
	case TyStruct:
		return "struct"
	case TyVolatile:
		return "volatile[" + t.Elem.String() + "]"
	}
	return "unknown"
}

// Pre-built common types (pointers to singletons)
var (
	Bool = &Type{Kind: TyBool}
	I8   = &Type{Kind: TyI8}
	I16  = &Type{Kind: TyI16}
	I32  = &Type{Kind: TyI32}
	I64  = &Type{Kind: TyI64}
	U8   = &Type{Kind: TyU8}
	U16  = &Type{Kind: TyU16}
	U32  = &Type{Kind: TyU32}
	U64  = &Type{Kind: TyU64}
	F32  = &Type{Kind: TyF32}
	F64  = &Type{Kind: TyF64}
	Void = &Type{Kind: TyVoid}
	UPtr = &Type{Kind: TyU64}    // uintptr = u64 on 64-bit
	Any  = &Type{Kind: TyAny}    // { typeID u64, data u64 } = 16 bytes
	Str  = &Type{Kind: TyString} // { ptr *u8, len i64 } = 16 bytes
)

// AnyTypeID constants for the `any` tagged union.
// These are compile-time-assigned, stable across compilations.
const (
	AnyTypeIDNil     int64 = 0
	AnyTypeIDBool    int64 = 1
	AnyTypeIDInt8    int64 = 2
	AnyTypeIDInt16   int64 = 3
	AnyTypeIDInt32   int64 = 4
	AnyTypeIDInt64   int64 = 5 // also int
	AnyTypeIDUint8   int64 = 6 // also byte
	AnyTypeIDUint16  int64 = 7
	AnyTypeIDUint32  int64 = 8
	AnyTypeIDUint64  int64 = 9 // also uint, uintptr
	AnyTypeIDFloat32 int64 = 10
	AnyTypeIDFloat64 int64 = 11
	AnyTypeIDPointer int64 = 12
	AnyTypeIDString  int64 = 13
)

// AnyTypeIDFor returns the any type ID for a given IR type.
func AnyTypeIDFor(t *Type) int64 {
	if t == nil {
		return AnyTypeIDNil
	}
	switch t.Kind {
	case TyBool:
		return AnyTypeIDBool
	case TyI8:
		return AnyTypeIDInt8
	case TyI16:
		return AnyTypeIDInt16
	case TyI32:
		return AnyTypeIDInt32
	case TyI64:
		return AnyTypeIDInt64
	case TyU8:
		return AnyTypeIDUint8
	case TyU16:
		return AnyTypeIDUint16
	case TyU32:
		return AnyTypeIDUint32
	case TyU64:
		return AnyTypeIDUint64
	case TyF32:
		return AnyTypeIDFloat32
	case TyF64:
		return AnyTypeIDFloat64
	case TyPtr:
		return AnyTypeIDPointer
	case TyString:
		return AnyTypeIDString
	default:
		return AnyTypeIDNil
	}
}

// SizeOf returns the size in bytes of an IR type on a 64-bit platform.
func SizeOf(t *Type) int64 {
	if t == nil {
		return 0
	}
	switch t.Kind {
	case TyBool, TyI8, TyU8:
		return 1
	case TyI16, TyU16:
		return 2
	case TyI32, TyU32, TyF32:
		return 4
	case TyI64, TyU64, TyF64, TyPtr:
		return 8
	case TyAny:
		return 16 // typeID(8) + data(8)
	case TyString:
		return 16 // ptr(8) + len(8)
	case TySlice:
		return 24 // ptr(8) + len(8) + cap(8)
	case TyArray:
		if t.Elem != nil {
			return t.Len * SizeOf(t.Elem)
		}
		return 0
	case TyStruct:
		return int64(len(t.Fields)) * 8 // all fields 8-byte aligned in v0.1
	case TyVolatile:
		if t.Elem != nil {
			return SizeOf(t.Elem)
		}
		return 8
	default:
		return 8
	}
}

// PtrTo returns a pointer type to elem.
func PtrTo(elem *Type) *Type { return &Type{Kind: TyPtr, Elem: elem} }

// SliceOf returns a slice type of elem.
func SliceOf(elem *Type) *Type { return &Type{Kind: TySlice, Elem: elem} }

// ArrayOf returns an array type.
func ArrayOf(n int64, elem *Type) *Type { return &Type{Kind: TyArray, Len: n, Elem: elem} }

// ValueKind identifies how a Value was created.
type ValueKind int

const (
	ValInstr  ValueKind = iota // result of an instruction
	ValParam                   // function parameter
	ValConst                   // constant value
	ValFunc                    // reference to a function
	ValGlobal                  // reference to a global variable
)

// Value represents a typed IR value (a virtual register or constant).
type Value struct {
	ID   int
	Kind ValueKind
	Type *Type
	Name string // optional debug name

	// For constants:
	IntVal   int64
	BoolVal  bool
	FloatVal float64
}

func (v *Value) String() string {
	if v.Name != "" {
		return "%" + v.Name
	}
	switch v.Kind {
	case ValConst:
		return fmt.Sprintf("%d", v.IntVal)
	case ValFunc:
		return "@" + v.Name
	}
	return fmt.Sprintf("%%v%d", v.ID)
}

var constIntID int

// ConstInt creates a constant integer value.
func ConstInt(typ *Type, val int64) *Value {
	constIntID++
	return &Value{ID: constIntID, Kind: ValConst, Type: typ, IntVal: val}
}

// ConstBool creates a constant boolean value.
func ConstBool(val bool) *Value {
	constIntID++
	v := &Value{ID: constIntID, Kind: ValConst, Type: Bool, BoolVal: val}
	if val {
		v.IntVal = 1
	}
	return v
}

// OpCode is an IR instruction opcode.
type OpCode int

const (
	// Arithmetic
	OpAdd OpCode = iota
	OpSub
	OpMul
	OpDiv
	OpRem
	OpAnd
	OpOr
	OpXor
	OpShl
	OpShr

	// Comparison (result is bool)
	OpCmp

	// Memory
	OpAlloca
	OpLoad
	OpStore
	OpGEP

	// Conversion
	OpExtend
	OpTruncate
	OpBitcast
	OpPtrToInt
	OpIntToPtr

	// Struct access
	OpExtractField
	OpInsertField

	// Call
	OpCall

	// Byte swap (endianness conversion)
	OpBSwap

	// Inline assembly
	OpAsm
)

// CmpOp is the comparison operator for CMP instructions.
type CmpOp int

const (
	CmpEQ CmpOp = iota
	CmpNE
	CmpLT
	CmpLE
	CmpGT
	CmpGE
	// Unsigned variants
	CmpULT
	CmpULE
	CmpUGT
	CmpUGE
)

// Instruction is a single IR instruction.
type Instruction struct {
	Op     OpCode
	Result *Value // nil for void instructions (store, br, ret)
	Args   []*Value
	// For OpCmp:
	CmpOp CmpOp
	// For OpCall:
	Callee *Function
	// For OpAlloca:
	AllocType *Type
	// For OpExtractField / OpInsertField / OpGEP:
	FieldIdx int
	// For OpGEP: element size in bytes (0 means default=8)
	ElemSize int64
	// For OpAsm:
	AsmTemplate    string
	AsmConstraints string
	// For conversions:
	FromType *Type
	ToType   *Type
	// For packed loads/stores with endianness conversion:
	NeedsBSwap bool
	// For volatile loads/stores — must not be elided, coalesced, or reordered:
	IsVolatile bool
	// For OpAlloca: explicit alignment override (0 = default)
	AllocAlign int
}

// TermKind identifies the kind of basic block terminator.
type TermKind int

const (
	TermBr      TermKind = iota // unconditional branch
	TermCondBr                  // conditional branch
	TermRet                     // return value
	TermRetVoid                 // return void
	TermUnreachable
)

// Terminator ends a basic block.
type Terminator struct {
	Kind    TermKind
	Cond    *Value      // for TermCondBr
	Then    *BasicBlock // for TermBr and TermCondBr
	Else    *BasicBlock // for TermCondBr
	RetVal  *Value      // for TermRet (nil for void)
	RetType *Type
}

// BasicBlock is a sequence of instructions ending in a terminator.
type BasicBlock struct {
	Label  string
	Instrs []*Instruction
	Term   *Terminator
	fn     *Function
}

// Function is a compiled IR function.
type Function struct {
	Name        string
	Params      []*Value
	RetType     *Type
	Blocks      []*BasicBlock
	nextID      int
	nextBlockID int
}

// Program is the top-level IR container.
type Program struct {
	Packages []*IRPackage
}

// StringLit is a string literal stored in .rodata.
type StringLit struct {
	ID   int
	Data []byte // raw bytes (without quotes, escape sequences resolved)
}

// IRPackage groups functions and globals.
type IRPackage struct {
	Name      string
	Functions []*Function
	Globals   []*Global
	Strings   []*StringLit // string literals for .rodata
}

// Global is a package-level variable.
type Global struct {
	Name string
	Type *Type
	Init *Value // nil for zero-initialized
}
