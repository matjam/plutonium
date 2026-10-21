// Package eir is the explicit IR the escape analysis runs on (plan
// docs/plans/2026-10-20-escape-redesign.md, section 3.1), and the one
// desugaring of the typed AST into it.
//
// Every construct the code generator turns into a call is a call here:
// explicit calls, calls through values whose core type is a signature (type
// parameters included), implicit drops (section 18.6), the forms of section
// 11.11 (Index, SetIndex, IndexOk, op=, ++, Len, All), a range over a
// function as a call of the iterator with the body as a function of its own
// (section 12.11), the wrappers and adapters the lowerer emits, deferred calls
// at each exit, task statements and sync.Scope.Task (sections 12.12, 19.4),
// and every function the lowerer expands inline, by the shared contract table.
// The lowering oracle (internal/lower CallFacts) checks that claim against
// the generated code.
//
// A function is a list of blocks of instructions over virtual registers in
// SSA form. Registers hold the values of locals whose address is never taken
// and of temporaries; every other variable is a frame object, reached through
// OpAddr, OpLoad and OpStore.
//
// This file is the interface other packages build against: change it only by
// agreement (internal/escape2 package comment).
package eir

import (
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// FuncID numbers the functions of a Program: an index into Program.Funcs.
type FuncID uint32

// NoFunc is the FuncID of no function (a callee the program does not have).
const NoFunc FuncID = ^FuncID(0)

// Reg is a virtual register of one function. Register 0 is no register.
type Reg uint32

// ObjID numbers the frame objects of one function: an index into
// Func.Objects.
type ObjID uint32

// NoObj is the ObjID of no object.
const NoObj ObjID = ^ObjID(0)

// BlockID numbers the blocks of one function: an index into Func.Blocks.
// Block 0 is the entry.
type BlockID uint32

// SiteID numbers the call sites of one function, for summaries and reasons
// that refer to a call (Instr.Call.Site).
type SiteID uint32

// Program is a whole program in EIR.
type Program struct {
	Funcs []*Func // by FuncID

	// Sources maps each function's source to its ID.
	Sources map[FuncKey]FuncID
}

// Lookup returns the function of a source, or NoFunc.
func (p *Program) Lookup(k FuncKey) FuncID {
	if id, ok := p.Sources[k]; ok {
		return id
	}
	return NoFunc
}

// FuncKey says where a function comes from. Exactly one field is set. A
// generic function has one EIR body, for its origin; calls of its instances
// call it.
type FuncKey struct {
	Obj   *types2.Func    // a declared function or method, by origin
	Lit   *ast.FuncLit    // a function literal
	Range *ast.RangeStmt  // the body of a range over a function (section 12.11)
	Init  *types2.Package // a package's initialization: its variable initializers in order
	Synth *Synth          // a function the lowerer makes that has no source
}

// SynthKind says what a synthesized function is.
type SynthKind uint8

const (
	SynthPromoted         SynthKind = iota + 1 // a promoted method: Method of the embedded field at Path of Recv
	SynthPtrWrapper                            // (*T).M calling the value method T.M
	SynthItabAdapter                           // the adapter an itab holds for Method of Recv
	SynthMethodValue                           // the function of a method value Recv.Method (record holds the receiver)
	SynthMethodExpr                            // the function of a method expression (Recv).Method, promoted ones included
	SynthIfaceMethodValue                      // a method value i.M of an interface: the record holds the interface
	SynthVariant                               // a union variant used as a function value, func(P) U
	SynthDropGlue                              // the drop of a value of type Recv: its Drop, then its fields' drops
	SynthTaskThunk                             // the function a task statement starts (Site is the statement)
	SynthDeferRecord                           // the function a deferred call runs at exit
)

// Synth identifies a synthesized function. Two Synths with the same fields
// name the same function; types are compared by identity in the Program's
// type interner, so a builder makes each Synth once and refers to it by
// pointer.
type Synth struct {
	Kind   SynthKind
	Recv   types2.Type  // the receiver or dropped type
	Method *types2.Func // the method wrapped, if any
	Path   []int        // the embedded field path, for SynthPromoted and promoted method expressions
	Site   ast.Node     // the statement, for task thunks and deferred records
}

// Func is one function in EIR.
type Func struct {
	ID   FuncID
	Key  FuncKey
	Name string // for messages and dumps: "pkg.(*T).M", "func literal in f at line 3"
	Pkg  *types2.Package
	Pos  token.Pos
	End  token.Pos
	Sig  *types2.Signature

	// Inputs are what the caller passes: the receiver first, then the
	// parameters; for a function literal or range body, its closure record
	// last. OpInput i reads input i.
	Inputs []Input
	// Results is the number of results.
	Results int

	Objects  []Object // frame objects, by ObjID
	Blocks   []*Block // by BlockID; Blocks[0] is the entry
	Loops    []Loop   // natural loops of the CFG, outermost first
	NumRegs  int      // registers are 1..NumRegs
	NumSites int      // call sites are 0..NumSites-1

	// Facts are what stage B needs from this function (plan section (b)).
	Facts Facts

	// External: called from outside Plutonium code (//pu:export,
	// //pu:interrupt, //pu:panic) or the program's entry: no caller binds
	// its inputs.
	External bool
	// Bodyless: no body (assembly, C, //pu:extern); its effects come from
	// the contract table or the default for bodyless functions.
	Bodyless bool
}

// Input is one input of a function.
type Input struct {
	Var  *types2.Var // the parameter or receiver; nil for a closure record
	Type types2.Type
}

// ObjKind says what a frame object is.
type ObjKind uint8

const (
	ObjVar         ObjKind = iota + 1 // a variable whose address is taken, or that a closure captures by reference
	ObjParam                          // a parameter's slot, when its address is taken
	ObjTemp                           // a temporary the lowerer makes in the frame
	ObjBox                            // an interface box of a value that is not direct
	ObjClosure                        // a closure record
	ObjMethodVal                      // a method value's record
	ObjSliceLit                       // the array of a slice literal
	ObjVarargs                        // the array of packed variadic arguments
	ObjStmtTemp                       // an owner temporary that lives until its statement ends (E3009)
	ObjRegion                         // a region the caller provides for a closure (section 11.5)
	ObjArenaWindow                    // a window of a local arena
)

// Lifetime orders where an object lives, shortest first. A pointer to an
// object may be stored only into a place that lives no longer than the
// object (plan section (a), "Location lifetimes").
type Lifetime uint8

const (
	LifeStmt   Lifetime = iota + 1 // until the statement ends
	LifeIter                       // until the iteration of its loop ends (section 18.4)
	LifeBlock                      // until its block ends: reserved for objects the lowerer ends at a block exit (none yet)
	LifeFrame                      // until the function returns: every plain local (section 18.2), whatever block declares it
	LifeCaller                     // memory the caller provides
	LifeHeap                       // heap or static memory
)

// Object is a frame object: memory in the function's frame.
type Object struct {
	Kind ObjKind
	Type types2.Type
	Pos  token.Pos
	Var  *types2.Var // for ObjVar and ObjParam
	Life Lifetime
	Loop int // index in Func.Loops of the innermost loop it is declared in, or -1
	Name string
}

// Loop is a natural loop of the CFG. Back edges of SSA registers are phis
// in Header.
type Loop struct {
	Header BlockID
	Blocks []BlockID
	Parent int // index of the enclosing loop, or -1
}

// Block is a basic block.
type Block struct {
	ID     BlockID
	Instrs []Instr // phis first
	Succs  []BlockID
	Preds  []BlockID
	Loop   int // innermost loop, or -1
}

// Op is the operation of an instruction. The comment of each gives its
// operands; Dst is the register it defines, if any.
type Op uint8

const (
	OpAddr      Op = iota + 1 // Dst = &Obj.Path (Path: field indices; an element step is ElemStep)
	OpHeap                    // Dst = a pointer to heap or static memory (Heap says which)
	OpInput                   // Dst = input Index
	OpLoad                    // Dst = *X (Type: the type of the location read)
	OpStore                   // *X = Y (Type: the type of the location written)
	OpField                   // Dst = &(*X).Path: an offset into what X points to (Type: the type of what X points to)
	OpElem                    // Dst = &(*X)[i]: an element of the array or slice X points to (Type: the type of what X points to)
	OpSelect                  // Dst = X.Path: a field of the struct value in X (no memory)
	OpCompose                 // Dst = a struct value whose field Fields[i] holds Args[i]
	OpCopy                    // Dst = X
	OpConv                    // Dst = X converted from Type to To (an interface): boxes into Obj when X is not direct
	OpClosure                 // Dst = the closure of Callee; its record Obj holds Args (captures, in record order); Obj NoObj: a static record (no captures)
	OpMethodVal               // Dst = the method value X.Method; its record Obj holds X at field 0; Obj NoObj: a static record
	OpAssert                  // Dst = X.(Type): Type a concrete type or an interface
	OpCall                    // Call
	OpSink                    // X goes to Sink (Index: the result, for SinkReturn)
	OpPhi                     // Dst = one of Args, by predecessor
	OpDrop                    // the end of Obj's lifetime (the place where E3006 and E3009 are checked)
)

// ElemStep is the path step of an array or slice element in OpAddr and
// OpField paths: elements are not told apart.
const ElemStep = -1

// HeapKind says what OpHeap's pointer is.
type HeapKind uint8

const (
	HeapAlloc   HeapKind = iota + 1 // a fresh allocation
	HeapStatic                      // static memory: a package variable's address, a constant, a string's bytes
	HeapUintptr                     // a pointer made from a uintptr (section 18.3)
	HeapCounted                     // a counted reference: owned/shared/synced box, map, channel
)

// SinkKind says where a value goes that outlives the frame or leaves it.
type SinkKind uint8

const (
	SinkReturn   SinkKind = iota + 1 // result Index of the function
	SinkGlobal                       // stored in package memory
	SinkSend                         // sent on a channel
	SinkTask                         // passed to a task statement
	SinkExternal                     // passed to code outside Plutonium
)

// Instr is one instruction. Which fields mean something depends on Op.
type Instr struct {
	Op     Op
	Dst    Reg
	X, Y   Reg
	Args   []Reg
	Obj    ObjID
	Path   []int   // field indices, ElemStep for an element
	Fields [][]int // OpCompose: the field path each of Args fills
	Index  int     // OpInput: the input; OpSink: the result
	Type   types2.Type
	To     types2.Type // OpConv: the interface converted to
	Heap   HeapKind
	Sink   SinkKind
	Method *types2.Func // OpMethodVal
	Callee FuncID       // OpClosure: the literal's function
	Call   *Call        // OpCall
	Pos    token.Pos
}

// CallKind says how a call finds its callee.
type CallKind uint8

const (
	CallStatic    CallKind = iota + 1 // Callee
	CallIface                         // method Method of the interface value Target, of type Iface
	CallTypeParam                     // method Method of a value of type parameter type: every method of that name and a matching signature over all instantiations
	CallFunc                          // the func value Target, of signature Sig
	CallContract                      // Contract, an entry of the contract table (a function the lowerer expands inline, a runtime hook)
)

// Implicit says what source construct made a call, for messages and for the
// lowering oracle: zero for a call written in the source.
type Implicit uint8

const (
	ImplNone      Implicit = iota
	ImplDrop               // an implicit drop (section 18.6)
	ImplIndex              // x[i] on a container (section 11.11)
	ImplSetIndex           // x[i] = v
	ImplIndexOk            // v, ok := x[i]
	ImplOpAssign           // x[i] op= v, x[i]++
	ImplLen                // len(x)
	ImplAll                // range over a container: its All method
	ImplRangeIter          // range over a function: the call of the iterator
	ImplDefer              // a deferred call, at an exit
	ImplTask               // a task statement or Scope.Task
	ImplWrapper            // a call inside a synthesized wrapper
)

// Call is the call of an OpCall instruction.
type Call struct {
	Kind     CallKind
	Callee   FuncID       // CallStatic
	Method   *types2.Func // CallIface, CallTypeParam: the method as the interface or constraint declares it
	Iface    types2.Type  // CallIface: the interface type; CallTypeParam: the type parameter
	Sig      *types2.Signature
	Target   Reg    // CallIface: the interface value; CallFunc: the func value
	Contract string // CallContract
	// Args are the arguments: receiver first for static method calls; for
	// CallIface and CallTypeParam the method's parameters without the
	// receiver, which is Target's data word (a candidate's input 0 binds
	// to it); for CallFunc the func value's parameters. They are registers
	// like any other, so stage B sees their sources (OpHeap, loads, inputs)
	// as it does at a static call, for every candidate's parameters.
	Args     []Reg
	Results  []Reg // one per result; 0 for a result not used
	Implicit Implicit
	Site     SiteID
}

// Facts are what stage B reads of a function, collected while it is built
// (plan section (b), stage A).
type Facts struct {
	// Converted are the types converted to an interface in the function,
	// as written (a type parameter stays one; stage B substitutes).
	Converted []types2.Type
	// FuncValues are the functions used as func values: declared
	// functions, literals, method values and expressions, variants.
	FuncValues []FuncID
	// Instances are the instantiations the function makes: a generic
	// function or type with its type arguments.
	Instances []Instance
	// FieldWrites are the struct values written into places that hold them
	// by value, for heap-only fields (plan invariant I6).
	FieldWrites []FieldWrite
}

// Instance is one instantiation.
type Instance struct {
	Obj  types2.Object // the generic function or type name
	Args []types2.Type
}

// FieldWrite records that a value of struct type Type is written at Pos into
// a place of a type that holds it by value. From is the register written; a
// heap-only analysis reads what it may point to from the facts of its
// source (allocation, nil, a load of a field, an input).
type FieldWrite struct {
	Type types2.Type
	From Reg
	Pos  token.Pos
}
