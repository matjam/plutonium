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

// start is called by _start with the initial stack pointer, which points at
// argc, then the argv pointers, a nil, then the environment pointers.
//
//pu:export
//pu:noreturn
func start(sp unsafe.Pointer) {
	argc = *(*int)(sp)
	argv = unsafe.Add(sp, 8)
	envp = unsafe.Add(sp, 8*(argc+2))
	Exit(int(programMain()))
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
// true. The program stops either way.
type FaultHandler func(addr, pc uintptr, write bool) bool

var (
	faultHandler   FaultHandler
	faultInstalled bool
)

// SetFaultHandler makes h the first to hear of memory access faults
// (SIGSEGV). Without a handler a fault kills the process with the signal;
// with one, the program prints a message and exits with status 2, as a
// panic does.
func SetFaultHandler(h FaultHandler) {
	faultHandler = h
	if faultInstalled {
		return
	}
	faultInstalled = true
	const (
		sigsegv     = 11
		saSiginfo   = 0x4
		saRestorer  = 0x04000000
		sigsetBytes = 8
	)
	var sa sigaction
	sa.handler = sigtrampAddr()
	sa.flags = saSiginfo | saRestorer
	sa.restorer = sigreturnAddr()
	syscall6(sysRtSigaction, sigsegv, uintptr(unsafe.Pointer(&sa)), 0, sigsetBytes, 0, 0)
}

// sigaction is the kernel's struct sigaction on x86-64.
type sigaction struct {
	handler  uintptr
	flags    uintptr
	restorer uintptr
	mask     uint64
}

// sigtrampAddr and sigreturnAddr return the addresses of the signal entry
// and return stubs (sys_linux_amd64.s).
func sigtrampAddr() uintptr
func sigreturnAddr() uintptr

// sigfault handles SIGSEGV. The signal stub calls it with the signal
// number, the siginfo and the ucontext.
//
//pu:export
func sigfault(sig int, info, ctx unsafe.Pointer) {
	const (
		siAddr   = 16  // siginfo_t.si_addr
		ucGregs  = 40  // ucontext_t.uc_mcontext.gregs
		regRIP   = 16  // REG_RIP
		regErr   = 19  // REG_ERR: the page fault error code
		pfWrite  = 0x2 // error code bit: the access was a write
		wordSize = uintptr(8)
	)
	addr := *(*uintptr)(unsafe.Add(info, siAddr))
	pc := *(*uintptr)(unsafe.Add(ctx, ucGregs+regRIP*wordSize))
	errCode := *(*uintptr)(unsafe.Add(ctx, ucGregs+regErr*wordSize))
	if faultHandler != nil && faultHandler(addr, pc, errCode&pfWrite != 0) {
		abort()
	}
	printString("panic: runtime error: invalid memory access at ")
	printPointer(unsafe.Pointer(addr))
	printString(" (pc ")
	PrintPC(pc)
	printString(")\n")
	abort()
}
