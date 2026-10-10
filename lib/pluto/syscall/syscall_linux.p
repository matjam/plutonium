// Package syscall provides raw Linux system calls.
package syscall

import "unsafe"

// Syscall issues system call num with three arguments and returns the raw
// result: a negative value in (-4096, 0) is an errno.
func Syscall(num, a1, a2, a3 uintptr) uintptr

// Syscall6 issues system call num with six arguments.
func Syscall6(num, a1, a2, a3, a4, a5, a6 uintptr) uintptr

// System call numbers (x86-64).
const (
	SYS_READ       = 0
	SYS_WRITE      = 1
	SYS_OPEN       = 2
	SYS_CLOSE      = 3
	SYS_STAT       = 4
	SYS_FSTAT      = 5
	SYS_LSEEK      = 8
	SYS_MMAP       = 9
	SYS_MPROTECT   = 10
	SYS_MUNMAP     = 11
	SYS_BRK        = 12
	SYS_IOCTL      = 16
	SYS_PREAD64    = 17
	SYS_PWRITE64   = 18
	SYS_GETPID     = 39
	SYS_EXIT       = 60
	SYS_KILL       = 62
	SYS_UNAME      = 63
	SYS_FCNTL      = 72
	SYS_GETCWD     = 79
	SYS_CHDIR      = 80
	SYS_RENAME     = 82
	SYS_MKDIR      = 83
	SYS_RMDIR      = 84
	SYS_UNLINK     = 87
	SYS_GETTIMEOFDAY = 96
	SYS_CLOCK_GETTIME = 228
	SYS_EXIT_GROUP = 231
	SYS_OPENAT     = 257
	SYS_GETDENTS64 = 217
)

// Open flags.
const (
	O_RDONLY = 0x0
	O_WRONLY = 0x1
	O_RDWR   = 0x2
	O_CREAT  = 0x40
	O_EXCL   = 0x80
	O_TRUNC  = 0x200
	O_APPEND = 0x400
	AT_FDCWD = -100
)

// result splits a raw system call result into a value and an error. The
// error is an Errno, which fits in the interface's data word.
func result(r uintptr) (int, error) {
	if r > ^uintptr(4095) {
		return -1, Errno(-r)
	}
	return int(r), nil
}

// Read reads up to len(p) bytes from fd.
func Read(fd int, p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return result(Syscall(SYS_READ, uintptr(fd), uintptr(unsafe.Pointer(&p[0])), uintptr(len(p))))
}

// Write writes p to fd.
func Write(fd int, p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return result(Syscall(SYS_WRITE, uintptr(fd), uintptr(unsafe.Pointer(&p[0])), uintptr(len(p))))
}

// Open opens the file at path, which must be NUL-terminated.
func Open(path []byte, flags int, mode uint32) (int, error) {
	return result(Syscall6(SYS_OPENAT, ^uintptr(99), uintptr(unsafe.Pointer(&path[0])), uintptr(flags), uintptr(mode), 0, 0))
}

// Close closes fd.
func Close(fd int) error {
	_, err := result(Syscall(SYS_CLOSE, uintptr(fd), 0, 0))
	return err
}

// Exit ends the process.
//
//pu:noreturn
func Exit(code int) {
	Syscall(SYS_EXIT_GROUP, uintptr(code), 0, 0)
	for {
	}
}
