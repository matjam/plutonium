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
//
// In hosted builds a panic first runs the panicking task's deferred calls
// (runDeferred), innermost frame first. It gives up panicOwner while they
// run, with its message copied out of msgBuf, so that other tasks go on
// (one of them may panic and end the program first, as in Go); a panic in
// one of the calls goes straight to the handler, linked to the first
// (Panic.During).

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
	// Puroutine is the ID of the puroutine that panicked (Task.ID): 1 for
	// the one that runs main, 0 outside any (a scheduler's own context).
	Puroutine uint64
	// During is the panic whose deferred calls the task was running when
	// this one happened, or nil. Only hosted builds run deferred calls on
	// a panic (language spec section 17.1).
	During *Panic

	// Where the stack trace starts: the faulting instruction (exact),
	// or a return address in the runtime's panic functions, whose frames
	// it leaves out (skipRuntime).
	pc, sp, bp  uintptr
	exact       bool
	skipRuntime bool
	// noDefers says the panic runs no deferred calls: a stack overflow, a
	// fault the fault handler explained, or a stop by Abort or
	// PanicFault, after which the program's state is not to be trusted.
	noDefers bool
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
// //pu:panic or, in a hosted program without one, to defaultPanic. The
// handler does not return, so the frame p points into outlives anything it
// does with p.
//
//pu:noescape
func callPanicHandler(p *Panic)

// casUint64 and loadUint64 are atomic operations (panic_amd64.s).
//
//pu:noescape
func casUint64(addr *uint64, old, new uint64) bool

//pu:noescape
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
		DisablePreemption() // the panic stays on this thread or processor
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
func raise(p *Panic) {
	capturing = false
	n := msgLen
	for n > 0 && msgBuf[n-1] == '\n' {
		n--
	}
	if n > 0 {
		// The message is the program's printed text, valid UTF-8, except
		// that it may be cut in the middle of a rune at msgBufSize: make
		// such bytes '?' so that the string holds valid UTF-8.
		for i := 0; i < n; {
			r, size := decodeRune(&msgBuf[0], n, i)
			if r == 0xFFFD && size == 1 {
				msgBuf[i] = '?'
			}
			i += size
		}
		p.Message = unsafe.String(&msgBuf[0], n)
	}
	p.CPU = cpuNumber()
	t := current()
	if t != nil {
		p.Puroutine = t.id
	}
	if deferOnPanic && t != nil {
		switch {
		case t.panicking != nil:
			// A panic in a deferred call that runs for a panic: straight
			// to the handler.
			p.During = t.panicking
		case !p.noDefers && t.locks == 1:
			// Only startPanic's own DisablePreemption: the task panicked
			// in ordinary code, not holding a runtime lock or in the
			// scheduler, where its deferred calls could wait for good.
			// raise never returns, so buf outlives every use of p
			// (Pinned).
			var buf [msgBufSize]byte
			bp := Pinned(&buf)
			for i := 0; i < len(p.Message); i++ {
				bp[i] = p.Message[i]
			}
			if len(p.Message) > 0 {
				p.Message = unsafe.String(&bp[0], len(p.Message))
			}
			runDeferred(t, p)
		}
	}
	callPanicHandler(p)
	lastResort() // the compiler checks that the handler cannot return
}

// runDeferred runs the pending deferred calls of t, the running task, for
// the panic p, innermost frame first. It gives up the panic while they run
// and claims it again afterwards.
func runDeferred(t *Task, p *Panic) {
	// Cleared before runDeferred returns, so p outlives the use (Pinned).
	t.panicking = Pinned(p)
	casUint64(&panicOwner, selfID()+1, 0)
	enablePreempt(t)
	walkDeferred(p)
	DisablePreemption()
	if !casUint64(&panicOwner, 0, selfID()+1) {
		otherPanicking()
	}
	t.panicking = nil
}

