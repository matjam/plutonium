package ssa

// An Op is an SSA operation.
//
// Generic ops are produced by lowering and handled by the machine-independent
// passes. Architecture backends add their own ops after OpGenericEnd.
type Op int32

// opFlags describe properties the passes rely on.
type opFlags uint16

const (
	fCommutative opFlags = 1 << iota // args 0 and 1 may be swapped
	fSideEffect                      // must not be removed even if unused (beyond producing memory)
	fMemArg                          // last argument is a memory state
	fMemResult                       // produces a memory state (alone or in a tuple)
	fCall                            // a call: clobbers caller-saved registers
	fNoCSE                           // two copies must not be merged
)

// An OpInfo describes an op.
type OpInfo struct {
	Name  string
	Args  int // number of arguments, or -1 for variable
	Flags opFlags
}

func (o Op) Info() *OpInfo  { return &opTable[o] }
func (o Op) String() string { return opTable[o].Name }

func (o Op) IsCommutative() bool { return opTable[o].Flags&fCommutative != 0 }
func (o Op) HasSideEffect() bool { return opTable[o].Flags&fSideEffect != 0 }
func (o Op) HasMemArg() bool     { return opTable[o].Flags&fMemArg != 0 }
func (o Op) HasMemResult() bool  { return opTable[o].Flags&fMemResult != 0 }
func (o Op) IsCall() bool        { return opTable[o].Flags&fCall != 0 }
func (o Op) NoCSE() bool         { return opTable[o].Flags&fNoCSE != 0 }

const (
	OpInvalid Op = iota

	// Values without inputs.
	OpConst      // integer, bool or pointer constant in AuxInt
	OpConstFloat // float constant; AuxInt holds the IEEE bits of a float64
	OpArg        // incoming parameter AuxInt (index into the flattened parameter list)
	OpInitMem    // the memory state on function entry
	OpSlotAddr   // address of stack slot Aux (*Slot)
	OpSymAddr    // address of global symbol Aux (*Sym)
	OpFuncAddr   // address of function Aux (*Sym), as a function value code pointer
	OpClosurePtr // the closure context pointer the caller passed (internal ABI)
	OpReturnAddr // the function's return address (a code address in its caller)
	OpCodeAddr   // the address of the code at this point (an OpReturnAddr once inlined)
	// OpPerCPUOffset is the link-time address of per-CPU symbol Aux (*Sym):
	// its offset in a per-CPU block, as an i64. (OpSymAddr of a per-CPU
	// symbol instead addresses the current processor's copy, GS-relative,
	// at the moment of each access.)
	OpPerCPUOffset

	// Integer arithmetic. Operands and result share the type.
	OpAdd
	OpSub
	OpMul
	OpDiv  // signed, truncating
	OpDivu // unsigned
	OpMod  // signed remainder
	OpModu // unsigned remainder
	OpAnd
	OpOr
	OpXor
	OpAndNot // a &^ b
	OpShl    // shift count is arg 1, any integer type; counts >= width give 0
	OpShrL   // logical right shift; counts >= width give 0
	OpShrA   // arithmetic right shift; counts >= width give the sign
	OpNeg
	OpCom    // bitwise complement
	OpMulHi  // high half of signed product
	OpMulHiu // high half of unsigned product

	// Floating point arithmetic.
	OpFAdd
	OpFSub
	OpFMul
	OpFDiv
	OpFNeg
	OpFSqrt
	OpFAbs

	// Comparisons produce bool.
	OpEq
	OpNe
	OpLess   // signed <
	OpLeq    // signed <=
	OpLessu  // unsigned <
	OpLequ   // unsigned <=
	OpFEq    // float ==, false for NaN
	OpFNe    // float !=, true for NaN
	OpFLess  // float <, false for NaN
	OpFLeq   // float <=, false for NaN
	OpNot    // bool negation
	OpSelect // arg0 ? arg1 : arg2

	// Conversions.
	OpSignExt    // widen signed integer to the result size
	OpZeroExt    // widen unsigned integer or bool to the result size
	OpTrunc      // narrow integer
	OpIntToFloat // signed integer to float
	OpUintToFloat
	OpFloatToInt // truncating float to signed integer
	OpFloatToUint
	OpFloatConv // float32 <-> float64
	OpBitcast   // reinterpret bits between same-size scalars (int, float, ptr)

	// Bit operations (pluto/bits intrinsics).
	OpPopCount
	OpCtz   // count trailing zeros; width for zero
	OpClz   // count leading zeros; width for zero
	OpBswap // reverse byte order
	OpRotl  // rotate left by arg1

	// Vectors (see TF64x2). Float arithmetic, loads and stores also take
	// vector types.
	OpVPack // vector of arg0 (lane 0) and arg1 (lane 1)
	OpVLane // lane AuxInt of vector arg0

	// Pointers.
	OpOffPtr // arg0 + AuxInt
	OpAddPtr // arg0 + arg1 (arg1 an integer)

	// Memory.
	OpLoad  // load Type from arg0; arg1 is memory. AuxInt&MemVolatile for volatile.
	OpStore // store arg1 (type in Aux as *Type) to arg0; arg2 is memory; result memory
	OpZero  // zero AuxInt bytes at arg0; arg1 memory; result memory
	OpMove  // copy AuxInt bytes from arg1 to arg0; arg2 memory; result memory
	OpFill  // set arg2 bytes (an i64, not negative) at arg0 to byte arg1; arg3 memory; result memory
	// OpVolatileRead orders volatile load arg0, which read memory arg1: its
	// result memory follows the load, so later loads and stores neither
	// merge with it nor move above it. It emits no code.
	OpVolatileRead
	OpVarDef // marks the start of slot Aux's lifetime; arg0 memory; result memory

	// OpIntrinsic is a machine instruction sequence named by Aux (an
	// Intrinsic, such as "x86.Cli"), expanded inline by the backend. The
	// last argument is memory; the result is a tuple of the memory state
	// followed by the sequence's results, read with SelectN as for calls.
	// It is a full compiler barrier: it consumes and produces memory, so
	// no load or store moves across it, and it is never removed or merged.
	OpIntrinsic

	// Calls. The last argument is memory; the result is a tuple of the
	// memory state followed by the callee's results.
	OpCall             // static call to Aux (*Sym); AuxCall in Aux2 describes the ABI
	OpCallInd          // indirect call through code pointer arg0
	OpSelectN          // element AuxInt of tuple arg0
	OpPhi              // one argument per predecessor, in predecessor order
	OpCopy             // arg0, removed by copyelim
	OpRegion           // arg0, a closure region passed to a call: a copy that marks it (Slot.Parts)
	OpUnreachableValue // placeholder for a value on a path that cannot execute

	OpGenericEnd
)

