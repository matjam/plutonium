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

var (
	argc int
	argv unsafe.Pointer
	envp unsafe.Pointer
)

//pu:extern pluto.main
func programMain() int64

// start is called by _start with the initial stack pointer, which points at
// argc, then the argv pointers, a nil, then the environment pointers.
//
//pu:export
//pu:noreturn
func start(sp unsafe.Pointer) {
	argc = *(*int)(sp)
	argv = unsafe.Add(sp, 8)
	envp = unsafe.Add(sp, 8*(argc+2))
	installFaultHandlers()
	Exit(int(programMain()))
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

	saSiginfo  = 0x4
	saOnstack  = 0x08000000
	saRestorer = 0x04000000
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
//pu:noreturn
func sigpanic(sig int32, info, ctx unsafe.Pointer) {
	const pfWrite = 0x2                              // page fault error code: the access was a write
	addr := *(*uintptr)(unsafe.Add(info, 16))        // si_addr
	pc := *(*uintptr)(unsafe.Add(ctx, 40+16*8))      // uc_mcontext.gregs[REG_RIP]
	sp := *(*uintptr)(unsafe.Add(ctx, 40+15*8))      // gregs[REG_RSP]
	bp := *(*uintptr)(unsafe.Add(ctx, 40+10*8))      // gregs[REG_RBP]
	errCode := *(*uintptr)(unsafe.Add(ctx, 40+19*8)) // gregs[REG_ERR]
	startPanic()
	kind := IllegalInstruction
	switch sig {
	case sigSEGV, sigBUS:
		kind = MemoryFault
		if h := faultHandler; h != nil && h(addr, pc, errCode&pfWrite != 0) {
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
//
//pu:noreturn
func defaultPanic(p *Panic) {
	p.Print(nil)
	exit(2)
}

// selfID identifies the calling thread: its Linux thread ID.
func selfID() uint64 { return uint64(syscall3(sysGettid, 0, 0, 0)) }

// cpuNumber is the CPU number a hosted panic reports: 0, as hosted
// programs run on a single thread.
func cpuNumber() int { return 0 }

// otherPanicking stops a thread that panics while another handles a
// panic: it waits until that one ends the process.
//
//pu:noreturn
func otherPanicking() {
	for {
		syscall3(sysPause, 0, 0, 0)
	}
}

// lastResort ends the process after a panic inside the panic handler, or
// while a panic's message was being written: it writes a fixed message
// with a raw system call and exits with status 2.
//
//pu:noreturn
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
//
//pu:noreturn
func exit(code int) {
	syscall3(sysExitGroup, uintptr(code), 0, 0)
	for {
	}
}

// Exit ends the process with the given status, after running the exit hook
// if one is set.
//
//pu:noreturn
func Exit(code int) {
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