// walkDeferred walks the frames p's stack trace starts in and calls the
// runner of each one whose defer record has calls armed (trace.p). A
// deferred call that ends with Goexit stops the walk: the task ends with
// the panic.
//
//pu:noinline
func walkDeferred(p *Panic) {
	var w frameWalk
	w.start(traceTable(), p.pc, p.sp, p.bp, p.exact)
	for w.fn != nil && !w.bottom() {
		if rec, runner := w.deferRecord(); runner != 0 && *(*uint64)(unsafe.Pointer(rec)) != 0 {
			callDeferRunner(runner, rec)
			if takeGoexit() {
				return
			}
		}
		w.next()
	}
}

// callDeferRunner calls the runner of a defer record, a func(rec
// unsafe.Pointer) at code address runner (panic_amd64.s).
func callDeferRunner(runner, rec uintptr)

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
func PanicFault(kind FaultKind, addr, pc, sp, bp uintptr) {
	BeginPanic()
	if msgLen == 0 {
		describeFault(kind, addr, sp)
	}
	var p Panic
	p.Fault = Fault{Kind: kind, Addr: addr, PC: pc}
	p.pc, p.sp, p.bp, p.exact = pc, sp, bp, true
	p.noDefers = true
	raise(&p)
}

// stackOverflow reports whether a memory fault at addr, with the stack
// pointer at sp, is the stack running into its guard: just below the
// stack pointer, in the guard region of the stack //pu:stack reserved, or
// in the guard region below a puroutine's stack (a page, unless the
// backend recorded more with SetStackGuard).
func stackOverflow(addr, sp uintptr) bool {
	t := current()
	if t != nil && t.stackLo != 0 && addr < t.stackLo && addr+max(t.guard, pageSize) >= t.stackLo {
		return true
	}
	return addr < sp+4096 && addr+65536 > sp || addr >= guardLo && addr < guardHi
}

// describeFault writes the runtime's message for a fault.
func describeFault(kind FaultKind, addr, sp uintptr) {
	switch kind {
	case MemoryFault:
		switch {
		case stackOverflow(addr, sp):
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
	p.noDefers = true
	raise(&p)
}

// Print writes the panic to w as a hosted program reports it: "panic: "
// and the message, a line describing the fault if there was one, then the
// stack trace. A panic in a deferred call that ran for another panic
// follows that one's message, indented, as Go prints it:
//
//	panic: first
//		panic: second
//
// Print is generic in the type of w so that a call with a writer of a
// known type, such as Console, calls its Write directly: in a panic or
// interrupt handler, a call through the Writer interface counts as
// calling every Writer the program has (language spec section 20.10).
func (p *Panic) Print[W Writer](w W) {
	p.printMessages(w)
	// The fault line describes the last fault among the panics, as Go's
	// does.
	f := p
	for f.Fault.Kind == NoFault && f.During != nil {
		f = f.During
	}
	if f.Fault.Kind != NoFault {
		writeString(w, "[")
		if name := signalName(f.Fault.Signal); name != "" {
			writeString(w, "signal ")
			writeString(w, name)
		} else {
			writeString(w, "fault")
		}
		writeString(w, " addr=")
		writePointer(w, f.Fault.Addr)
		writeString(w, " pc=")
		writePointer(w, f.Fault.PC)
		writeString(w, "]\n")
	}
	if p.Puroutine > 1 {
		writeString(w, "puroutine ")
		writeUint(w, p.Puroutine)
		writeString(w, ":\n")
	}
	p.PrintTrace(w)
}

// printMessages writes "panic: " and the message of p, after those of the
// panics whose deferred calls were running when it happened.
func (p *Panic) printMessages[W Writer](w W) {
	if p.During != nil {
		p.During.printMessages(w)
		writeString(w, "\t")
	}
	writeString(w, "panic: ")
	writeString(w, p.Message)
	writeString(w, "\n")
}

// PrintTrace writes the stack trace of the code that panicked to w, one
// frame per line, innermost first, the way a hosted program prints it
// (docs/spec/debugging.md). It reads the trace table and the stack and
// allocates nothing; a program built with --trace=false prints nothing.
// For a panic in a deferred call that ran for another panic, the trace
// goes from the deferred call on to the frames of the first panic. Like
// Print, it is generic in the type of w.
func (p *Panic) PrintTrace[W Writer](w W) {
	traceback(w, p.pc, p.sp, p.bp, p.exact, p.skipRuntime, p.During)
}