// An Intrinsic names the machine instruction sequence of an OpIntrinsic.
// The backend for the target defines the names it accepts. A value's Aux
// holds a *Intrinsic (NewIntrinsic): the self-hosted compiler keeps no
// string in an interface, whose box would live in one frame.
type Intrinsic string

// NewIntrinsic returns the Aux of an OpIntrinsic that runs the sequence name.
func NewIntrinsic(name string) *Intrinsic {
	p := new(Intrinsic)
	*p = Intrinsic(name)
	return p
}

// Memory access flags in OpLoad / OpStore AuxInt.
const (
	MemVolatile int64 = 1 << iota // never removed, merged or reordered
	// MemStatic marks a load from memory that is never freed, whose
	// address carries no generation tag: an itab or a type descriptor,
	// which the compiler lays out in static data. Generation checks
	// (opt.GenCheck) leave it alone.
	MemStatic
)

var opTable = [opTableSize]OpInfo{
	OpInvalid:    {Name: "Invalid"},
	OpConst:      {Name: "Const", Args: 0},
	OpConstFloat: {Name: "ConstFloat", Args: 0},
	OpArg:        {Name: "Arg", Args: 0, Flags: fNoCSE},
	OpInitMem:    {Name: "InitMem", Args: 0, Flags: fNoCSE},
	OpSlotAddr:   {Name: "SlotAddr", Args: 0},
	OpSymAddr:    {Name: "SymAddr", Args: 0},
	OpFuncAddr:   {Name: "FuncAddr", Args: 0},
	OpClosurePtr: {Name: "ClosurePtr", Args: 0, Flags: fNoCSE},
	OpReturnAddr: {Name: "ReturnAddr", Args: 0},
	OpCodeAddr:   {Name: "CodeAddr", Args: 0, Flags: fNoCSE},

	OpPerCPUOffset: {Name: "PerCPUOffset", Args: 0},

	OpAdd:    {Name: "Add", Args: 2, Flags: fCommutative},
	OpSub:    {Name: "Sub", Args: 2},
	OpMul:    {Name: "Mul", Args: 2, Flags: fCommutative},
	OpDiv:    {Name: "Div", Args: 2},
	OpDivu:   {Name: "Divu", Args: 2},
	OpMod:    {Name: "Mod", Args: 2},
	OpModu:   {Name: "Modu", Args: 2},
	OpAnd:    {Name: "And", Args: 2, Flags: fCommutative},
	OpOr:     {Name: "Or", Args: 2, Flags: fCommutative},
	OpXor:    {Name: "Xor", Args: 2, Flags: fCommutative},
	OpAndNot: {Name: "AndNot", Args: 2},
	OpShl:    {Name: "Shl", Args: 2},
	OpShrL:   {Name: "ShrL", Args: 2},
	OpShrA:   {Name: "ShrA", Args: 2},
	OpNeg:    {Name: "Neg", Args: 1},
	OpCom:    {Name: "Com", Args: 1},
	OpMulHi:  {Name: "MulHi", Args: 2, Flags: fCommutative},
	OpMulHiu: {Name: "MulHiu", Args: 2, Flags: fCommutative},

	OpFAdd:  {Name: "FAdd", Args: 2, Flags: fCommutative},
	OpFSub:  {Name: "FSub", Args: 2},
	OpFMul:  {Name: "FMul", Args: 2, Flags: fCommutative},
	OpFDiv:  {Name: "FDiv", Args: 2},
	OpFNeg:  {Name: "FNeg", Args: 1},
	OpFSqrt: {Name: "FSqrt", Args: 1},
	OpFAbs:  {Name: "FAbs", Args: 1},

	OpEq:     {Name: "Eq", Args: 2, Flags: fCommutative},
	OpNe:     {Name: "Ne", Args: 2, Flags: fCommutative},
	OpLess:   {Name: "Less", Args: 2},
	OpLeq:    {Name: "Leq", Args: 2},
	OpLessu:  {Name: "Lessu", Args: 2},
	OpLequ:   {Name: "Lequ", Args: 2},
	OpFEq:    {Name: "FEq", Args: 2, Flags: fCommutative},
	OpFNe:    {Name: "FNe", Args: 2, Flags: fCommutative},
	OpFLess:  {Name: "FLess", Args: 2},
	OpFLeq:   {Name: "FLeq", Args: 2},
	OpNot:    {Name: "Not", Args: 1},
	OpSelect: {Name: "Select", Args: 3},

	OpSignExt:     {Name: "SignExt", Args: 1},
	OpZeroExt:     {Name: "ZeroExt", Args: 1},
	OpTrunc:       {Name: "Trunc", Args: 1},
	OpIntToFloat:  {Name: "IntToFloat", Args: 1},
	OpUintToFloat: {Name: "UintToFloat", Args: 1},
	OpFloatToInt:  {Name: "FloatToInt", Args: 1},
	OpFloatToUint: {Name: "FloatToUint", Args: 1},
	OpFloatConv:   {Name: "FloatConv", Args: 1},
	OpBitcast:     {Name: "Bitcast", Args: 1},

	OpPopCount: {Name: "PopCount", Args: 1},
	OpCtz:      {Name: "Ctz", Args: 1},
	OpClz:      {Name: "Clz", Args: 1},
	OpBswap:    {Name: "Bswap", Args: 1},
	OpRotl:     {Name: "Rotl", Args: 2},

	OpVPack: {Name: "VPack", Args: 2},
	OpVLane: {Name: "VLane", Args: 1},

	OpOffPtr: {Name: "OffPtr", Args: 1},
	OpAddPtr: {Name: "AddPtr", Args: 2},

	OpLoad:  {Name: "Load", Args: 2, Flags: fMemArg},
	OpStore: {Name: "Store", Args: 3, Flags: fMemArg | fMemResult},
	OpZero:  {Name: "Zero", Args: 2, Flags: fMemArg | fMemResult},
	OpMove:  {Name: "Move", Args: 3, Flags: fMemArg | fMemResult},
	OpFill:  {Name: "Fill", Args: 4, Flags: fMemArg | fMemResult},

	OpVolatileRead: {Name: "VolatileRead", Args: 2, Flags: fMemArg | fMemResult},
	OpVarDef:       {Name: "VarDef", Args: 1, Flags: fMemArg | fMemResult},

	OpIntrinsic: {Name: "Intrinsic", Args: -1, Flags: fMemArg | fMemResult | fSideEffect | fNoCSE},

	OpCall:             {Name: "Call", Args: -1, Flags: fMemArg | fMemResult | fCall | fSideEffect},
	OpCallInd:          {Name: "CallInd", Args: -1, Flags: fMemArg | fMemResult | fCall | fSideEffect},
	OpSelectN:          {Name: "SelectN", Args: 1},
	OpPhi:              {Name: "Phi", Args: -1},
	OpCopy:             {Name: "Copy", Args: 1},
	OpRegion:           {Name: "Region", Args: 1},
	OpUnreachableValue: {Name: "Unreachable", Args: 0},

	OpGenericEnd: {Name: "GenericEnd"},
}

// opTableSize leaves room for architecture ops registered with RegisterOps.
const opTableSize = 1024

var nextArchOp = OpGenericEnd + 1

// RegisterOps adds architecture-specific ops and returns the first one.
// Ops are numbered consecutively in the order given.
func RegisterOps(infos []OpInfo) Op {
	first := nextArchOp
	for _, info := range infos {
		if nextArchOp >= opTableSize {
			panic("ssa: op table full")
		}
		opTable[nextArchOp] = info
		nextArchOp++
	}
	return first
}
