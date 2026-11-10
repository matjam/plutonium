// Translated by go2pu from internal/escape2/contract/contract.go at the go-cutover tag; edited by hand since.

// Package contract is the table of functions whose effects on pointers the
// escape analysis takes from the compiler instead of from a body
// (docs/plans/2026-10-20-escape-redesign.md, sections 3.1 and (d), I7): the
// library functions internal/lower expands inline, whose bodies never run
// from a call (a placeholder that panics, or a call of the function
// itself), and the runtime functions the lowerer calls for a construct of
// the language rather than for a call in the source.
//
// The runtime's contract with the compiler is by name in a compiler table,
// not by directive (AGENTS.md, "Directives"). Each entry says what the
// expansion or the runtime function does with the pointers it is passed.
// They are callable from safe code (section 18.12), and the containers of
// the standard library are built on them, so an entry that said less than
// the code does would let a container keep a view of a frame (section
// 18.3).
//
// The lowering oracle (internal/escape2/eir, oracle_test.go) checks the
// table against internal/lower: every call the lowerer makes to a function
// that is not an EIR call is a Runtime entry, and no Inline entry is ever
// called.
package contract

import (
	"compiler/compat"
	"compiler/pu"
	"pluto/sort"
)

// Kind says what an entry is.
type Kind uint8

const (
	// Inline is a library function the lowerer expands inline: a call of
	// it in the source is a CallContract in EIR, and the generated code
	// makes no call of it (it may call Runtime entries).
	Inline Kind = iota + 1
	// Runtime is a runtime function the lowerer calls for a construct:
	// a retain or release of a counted reference, a check that panics, a
	// channel operation, a conversion. EIR states the construct's effect
	// with its own instructions (a send is OpSink), so it has no EIR call.
	Runtime
	// Special is an entry EIR uses for a construct the lowerer expands
	// into something that is not one call: a ring's built-in methods, the
	// runtime's call of the panic handler.
	Special
	// Trusted is a function the lowerer calls as it calls any other, whose
	// body the analysis cannot see (assembly) or must not trust: its
	// effects are the entry's (plan invariant I7). A call of it is a
	// CallContract in EIR.
	Trusted
)

// A Ref names a pointer an expansion is passed: argument Arg (the receiver
// first), loaded Deref times (0: the pointer itself; 1: the pointers stored
// where it points).
type Ref struct {
	Arg   int
	Deref int
}

// A Store is a store an expansion makes: what Src leads to is stored into
// the memory Dst points to.
type Store struct {
	Src, Dst Ref
	What     string // for messages: what is stored where
}

// Effect is what a function does with the pointers it is passed.
type Effect struct {
	Stores []Store
	// Results are, for each result, the pointers it may hold; Heap says
	// a result may also hold heap pointers.
	Results [][]Ref
	Heap    []bool
	// Escapes are the pointers it keeps where they outlive the call.
	Escapes []Ref
}

// Entry is one function of the table.
type Entry struct {
	Name   string // "pluto/rt.InitValue": package path, ".", name
	Kind   Kind
	Effect Effect
	// Construct says, for a Runtime entry, which construct the lowerer
	// calls it for and so which EIR instruction stands for it.
	Construct string
}

// TaskStart is the Construct of a Trusted entry for a function that starts
// a task with its arguments: EIR marks a call of it ImplTask.
const TaskStart = "starts a task with its arguments"

// Names of the Special entries EIR uses.
const (
	// RingMethod is a built-in method of ring[T, N], expanded inline: the
	// ring's elements are stored in the ring (the receiver, argument 0);
	// what it returns comes from the ring.
	RingMethod = "ring method"
	// PanicHook is pluto/rt.callPanicHandler, the runtime's call of the
	// program's panic handler; EIR calls the handler.
	PanicHook = "pluto/rt.callPanicHandler"
)

func self(i int) Ref { return Ref{Arg: i} }
func load(i int) Ref { return Ref{Arg: i, Deref: 1} }

// table is every entry, by name.
var table = map[string]*Entry{}

func add(k Kind, construct string, eff Effect, names ...string) {
	for _, n := range names {
		table[n] = pu.New(Entry{Name: n, Kind: k, Effect: eff, Construct: construct})
	}
}

