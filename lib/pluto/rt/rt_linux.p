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
	sysExitGroup   = 231
)

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
//pu:noreturn
func start(sp unsafe.Pointer) {
	argc = *(*int)(sp)
	argv = unsafe.Add(sp, 8)
	envp = unsafe.Add(sp, 8*(argc+2))
	installFaultHandlers()
	if size := mainStackSize(); size > 0 {
		runOnStack(reserveStack(size))
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
//pu:noreturn
func mainOnStack() { Exit(int(programMain())) }

// stackGuard is the inaccessible region below a reserved stack: as large as
// Linux's own gap below the process stack, so that a frame larger than a
// page still lands in it.
const stackGuard = 1 << 20

// guardLo and guardHi bound the guard region of the reserved stack, so the
// fault handler can tell a stack overflow from another fault.
var guardLo, guardHi uintptr

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
// an overflowing division raises a signal; the handler reports it like a
// panic, with a stack trace from the faulting instruction. A memory access
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
// prints the panic message (the fault handler's description of a memory
// access fault, if it recognises the address, or the runtime's own), then
// the signal, then the stack trace from the faulting instruction, and exits
// with status 2 without running the exit hook.
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
	switch sig {
	case sigSEGV, sigBUS:
		if h := faultHandler; h != nil && h(addr, pc, errCode&pfWrite != 0) {
			break // h printed the message
		}
		if addr < sp+4096 && addr+65536 > sp || addr >= guardLo && addr < guardHi {
			// Just below the stack pointer: the stack ran into its guard.
			printString("panic: runtime error: stack overflow\n")
			break
		}
		if addr < 4096 {
			printString("panic: runtime error: invalid memory address or nil pointer dereference\n")
		} else {
			printString("panic: runtime error: unexpected fault address ")
			printPointer(unsafe.Pointer(addr))
			printNewline()
		}
	case sigFPE:
		printString("panic: runtime error: integer divide error\n")
	default:
		printString("panic: runtime error: illegal instruction\n")
	}
	printString("[signal ")
	switch sig {
	case sigSEGV:
		printString("SIGSEGV: segmentation violation")
	case sigBUS:
		printString("SIGBUS: bus error")
	case sigFPE:
		printString("SIGFPE: floating-point exception")
	default:
		printString("SIGILL: illegal instruction")
	}
	printString(" addr=")
	printPointer(unsafe.Pointer(addr))
	printString(" pc=")
	printPointer(unsafe.Pointer(pc))
	printString("]\n")
	if !tracing {
		Traceback(pc, sp, bp)
	}
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

// abort ends the process after a panic.
//
//pu:noreturn
func abort() { exit(2) }

func printBytes(p unsafe.Pointer, n int) {
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
// the instruction at pc, a write if write is set. If it recognises addr it
// prints a description of the fault, starting with "panic: ", and returns
// true; otherwise it prints nothing and returns false. The program stops
// either way.
type FaultHandler func(addr, pc uintptr, write bool) bool

var faultHandler FaultHandler

// SetFaultHandler makes h the first to hear of memory access faults
// (SIGSEGV and SIGBUS). The runtime's fault handler, installed at start,
// calls h before anything else; if h does not recognise the fault the
// runtime prints its own message. The signal and the stack trace follow,
// and the program exits with status 2, as a panic does.
func SetFaultHandler(h FaultHandler) { faultHandler = h }
