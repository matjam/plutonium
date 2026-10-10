//pu:build linux

package rt

import "unsafe"

// syscall3 and syscall6 issue Linux system calls (sys_linux_amd64.s).
func syscall3(num, a1, a2, a3 uintptr) uintptr
func syscall6(num, a1, a2, a3, a4, a5, a6 uintptr) uintptr

const (
	sysWrite       = 1
	sysMmap        = 9
	sysMprotect    = 10
	sysMunmap      = 11
	sysRtSigaction = 13
	sysPause       = 34
	sysGettid      = 186
	sysExitGroup   = 231
)

// A hosted program runs on one thread, with no puroutines, until it starts
// its first puroutine, when the runtime calls SetThreaded; string counts
// start with plain arithmetic (str.p).
const atomicCountsAtStart = false

var (
	argc int
	argv unsafe.Pointer
	envp unsafe.Pointer
)

//pu:extern pluto.main
func programMain() int64

// mainStackSize returns the size of the stack main asked for with
// //pu:stack, or 0 to run on the stack the system gave the process.
//
//pu:extern pluto.mainstack
func mainStackSize() uintptr

// start is called by _start with the initial stack pointer, which points at
// argc, then the argv pointers, a nil, then the environment pointers.
//
//pu:export
func start(sp unsafe.Pointer) {
	argc = *(*int)(sp)
	bootTask.stackHi = uintptr(sp) // so a go statement can tell a closure in this stack
	argv = unsafe.Add(sp, 8)
	envp = unsafe.Add(sp, 8*(argc+2))
	installFaultHandlers()
	if size := mainStackSize(); size > 0 {
		top := reserveStack(size)
		bootTask.stackHi = top // main runs here instead
		runOnStack(top)
	}
	stackHi = uintptr(sp)
	stackLo = systemStackLimit(stackHi)
	Exit(int(programMain()))
}

// runOnStack switches to the stack whose highest address is top and calls
// mainOnStack there (sys_linux_amd64.s).
//
//pu:noreturn
func runOnStack(top uintptr)

// mainOnStack runs the program on the stack reserved for //pu:stack.
//
//pu:export
func mainOnStack() { Exit(int(programMain())) }

// stackGuard is the inaccessible region below a reserved stack: as large as
// Linux's own gap below the process stack, so that a frame larger than a
// page still lands in it.
const stackGuard = 1 << 20

// reserveStack maps size bytes of stack above a guard region and returns
// the stack's highest address. The memory is reserved, not committed: pages
// are backed as the stack grows into them.
func reserveStack(size uintptr) uintptr {
	const (
		protRW     = 0x3
		mapFlags   = 0x22 | 0x4000 | 0x20000 // private, anonymous, no reserve, stack
		sysMmapErr = ^uintptr(4095)
	)
	total := size + stackGuard
	r := syscall6(sysMmap, 0, total, protRW, mapFlags, ^uintptr(0), 0)
	if r > sysMmapErr {
		panicMessage("runtime: cannot reserve the stack that //pu:stack asks for", "", "", "", "")
	}
	syscall3(sysMprotect, r, stackGuard, ProtNone)
	guardLo, guardHi = r, r+stackGuard
	stackLo, stackHi = r+stackGuard, r+total
	return r + total
}

// systemStackLimit returns the lowest address the process stack may grow
// to, given an address near its top: the stack's resource limit below it,
// less a margin for the arguments and environment above. It returns 0 if
// the limit is unknown or unlimited.
func systemStackLimit(top uintptr) uintptr {
	const (
		sysPrlimit64 = 302
		rlimitStack  = 3
		margin       = 64 << 10
	)
	var lim [2]uint64 // soft and hard limits
	if syscall6(sysPrlimit64, 0, rlimitStack, 0, uintptr(unsafe.Pointer(&lim)), 0, 0) != 0 {
		return 0
	}
	if lim[0] == ^uint64(0) || uintptr(lim[0]) >= top || lim[0] <= margin {
		return 0
	}
	return top - uintptr(lim[0]) + margin
}

