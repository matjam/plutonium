//pu:build linux && arm64

package os

import (
	"pluto/syscall"
	"unsafe"
)

// Open opens a file.
// name must be a null-terminated byte slice.
func Open(name []byte, flags int, perm uint32) (int, error) {
	fd, _, errno := syscall.Syscall6(
		syscall.SYS_OPENAT,
		syscall.AT_FDCWD,
		uintptr(unsafe.Pointer(&name[0])),
		uintptr(flags),
		uintptr(perm),
		0,
		0,
	)
	if errno != 0 {
		return -1, syscall.Errno(errno)
	}
	return int(fd), nil
}
