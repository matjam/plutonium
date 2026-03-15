//pu:build linux

package os

import (
	"pluto/syscall"
	"unsafe"
)

// Linux open(2) flags.
const (
	O_RDONLY = 0
	O_WRONLY = 1
	O_RDWR   = 2
	O_CREAT  = 0x40
	O_TRUNC  = 0x200
	O_APPEND = 0x400
	O_CLOEXEC = 0x80000
)

// Write writes p to file descriptor fd.
func Write(fd int, p []byte) (int, error) {
	n, _, errno := syscall.Syscall(
		syscall.SYS_WRITE,
		uintptr(fd),
		uintptr(unsafe.Pointer(&p[0])),
		uintptr(len(p)),
	)
	if errno != 0 {
		return int(n), syscall.Errno(errno)
	}
	return int(n), nil
}

// Read reads from fd into p.
func Read(fd int, p []byte) (int, error) {
	n, _, errno := syscall.Syscall(
		syscall.SYS_READ,
		uintptr(fd),
		uintptr(unsafe.Pointer(&p[0])),
		uintptr(len(p)),
	)
	if errno != 0 {
		return int(n), syscall.Errno(errno)
	}
	return int(n), nil
}

// Close closes a file descriptor.
func Close(fd int) error {
	_, _, errno := syscall.Syscall(syscall.SYS_CLOSE, uintptr(fd), 0, 0)
	if errno != 0 {
		return syscall.Errno(errno)
	}
	return nil
}

// Exit terminates the process with the given exit code.
func Exit(code int) {
	syscall.Syscall(syscall.SYS_EXIT_GROUP, uintptr(code), 0, 0)
}