// ----------------------------------------------------------------------------
// Hardware faults. A nil dereference, a stack overflow into a guard page or
// an overflowing division raises a signal; the handler raises a panic for
// it, with a stack trace from the faulting instruction. A memory access
// fault is first offered to the handler registered with SetFaultHandler
// (the debug allocator's), which can explain a fault in its heap.

const (
	sysSigaltstack = 131

	sigILL  = 4
	sigBUS  = 7
	sigFPE  = 8
	sigSEGV = 11
	sigPIPE = 13

	saSiginfo  = 0x4
	saOnstack  = 0x08000000
	saRestorer = 0x04000000
	saRestart  = 0x10000000
)

// signalPCs stores the addresses of sigpanic and of the signal restorer
// (sys_linux_amd64.s).
//
//pu:noescape
func signalPCs(out *[2]uintptr)

type sigactionT struct {
	handler  uintptr
	flags    uint64
	restorer uintptr
	mask     uint64
}

type stackT struct {
	sp    unsafe.Pointer
	flags int32
	_     int32
	size  uintptr
}

// faultStack is where the fault handler runs, so that it can report a stack
// overflow.
var faultStack [16 << 10]byte

func installFaultHandlers() {
	st := stackT{sp: unsafe.Pointer(&faultStack[0]), size: uintptr(len(faultStack))}
	syscall3(sysSigaltstack, uintptr(unsafe.Pointer(&st)), 0, 0)
	var pcs [2]uintptr
	signalPCs(&pcs)
	sa := sigactionT{handler: pcs[0], flags: saSiginfo | saOnstack | saRestorer, restorer: pcs[1]}
	for _, sig := range [...]uintptr{sigSEGV, sigBUS, sigFPE, sigILL} {
		syscall6(sysRtSigaction, sig, uintptr(unsafe.Pointer(&sa)), 0, 8, 0, 0)
	}
	// SIGPIPE is caught by a handler that does nothing, as in Go, so that
	// a write to a pipe with no reader fails with EPIPE instead of ending
	// the process. A handler rather than SIG_IGN: a handled signal goes
	// back to its default in a program started with exec, while an ignored
	// one stays ignored.
	h := sigpipe
	sa = sigactionT{
		handler:  *(*uintptr)(*(*unsafe.Pointer)(unsafe.Pointer(&h))),
		flags:    saSiginfo | saOnstack | saRestorer | saRestart,
		restorer: pcs[1],
	}
	syscall6(sysRtSigaction, sigPIPE, uintptr(unsafe.Pointer(&sa)), 0, 8, 0, 0)
}

// sigpipe is the SIGPIPE handler. It returns, and the write that raised
// the signal reports EPIPE.
func sigpipe(sig int32, info, ctx unsafe.Pointer) {}

// DieFromSigpipe ends the program as if SIGPIPE had its default action:
// the process is killed by the signal, which its parent sees as a death by
// SIGPIPE. pluto/os calls it when a write to standard output or standard
// error fails with EPIPE, as Go's os package does; a write to any other
// descriptor reports the error.
func DieFromSigpipe() {
	sa := sigactionT{} // SIG_DFL
	syscall6(sysRtSigaction, sigPIPE, uintptr(unsafe.Pointer(&sa)), 0, 8, 0, 0)
	syscall3(sysTgkill, uintptr(syscall3(sysGetpid, 0, 0, 0)), uintptr(selfID()), sigPIPE)
	exit(2) // not reached: the signal is delivered on return from tgkill
}

