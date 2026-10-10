package rt

import "unsafe"

// ----------------------------------------------------------------------------
// The panic handler. A panic, a runtime error (an index out of range, a
// division by zero and the rest) and, in hosted builds, a hardware fault
// all end in a call to the program's panic handler: the one function
// marked //pu:panic, or in a hosted program without one, defaultPanic,
// which prints the panic and exits with status 2. The compiler links the
// handler as a direct call (callPanicHandler), so it works from the first
// instruction, with nothing to register. The handler never returns, which
// the compiler checks.
//
// The panic's message is written into msgBuf by the ordinary print
// functions while capturing is set, so that the panic functions, and
// debugging tools such as the debug allocator, compose it as they would
// print it. Nothing allocates.
//
// One panic is handled at a time. The first thread or processor to panic
// claims panicOwner and runs the handler; another that panics meanwhile
// stops for good (otherPanicking), and a panic on the owner itself, inside
// the handler or while its message is written, goes to lastResort.

// A Panic describes a panic to the panic handler. It lives in the frame of
// the runtime function that called the handler, which never returns.
type Panic struct {
	// Message says what went wrong, as hosted programs print it after
	// "panic: ": "runtime error: index out of range [4] with length 4",
	// or the value passed to panic. It may span several lines and has no
	// final newline. It is cut at msgBufSize bytes.
	Message string
	// Value is the value passed to panic, or nil for a runtime error or
	// a fault.
	Value any
	// Fault describes the hardware fault the panic came from: a signal
	// in hosted builds, or an exception reported with PanicFault. Its
	// Kind is NoFault for other panics.
	Fault Fault
	// CPU is the processor that panicked: in kernel mode the number the
	// kernel gave it with SetCPU (0 until it does); otherwise 0.
	CPU int

	// Where the stack trace starts: the faulting instruction (exact),
	// or a return address in the runtime's panic functions, whose frames
	// it leaves out (skipRuntime).
	pc, sp, bp  uintptr
	exact       bool
	skipRuntime bool
}

// A FaultKind says what kind of hardware fault stopped the program.
type FaultKind uint8

const (
	NoFault            FaultKind = iota // the panic did not come from a fault
	MemoryFault                         // an access to an address that is not mapped or not allowed (SIGSEGV, SIGBUS; #PF, #GP)
	DivideFault                         // an integer division error (SIGFPE; #DE)
	IllegalInstruction                  // an instruction the processor does not accept (SIGILL; #UD)
)

// A Fault describes a hardware fault.
type Fault struct {
	Kind FaultKind
	// Addr is the address the faulting instruction accessed, for a
	// MemoryFault where it is known, or 0.
	Addr uintptr
	// PC is the address of the faulting instruction.
	PC uintptr
	// Signal is the signal that reported the fault in hosted builds
	// (11 for SIGSEGV), or 0.
	Signal int
}

// callPanicHandler calls the program's panic handler. It has no body: the
// compiler turns each call into a direct call to the function marked
// //pu:panic or, in a hosted program without one, to defaultPanic.
func callPanicHandler(p *Panic)

// casUint64 and loadUint64 are atomic operations (panic_amd64.s).
func casUint64(addr *uint64, old, new uint64) bool
func loadUint64(addr *uint64) uint64

const msgBufSize = 1024

var (
	// panicOwner is 0, or one more than the selfID of the thread or
	// processor handling a panic.
	panicOwner uint64
	// capturing is set while the owner writes the panic's message.
	capturing bool
	msgBuf    [msgBufSize]byte
	msgLen    int
)

// startPanic claims the panic for the caller and starts capturing its
// message. A second panic on the owner goes to lastResort.
func startPanic() {
	me := selfID() + 1
	if casUint64(&panicOwner, 0, me) {
		msgLen = 0
		capturing = true
		return
	}
	if loadUint64(&panicOwner) != me {
		otherPanicking()
	}
	lastResort()
}

// BeginPanic starts a panic whose message the caller prints: from now until
// it calls Abort or PanicFault, which raise the panic, print, println and
// PrintPC write into the panic's message (Panic.Message) instead of the
// console. A caller that is already writing a panic's message continues
// it. The debug allocator reports heap errors this way.
func BeginPanic() {
	if capturing && loadUint64(&panicOwner) == selfID()+1 {
		return
	}
	startPanic()
}

// captureBytes appends n bytes at p to the panic's message, dropping what
// does not fit.
func captureBytes(p unsafe.Pointer, n int) {
	for i := 0; i < n && msgLen < len(msgBuf); i++ {
		msgBuf[msgLen] = *(*byte)(unsafe.Add(p, i))
		msgLen++
	}
}