func init() {
	none := Effect{}
	// The intrinsics of internal/lower (intrinsic.go): one operation each,
	// on numbers or on machine state. A pointer argument (x86.Lgdt's
	// descriptor) is read by the instruction and not kept.
	add(Inline, "", none,
		"pluto/math.Sqrt", "pluto/math.Abs", "pluto/math.Float64bits", "pluto/math.Float64frombits",
		"pluto/math.Float32bits", "pluto/math.Float32frombits",
		"pluto/bits.TrailingZeros64", "pluto/bits.TrailingZeros32", "pluto/bits.TrailingZeros",
		"pluto/bits.LeadingZeros64", "pluto/bits.LeadingZeros32", "pluto/bits.LeadingZeros",
		"pluto/bits.ReverseBytes64", "pluto/bits.ReverseBytes32", "pluto/bits.ReverseBytes16",
		"pluto/bits.RotateLeft64", "pluto/bits.RotateLeft32", "pluto/bits.Mul64",
		"pluto/rt.CallerPC", "pluto/rt.mul64")
	for _, n := range []string{
		"Cli", "Sti", "StiHlt", "Hlt", "Pause", "SaveFlagsCli", "RestoreFlags",
		"Inb", "Inw", "Inl", "Outb", "Outw", "Outl",
		"Rdmsr", "Wrmsr",
		"ReadCR0", "ReadCR2", "ReadCR3", "ReadCR4", "WriteCR0", "WriteCR3", "WriteCR4", "Invlpg",
		"Lgdt", "Lidt", "Ltr",
		"Cpuid", "Rdtsc", "Rdtscp",
		"Swapgs", "ReadGSBase", "WriteGSBase", "GSLoad64", "GSLoad32", "GSStore64", "GSStore32",
		"FPUBegin", "FPUEnd",
	} {
		add(Inline, "", none, compat.Concat("pluto/arch/x86.", n))
	}

	// The count intrinsics of pluto/rt (lower/strops.go countIntrinsic),
	// which give library containers the compiler's drop glue and copies.
	// InitValue(p, x) moves x into *p.
	add(Inline, "", Effect{Stores: compat.Clone([]Store{{self(1), self(0), "x in *p"}})}, "pluto/rt.InitValue")
	// MoveValues and CopyValues(dst, src, n) copy the values at src to dst.
	add(Inline, "", Effect{Stores: compat.Clone([]Store{{load(1), self(0), "the values at src in *dst"}})}, "pluto/rt.MoveValues", "pluto/rt.CopyValues")
	// CopyValue(p) returns *p.
	add(Inline, "", Effect{Results: compat.Clone([][]Ref{compat.Clone([]Ref{load(0)})})}, "pluto/rt.CopyValue")
	// ViewOf(p) returns *p, or p itself for an owner.
	add(Inline, "", Effect{Results: compat.Clone([][]Ref{compat.Clone([]Ref{self(0), load(0)})})}, "pluto/rt.ViewOf")
	// SliceBytewise(s, lo, hi), strings.Slice(s, i, j) and
	// strings.SliceFrom(s, i) return a substring of s, which shares its
	// buffer.
	add(Inline, "", Effect{Results: compat.Clone([][]Ref{compat.Clone([]Ref{self(0)})})}, "pluto/rt.SliceBytewise", "pluto/strings.Slice", "pluto/strings.SliceFrom")
	// ReleaseValues(p, n) runs the drop glue of the values at p, each
	// element's Drop method with its address: EIR states that with a drop
	// of a T at p after the call (eir/call.go releaseValues), which the
	// table cannot, since the glue depends on T. ZeroValues writes zeros
	// and drops nothing.
	add(Inline, "", none,
		"pluto/rt.ReleaseValues", "pluto/rt.RetainValues", "pluto/rt.ZeroValues", "pluto/rt.NeedsDrop",
		"pluto/rt.HoldsStrings", "pluto/rt.MustCopy", "pluto/rt.StringControl", "pluto/rt.KnownASCII")
	// errors.New(text) at a call site is a static error value of a type of
	// its own (lower/errorsnew.go): static memory.
	add(Inline, "", Effect{Heap: compat.Clone([]bool{true})}, "pluto/errors.New")

	// The atomic pointer operations of pluto/sync/atomic, in assembly.
	// //pu:noescape says less than they do: LoadPointer returns what *addr
	// holds, not addr.
	add(Trusted, "", Effect{Results: compat.Clone([][]Ref{compat.Clone([]Ref{load(0)})})}, "pluto/sync/atomic.LoadPointer")
	add(Trusted, "", Effect{Stores: compat.Clone([]Store{{self(1), self(0), "val in *addr"}})}, "pluto/sync/atomic.StorePointer")
	add(Trusted, "", Effect{Stores: compat.Clone([]Store{{self(1), self(0), "new in *addr"}}), Results: compat.Clone([][]Ref{compat.Clone([]Ref{load(0)})})}, "pluto/sync/atomic.SwapPointer")
	add(Trusted, "", Effect{Stores: compat.Clone([]Store{{self(2), self(0), "new in *addr"}}), Results: compat.Clone([][]Ref{nil})}, "pluto/sync/atomic.CompareAndSwapPointer")

	// sync.WaitGroup.Task starts a task that uses the WaitGroup until it
	// calls Done, and calls f: its body hides the WaitGroup from the
	// analysis (rt.Pinned). Section 12.12: passing the address of a local
	// WaitGroup to something that starts a task is E3004; a sync.Scope is
	// the checked form.
	add(Trusted, TaskStart, Effect{Escapes: compat.Clone([]Ref{self(0), self(1)})}, "pluto/sync.WaitGroup.Task")

	// The built-in maps' operations (pluto/rt map.p), which the lowerer
	// calls for map expressions: they copy keys and elements in and out
	// of the table through unsafe pointers the analysis cannot follow.
	// The table is heap memory; a key or element stored in it is stored
	// in the heap, and what comes out of it was stored there, so is heap
	// memory too.
	heap1 := Effect{Heap: compat.Clone([]bool{true})}
	add(Trusted, "", Effect{Stores: compat.Clone([]Store{{self(1), self(0), "the key in the map"}, {self(2), self(0), "the element in the map"}})}, "pluto/rt.mapAssign")
	add(Trusted, "", Effect{Stores: compat.Clone([]Store{{self(1), self(0), "the key in the map"}}), Heap: compat.Clone([]bool{true})}, "pluto/rt.mapAssignPtr")
	// make(map[K]V, hint, a): a table allocated from a, which records a
	// to free to it (section 18.9). EIR makes the map a view of the
	// memory of a local arena a (eir/arena.go); nothing else keeps a.
	add(Trusted, "", heap1, "pluto/rt.mapMakeWith")
	add(Trusted, "", heap1, "pluto/rt.mapAccess")
	add(Trusted, "", Effect{Heap: compat.Clone([]bool{true, false})}, "pluto/rt.mapAccessOk")
	add(Trusted, "", none, "pluto/rt.mapDelete", "pluto/rt.mapLen", "pluto/rt.mapClear")
	add(Trusted, "", Effect{Stores: compat.Clone([]Store{{self(1), self(0), "the table in the iterator"}})}, "pluto/rt.MapIter.Start")
	add(Trusted, "", Effect{Heap: compat.Clone([]bool{true, true, false})}, "pluto/rt.MapIter.Next")
	add(Trusted, "", Effect{Heap: compat.Clone([]bool{true, false})}, "pluto/rt.MapIter.NextKey")

	add(Special, "", Effect{Stores: compat.Clone([]Store{{self(1), self(0), "an element in the ring"}}), Results: compat.Clone([][]Ref{compat.Clone([]Ref{self(0), load(0)})})}, RingMethod)
	add(Special, "", none, PanicHook)

	// Runtime functions the lowerer calls for constructs.
	add(Runtime, "counted references (retain, release)", none,
		"pluto/rt.strRetain", "pluto/rt.strRelease", "pluto/rt.boxRetain", "pluto/rt.boxReleaseStrong",
		"pluto/rt.chanRetain", "pluto/rt.chanRelease", "pluto/rt.weakRelease", "pluto/rt.boxUpgrade")
	add(Runtime, "boxes (new, free)", none,
		"pluto/rt.boxNew", "pluto/rt.boxFree", "pluto/rt.chanFree", "pluto/rt.chanDrop")
	// new and make with an allocator: EIR calls these (their bodies call
	// the allocator), and the lowerer calls them for the construct.
	add(Runtime, "new(T, a) and make(chan T, n, a): EIR calls them", none,
		"pluto/rt.boxNewWith", "pluto/rt.makechanWith")
	add(Runtime, "checks that panic", none,
		"pluto/rt.panicAssert", "pluto/rt.panicDivide", "pluto/rt.panicEnum", "pluto/rt.panicIndex",
		"pluto/rt.panicNil", "pluto/rt.panicRangeState", "pluto/rt.panicShift", "pluto/rt.panicSlice",
		"pluto/rt.panicSliceConvert", "pluto/rt.panicStale", "pluto/rt.panicStaleRange",
		"pluto/rt.panicTaskOverflow", "pluto/rt.panicTaskOverlap", "pluto/rt.panicTaskStep",
		"pluto/rt.panicUnion", "pluto/rt.panicUnionTag", "pluto/rt.panicValue", "pluto/rt.checkRuneBounds")
	add(Runtime, "channel operations (OpSink for a send; a receive yields heap memory)", none,
		"pluto/rt.chansend1", "pluto/rt.chanrecv1", "pluto/rt.chanrecv2", "pluto/rt.closechan",
		"pluto/rt.makechan", "pluto/rt.chanBuffered", "pluto/rt.chanlen", "pluto/rt.chancap",
		"pluto/rt.selectgo", "pluto/rt.selectnbsend", "pluto/rt.selectnbrecv")
	add(Runtime, "strings (concatenation, conversion, comparison, ranges)", none,
		"pluto/rt.concatStrings", "pluto/rt.stringFromRune", "pluto/rt.stringFromRunes",
		"pluto/rt.strcmp", "pluto/rt.strequal", "pluto/rt.decodeRune")
	add(Runtime, "interfaces (conversion, assertion, comparison)", none,
		"pluto/rt.convI2I", "pluto/rt.assertItab", "pluto/rt.ifaceEqual")
	add(Runtime, "memory (copy, clear, compare)", none,
		"pluto/rt.memmove", "pluto/rt.memclr", "pluto/rt.memequal")
	add(Runtime, "print and println", none,
		"pluto/rt.printBegin", "pluto/rt.printEnd")
	add(Runtime, "drops in progress complete before exit (section 15.4)", none,
		"pluto/rt.dropBegin", "pluto/rt.dropEnd")
	add(Runtime, "task starts (OpSink of every argument, and a call of the target)", none,
		"pluto/rt.spawnGo", "pluto/rt.spawnScoped", "pluto/rt.joinMoved")
	add(Runtime, "rt.ExitTask (section 12.12)", none,
		"pluto/rt.setGoexit", "pluto/rt.takeGoexit", "pluto/rt.mainGoexit", "pluto/rt.goexitFromC")
	add(Runtime, "errors.New call sites", none, "pluto/rt.errorsNewCount")
	add(Runtime, "program shutdown and static cleanup (section 15.4)", none,
		"pluto/rt.normalShutdown", "pluto/rt.normalShutdownFinish",
		"pluto/rt.staticCleanupCheck", "pluto/rt.staticCleanupScan", "pluto/rt.staticCleanupDone", "pluto/rt.staticCleanupPause",
		"pluto/rt.staticCleanupResume", "pluto/rt.staticCleanupSelect", "pluto/rt.staticCleanupSet")
	add(Runtime, "generation checks of custom allocators (abi.md section 1.3)", none,
		"pluto/rt.GenCustomAlloc", "pluto/rt.GenCustomFree", "pluto/rt.GenAfterCustomFree",
		"pluto/rt.GenRegisteredLive", "pluto/rt.GenRegisteredRangeLive")
}

// Lookup returns the entry of a function, by its qualified name.
func Lookup(name string) (*Entry, bool) {
	e, ok := table[name]
	return e, ok
}

// IsInline reports whether the lowerer expands a function inline.
func IsInline(name string) bool {
	e, ok := table[name]
	return ok && e.Kind == Inline
}

// IsRuntime reports whether a function is a runtime function the lowerer
// calls for a construct.
func IsRuntime(name string) bool {
	e, ok := table[name]
	return ok && e.Kind == Runtime
}

// IsTrusted reports whether a function's effects come from the table
// although the lowerer calls it.
func IsTrusted(name string) bool {
	e, ok := table[name]
	return ok && e.Kind == Trusted
}

// Names returns the names of every entry, sorted.
func Names() []string {
	out := compat.MakeCap[string](0, len(table))
	for n := range table {
		out = compat.Append(out, n)
	}
	sort.Strings(out)
	return out
}
