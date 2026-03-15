//pu:build linux && amd64

package os

import (
	"pluto/syscall"
	"unsafe"
)

// Open opens a file.
// name must be a null-terminated byte slice (the caller is responsible for this).
// To open "foo.txt": var name = [8]byte{'f','o','o','.','t','x','t', 0}
func Open(name []byte, flags int, perm uint32) (int, error) {
	fd, _, errno := syscall.Syscall(
		syscall.SYS_OPEN,
		uintptr(unsafe.Pointer(&name[0])),
		uintptr(flags),
		uintptr(perm),
	)
	if errno != 0 {
		return -1, syscall.Errno(errno)
	}
	return int(fd), nil
}
