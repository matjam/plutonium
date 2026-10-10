//pu:build linux && arm64

package syscall

// Linux aarch64 syscall numbers.
// Reference: https://syscalls.mebeim.net/?table=arm64/64/aarch64/v6.7
const (
	SYS_READ         uintptr = 63
	SYS_WRITE        uintptr = 64
	SYS_CLOSE        uintptr = 57
	SYS_FSTAT        uintptr = 80
	SYS_LSEEK        uintptr = 62
	SYS_MMAP         uintptr = 222
	SYS_MPROTECT     uintptr = 226
	SYS_MUNMAP       uintptr = 215
	SYS_BRK          uintptr = 214
	SYS_IOCTL        uintptr = 29
	SYS_PIPE2        uintptr = 59
	SYS_DUP          uintptr = 23
	SYS_DUP3         uintptr = 24
	SYS_NANOSLEEP    uintptr = 101
	SYS_GETPID       uintptr = 172
	SYS_SOCKET       uintptr = 198
	SYS_CONNECT      uintptr = 203
	SYS_ACCEPT       uintptr = 202
	SYS_SENDTO       uintptr = 206
	SYS_RECVFROM     uintptr = 207
	SYS_BIND         uintptr = 200
	SYS_LISTEN       uintptr = 201
	SYS_EXECVE       uintptr = 221
	SYS_EXIT         uintptr = 93
	SYS_WAIT4        uintptr = 260
	SYS_KILL         uintptr = 129
	SYS_GETDENTS64   uintptr = 61
	SYS_EXIT_GROUP   uintptr = 94
	SYS_OPENAT       uintptr = 56
	SYS_NEWFSTATAT   uintptr = 79
	SYS_UNLINKAT     uintptr = 35
	SYS_MKDIRAT      uintptr = 34
)

// Note: aarch64 Linux does not have open(2) or stat(2) — use openat/newfstatat
// with AT_FDCWD (-100) as the dirfd argument.
const AT_FDCWD uintptr = 0xFFFFFFFFFFFFFF9C // -100 as uintptr