// sigpanic is the fault handler, called by the kernel with the signal
// number, its siginfo_t and the interrupted context (a ucontext_t). It
// raises a panic describing the fault, with the stack trace from the
// faulting instruction: the fault handler's description of a memory
// access fault if it recognises the address, or the runtime's own. The
// default panic handler prints it and exits with status 2 without running
// the exit hook.
//
//pu:export
func sigpanic(sig int32, info, ctx unsafe.Pointer) {
	const pfWrite = 0x2                              // page fault error code: the access was a write
	addr := *(*uintptr)(unsafe.Add(info, 16))        // si_addr
	pc := *(*uintptr)(unsafe.Add(ctx, 40+16*8))      // uc_mcontext.gregs[REG_RIP]
	sp := *(*uintptr)(unsafe.Add(ctx, 40+15*8))      // gregs[REG_RSP]
	bp := *(*uintptr)(unsafe.Add(ctx, 40+10*8))      // gregs[REG_RBP]
	errCode := *(*uintptr)(unsafe.Add(ctx, 40+19*8)) // gregs[REG_ERR]
	startPanic()
	kind := IllegalInstruction
	explained := false
	switch sig {
	case sigSEGV, sigBUS:
		kind = MemoryFault
		if h := faultHandler; h != nil && h(addr, pc, errCode&pfWrite != 0) {
			explained = true
			break // h wrote the message
		}
		describeFault(kind, addr, sp)
	case sigFPE:
		kind = DivideFault
		describeFault(kind, addr, sp)
	default:
		describeFault(kind, addr, sp)
	}
	var p Panic
	p.Fault = Fault{Kind: kind, Addr: addr, PC: pc, Signal: int(sig)}
	p.pc, p.sp, p.bp, p.exact = pc, sp, bp, true
	// A stack overflow has no stack left to run deferred calls on, and a
	// fault the debug allocator explained leaves the heap untrustworthy.
	p.noDefers = explained || kind == MemoryFault && stackOverflow(addr, sp)
	if t := current(); !p.noDefers && t != nil && t.locks == 1 && t.panicking == nil {
		// The panic will run the task's deferred calls, which must not
		// run in the signal handler, on the signal stack with the fault
		// signals blocked: resume on the task's stack, below the red
		// zone, and raise the panic there (faultResume).
		faultPanic = p
		*(*uintptr)(unsafe.Add(ctx, 40+15*8)) = (sp - 128) &^ 15 // gregs[REG_RSP]
		*(*uintptr)(unsafe.Add(ctx, 40+16*8)) = faultResumePC()  // gregs[REG_RIP]
		return
	}
	raise(&p)
}

// faultPanic is the panic sigpanic describes for raiseFault. It belongs to
// the thread that holds the panic (panicOwner).
var faultPanic Panic

// faultResumePC returns the address of faultResume (sys_linux_amd64.s).
func faultResumePC() uintptr

// raiseFault raises the fault panic sigpanic described, on the stack of the
// task that faulted (called by faultResume).
//
//pu:export
func raiseFault() {
	p := faultPanic
	raise(&p)
}

// signalName names a signal for Panic.Print, or returns "".
func signalName(sig int) string {
	switch sig {
	case sigSEGV:
		return "SIGSEGV: segmentation violation"
	case sigBUS:
		return "SIGBUS: bus error"
	case sigFPE:
		return "SIGFPE: floating-point exception"
	case sigILL:
		return "SIGILL: illegal instruction"
	}
	return ""
}

// defaultPanic is the panic handler of a hosted program that does not
// define one: it prints the panic to standard error and exits with status
// 2, without running the exit hook.
func defaultPanic(p *Panic) {
	p.Print(Console{})
	exit(2)
}

// selfID identifies the calling thread: its Linux thread ID.
func selfID() uint64 { return uint64(syscall3(sysGettid, 0, 0, 0)) }

// cpuNumber is the CPU number a hosted panic reports: 0, as hosted
// programs run on a single thread.
func cpuNumber() int { return 0 }

// deferOnPanic says a panic runs the panicking task's deferred calls
// before the panic handler: hosted builds only (language spec section 8).
const deferOnPanic = true

