//pu:build linux

package rt

import "unsafe"

// syscall3 and syscall6 issue Linux system calls (sys_linux_amd64.s).
func syscall3(num, a1, a2, a3 uintptr) uintptr
func syscall6(num, a1, a2, a3, a4, a5, a6 uintptr) uintptr

const (
	sysWrite     = 1
	sysMmap      = 9
	sysMunmap    = 11
	sysExitGroup = 231
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
//pu:noreturn
func start(sp unsafe.Pointer) {
	argc = *(*int)(sp)
	argv = unsafe.Add(sp, 8)
	envp = unsafe.Add(sp, 8*(argc+2))
	exit(int(programMain()))
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

// Exit ends the process with the given status.
//
//pu:noreturn
func Exit(code int) { exit(code) }

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