// fatal raises the panic whose message the caller has written, with the
// stack trace starting in the caller (whose runtime frames it leaves out).
// v is the value passed to panic, if any.
//
//pu:noreturn
func fatal(v any) {
	var p Panic
	p.Value = v
	var fr [3]uintptr
	callerFrame(&fr)
	p.pc, p.sp, p.bp = fr[0], fr[1], fr[2]
	p.skipRuntime = true
	raise(&p)
}

// raise finishes p and calls the panic handler.
//
//pu:noreturn
func raise(p *Panic) {
	capturing = false
	n := msgLen
	for n > 0 && msgBuf[n-1] == '\n' {
		n--
	}
	if n > 0 {
		p.Message = unsafe.String(&msgBuf[0], n)
	}
	p.CPU = cpuNumber()
	callPanicHandler(p)
	lastResort() // the compiler checks that the handler cannot return
}

// PanicFault stops the program with a panic describing a hardware fault:
// one of kind, at the instruction at pc, accessing addr (for a
// MemoryFault, if known). sp and bp are the stack pointer and RBP at the
// fault, where the stack trace starts (bp may be 0). A kernel calls it
// from its exception handlers, with the interrupted RIP and RSP from the
// interrupt frame:
//
//	rt.PanicFault(rt.MemoryFault, uintptr(x86.ReadCR2()), uintptr(f.RIP), uintptr(f.RSP), 0)
//
// If the caller has started the panic's message with BeginPanic (as
// debug.DescribeFault does for a fault in the debug allocator's heap),
// that is the message; otherwise the runtime describes the fault.
//
//pu:noreturn
func PanicFault(kind FaultKind, addr, pc, sp, bp uintptr) {
	BeginPanic()
	if msgLen == 0 {
		describeFault(kind, addr, sp)
	}
	var p Panic
	p.Fault = Fault{Kind: kind, Addr: addr, PC: pc}
	p.pc, p.sp, p.bp, p.exact = pc, sp, bp, true
	raise(&p)
}

// describeFault writes the runtime's message for a fault.
func describeFault(kind FaultKind, addr, sp uintptr) {
	switch kind {
	case MemoryFault:
		switch {
		case addr < sp+4096 && addr+65536 > sp:
			// Just below the stack pointer: the stack ran into its guard.
			printString("runtime error: stack overflow")
		case addr < 4096:
			printString("runtime error: invalid memory address or nil pointer dereference")
		default:
			printString("runtime error: unexpected fault address ")
			printPointer(unsafe.Pointer(addr))
		}
	case DivideFault:
		printString("runtime error: integer divide error")
	default:
		printString("runtime error: illegal instruction")
	}
}

// Abort stops the program the way a panic does, after the caller has
// written the panic's message following BeginPanic. The stack trace starts
// at the caller of Abort. Without BeginPanic the message is "abort".
//
//pu:noreturn
func Abort() {
	BeginPanic()
	if msgLen == 0 {
		printString("abort")
	}
	var p Panic
	var fr [3]uintptr
	callerFrame(&fr)
	p.pc, p.sp, p.bp = fr[0], fr[1], fr[2]
	p.skipRuntime = true
	raise(&p)
}

// Print writes the panic to w as a hosted program reports it: "panic: "
// and the message, a line describing the fault if there was one, then the
// stack trace.
func (p *Panic) Print(w Writer) {
	writeString(w, "panic: ")
	writeString(w, p.Message)
	writeString(w, "\n")
	if p.Fault.Kind != NoFault {
		writeString(w, "[")
		if name := signalName(p.Fault.Signal); name != "" {
			writeString(w, "signal ")
			writeString(w, name)
		} else {
			writeString(w, "fault")
		}
		writeString(w, " addr=")
		writePointer(w, p.Fault.Addr)
		writeString(w, " pc=")
		writePointer(w, p.Fault.PC)
		writeString(w, "]\n")
	}
	p.PrintTrace(w)
}

// PrintTrace writes the stack trace of the code that panicked to w, one
// frame per line, innermost first, the way a hosted program prints it
// (docs/spec/debugging.md). It reads the trace table and the stack and
// allocates nothing; a program built with --trace=false prints nothing.
func (p *Panic) PrintTrace(w Writer) {
	traceback(w, p.pc, p.sp, p.bp, p.exact, p.skipRuntime)
}