// otherPanicking stops a thread that panics while another handles a
// panic: it waits until that one ends the process.
func otherPanicking() {
	for {
		syscall3(sysPause, 0, 0, 0)
	}
}

// lastResort ends the process after a panic inside the panic handler, or
// while a panic's message was being written: it writes a fixed message
// with a raw system call and exits with status 2.
func lastResort() {
	const msg = "fatal: panic while handling a panic\n"
	syscall3(sysWrite, 2, uintptr(unsafe.Pointer(unsafe.StringData(msg))), uintptr(len(msg)))
	exit(2)
}

// Args returns the number of command-line arguments and a pointer to the
// argv array.
func Args() (int, unsafe.Pointer) { return argc, argv }

// Environ returns a pointer to the nil-terminated environment array.
func Environ() unsafe.Pointer { return envp }

// exit ends the process.
func exit(code int) {
	syscall3(sysExitGroup, uintptr(code), 0, 0)
	for {
	}
}

// Exit ends the process with the given status, after running the exit hook
// if one is set.
func Exit(code int) {
	flushStackCache()
	if h := exitHook; h != nil {
		exitHook = nil
		code = h(code)
	}
	exit(code)
}

var exitHook func(code int) int

// SetExitHook registers h to run when the program exits normally, because
// main returned or Exit was called; panics and faults skip it. h receives
// the exit status and returns the status to exit with. There is one hook:
// SetExitHook returns the one it replaces, which h may call to chain.
func SetExitHook(h func(code int) int) func(code int) int {
	prev := exitHook
	exitHook = h
	return prev
}

// printReentrant: a hosted print statement cannot be interrupted by code
// that prints (rt.p, printBegin).
const printReentrant = false

// consoleWrite writes n bytes at p to standard error.
func consoleWrite(p unsafe.Pointer, n int) {
	for n > 0 {
		r := int(syscall3(sysWrite, 2, uintptr(p), uintptr(n)))
		if r <= 0 {
			return
		}
		p = unsafe.Add(p, r)
		n -= r
	}
}

// Mmap maps n bytes of zeroed anonymous memory, or returns nil.
func Mmap(n uintptr) unsafe.Pointer {
	const protRW = 0x3
	const mapPrivateAnon = 0x22
	r := syscall6(sysMmap, 0, n, protRW, mapPrivateAnon, ^uintptr(0), 0)
	if r > ^uintptr(4095) {
		return nil // -errno
	}
	return unsafe.Pointer(r)
}

// Munmap unmaps memory returned by Mmap.
func Munmap(p unsafe.Pointer, n uintptr) {
	syscall3(sysMunmap, uintptr(p), n, 0)
}

// Memory protections for Mprotect.
const (
	ProtNone  = 0x0
	ProtRead  = 0x1
	ProtWrite = 0x2
)

// Mprotect sets the access to n bytes of mapped memory at p, which must be
// page-aligned, and reports whether it succeeded.
func Mprotect(p unsafe.Pointer, n uintptr, prot int) bool {
	return syscall3(sysMprotect, uintptr(p), n, uintptr(prot)) == 0
}

// ----------------------------------------------------------------------------
// Memory faults

// A FaultHandler is told about a memory access fault: an access to addr by
// the instruction at pc, a write if write is set. It runs while the panic
// that reports the fault is being written: if it recognises addr it
// prints a description of the fault, which becomes the panic's message
// (Panic.Message, so without "panic: "), and returns true; otherwise it
// prints nothing and returns false. The program stops either way.
type FaultHandler func(addr, pc uintptr, write bool) bool

var faultHandler FaultHandler

// SetFaultHandler makes h the first to hear of memory access faults
// (SIGSEGV and SIGBUS). The runtime's fault handler, installed at start,
// calls h before anything else; if h does not recognise the fault the
// runtime writes its own message. The panic then goes to the panic
// handler, with the fault in Panic.Fault and the stack trace starting at
// the faulting instruction.
func SetFaultHandler(h FaultHandler) { faultHandler = h }
