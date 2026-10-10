package syscall

import "unsafe"

// System calls for files, directories, processes and time, used by pluto/os,
// pluto/os/exec and pluto/time. Paths are NUL-terminated byte slices, as
// for Open.

// More system call numbers (x86-64).
const (
	SYS_POLL       = 7
	SYS_DUP2       = 33
	SYS_NANOSLEEP  = 35
	SYS_CLONE      = 56
	SYS_FORK       = 57
	SYS_EXECVE     = 59
	SYS_WAIT4      = 61
	SYS_FSYNC      = 74
	SYS_FTRUNCATE  = 77
	SYS_GETUID     = 102
	SYS_MKDIRAT    = 258
	SYS_NEWFSTATAT = 262
	SYS_UNLINKAT   = 263
	SYS_RENAMEAT   = 264
	SYS_FCHMODAT   = 268
	SYS_FACCESSAT  = 269
	SYS_DUP3       = 292
	SYS_PIPE2      = 293
	SYS_GETRANDOM  = 318
)

// Flags and modes.
const (
	O_NONBLOCK  = 0x800
	O_DIRECTORY = 0x10000
	O_NOFOLLOW  = 0x20000
	O_CLOEXEC   = 0x80000

	AT_SYMLINK_NOFOLLOW = 0x100
	AT_REMOVEDIR        = 0x200

	F_GETFD    = 1
	F_SETFD    = 2
	F_GETFL    = 3
	F_SETFL    = 4
	FD_CLOEXEC = 1

	SEEK_SET = 0
	SEEK_CUR = 1
	SEEK_END = 2

	X_OK = 1
	W_OK = 2
	R_OK = 4

	S_IFMT   = 0o170000
	S_IFSOCK = 0o140000
	S_IFLNK  = 0o120000
	S_IFREG  = 0o100000
	S_IFBLK  = 0o060000
	S_IFDIR  = 0o040000
	S_IFCHR  = 0o020000
	S_IFIFO  = 0o010000
	S_ISUID  = 0o4000
	S_ISGID  = 0o2000
	S_ISVTX  = 0o1000

	// Directory entry types in Dirent64.Type.
	DT_UNKNOWN = 0
	DT_FIFO    = 1
	DT_CHR     = 2
	DT_DIR     = 4
	DT_BLK     = 6
	DT_REG     = 8
	DT_LNK     = 10
	DT_SOCK    = 12

	SIGHUP  = 1
	SIGINT  = 2
	SIGQUIT = 3
	SIGKILL = 9
	SIGPIPE = 13
	SIGTERM = 15
	SIGCHLD = 17

	WNOHANG = 1

	POLLIN   = 0x1
	POLLOUT  = 0x4
	POLLERR  = 0x8
	POLLHUP  = 0x10
	POLLNVAL = 0x20
)

// A Timespec is a time in seconds and nanoseconds.
type Timespec struct {
	Sec  int64
	Nsec int64
}

// Stat_t is struct stat (x86-64).
type Stat_t struct {
	Dev     uint64
	Ino     uint64
	Nlink   uint64
	Mode    uint32
	Uid     uint32
	Gid     uint32
	_       int32
	Rdev    uint64
	Size    int64
	Blksize int64
	Blocks  int64
	Atim    Timespec
	Mtim    Timespec
	Ctim    Timespec
	_       [3]int64
}

// A PollFd is struct pollfd.
type PollFd struct {
	Fd      int32
	Events  int16
	Revents int16
}

func ptr(b []byte) uintptr { return uintptr(unsafe.Pointer(unsafe.SliceData(b))) }

// errOnly turns a raw result into an error.
func errOnly(r uintptr) error {
	_, err := result(r)
	return err
}

// Openat opens path relative to dirfd (AT_FDCWD for the working
// directory).
func Openat(dirfd int, path []byte, flags int, mode uint32) (int, error) {
	return result(Syscall6(SYS_OPENAT, uintptr(dirfd), ptr(path), uintptr(flags), uintptr(mode), 0, 0))
}

// Getrandom fills buf with random bytes from the kernel and returns how
// many it wrote.
func Getrandom(buf []byte, flags int) (int, error) {
	return result(Syscall(SYS_GETRANDOM, ptr(buf), uintptr(len(buf)), uintptr(flags)))
}

// Fstatat stats path relative to dirfd (AT_FDCWD for the working
// directory); flags may be AT_SYMLINK_NOFOLLOW.
func Fstatat(dirfd int, path []byte, st *Stat_t, flags int) error {
	return errOnly(Syscall6(SYS_NEWFSTATAT, uintptr(dirfd), ptr(path), uintptr(unsafe.Pointer(st)), uintptr(flags), 0, 0))
}

// Fstat stats an open file.
func Fstat(fd int, st *Stat_t) error {
	return errOnly(Syscall(SYS_FSTAT, uintptr(fd), uintptr(unsafe.Pointer(st)), 0))
}

// Getdents64 reads directory entries from fd into buf and returns the
// number of bytes read, 0 at the end of the directory. ParseDirent walks
// them.
func Getdents64(fd int, buf []byte) (int, error) {
	return result(Syscall(SYS_GETDENTS64, uintptr(fd), ptr(buf), uintptr(len(buf))))
}

// ParseDirent returns the first entry of a buffer filled by Getdents64: its
// name (a view of buf), its type (DT_*), and the length of the record.
func ParseDirent(buf []byte) (name string, typ uint8, reclen int) {
	// struct linux_dirent64: ino u64, off i64, reclen u16, type u8, name.
	reclen = int(buf[16]) | int(buf[17])<<8
	typ = buf[18]
	n := 0
	for 19+n < reclen && buf[19+n] != 0 {
		n++
	}
	return unsafe.String(&buf[19], n), typ, reclen
}

// Mkdirat creates a directory.
func Mkdirat(dirfd int, path []byte, mode uint32) error {
	return errOnly(Syscall(SYS_MKDIRAT, uintptr(dirfd), ptr(path), uintptr(mode)))
}

// Unlinkat removes a file, or with AT_REMOVEDIR an empty directory.
func Unlinkat(dirfd int, path []byte, flags int) error {
	return errOnly(Syscall(SYS_UNLINKAT, uintptr(dirfd), ptr(path), uintptr(flags)))
}

// Renameat renames oldpath to newpath.
func Renameat(olddirfd int, oldpath []byte, newdirfd int, newpath []byte) error {
	return errOnly(Syscall6(SYS_RENAMEAT, uintptr(olddirfd), ptr(oldpath), uintptr(newdirfd), ptr(newpath), 0, 0))
}

// Fchmodat changes a file's permission bits.
func Fchmodat(dirfd int, path []byte, mode uint32) error {
	return errOnly(Syscall(SYS_FCHMODAT, uintptr(dirfd), ptr(path), uintptr(mode)))
}

// Faccessat checks whether the calling process may access path with mode
// (X_OK, W_OK, R_OK).
func Faccessat(dirfd int, path []byte, mode uint32) error {
	return errOnly(Syscall(SYS_FACCESSAT, uintptr(dirfd), ptr(path), uintptr(mode)))
}

// Getcwd stores the working directory, NUL-terminated, in buf and returns
// its length including the NUL.
func Getcwd(buf []byte) (int, error) {
	return result(Syscall(SYS_GETCWD, ptr(buf), uintptr(len(buf)), 0))
}

// Chdir changes the working directory.
func Chdir(path []byte) error { return errOnly(Syscall(SYS_CHDIR, ptr(path), 0, 0)) }

// Seek sets the offset of fd and returns the new offset.
func Seek(fd int, offset int64, whence int) (int64, error) {
	n, err := result(Syscall(SYS_LSEEK, uintptr(fd), uintptr(offset), uintptr(whence)))
	return int64(n), err
}

// Fsync flushes fd's data to its device.
func Fsync(fd int) error { return errOnly(Syscall(SYS_FSYNC, uintptr(fd), 0, 0)) }

// Ftruncate sets the size of fd's file.
func Ftruncate(fd int, size int64) error {
	return errOnly(Syscall(SYS_FTRUNCATE, uintptr(fd), uintptr(size), 0))
}

// Pipe2 creates a pipe: p[0] is the read end, p[1] the write end.
func Pipe2(p *[2]int32, flags int) error {
	return errOnly(Syscall(SYS_PIPE2, uintptr(unsafe.Pointer(p)), uintptr(flags), 0))
}

// Dup3 duplicates oldfd onto newfd; flags may be O_CLOEXEC.
func Dup3(oldfd, newfd, flags int) error {
	return errOnly(Syscall(SYS_DUP3, uintptr(oldfd), uintptr(newfd), uintptr(flags)))
}

// Fcntl performs the file control operation cmd.
func Fcntl(fd, cmd, arg int) (int, error) {
	return result(Syscall(SYS_FCNTL, uintptr(fd), uintptr(cmd), uintptr(arg)))
}

// Fork creates a child process that is a copy of the caller: it returns
// the child's process ID in the parent and 0 in the child. The child has
// one thread, the one that called Fork.
func Fork() (int, error) {
	return result(Syscall6(SYS_CLONE, SIGCHLD, 0, 0, 0, 0, 0))
}

// Execve replaces the process image with the program at path. argv and
// envp are nil-terminated arrays of NUL-terminated strings. It returns only
// on failure.
func Execve(path []byte, argv, envp unsafe.Pointer) error {
	return errOnly(Syscall(SYS_EXECVE, ptr(path), uintptr(argv), uintptr(envp)))
}

// Wait4 waits for a child process to change state and stores its wait
// status in status.
func Wait4(pid int, status *int32, options int) (int, error) {
	return result(Syscall6(SYS_WAIT4, uintptr(pid), uintptr(unsafe.Pointer(status)), uintptr(options), 0, 0, 0))
}

// Kill sends signal sig to process pid.
func Kill(pid, sig int) error { return errOnly(Syscall(SYS_KILL, uintptr(pid), uintptr(sig), 0)) }

// Getpid returns the process ID.
func Getpid() int {
	n, _ := result(Syscall(SYS_GETPID, 0, 0, 0))
	return n
}

// Getuid returns the user ID.
func Getuid() int {
	n, _ := result(Syscall(SYS_GETUID, 0, 0, 0))
	return n
}

// Poll waits for events on fds, at most timeout milliseconds (-1: no
// limit), and returns the number of descriptors with events.
func Poll(fds []PollFd, timeout int) (int, error) {
	return result(Syscall(SYS_POLL, uintptr(unsafe.Pointer(unsafe.SliceData(fds))), uintptr(len(fds)), uintptr(timeout)))
}

// WaitStatus is a process's wait status, as Wait4 reports it.
type WaitStatus uint32

// Exited reports whether the process exited normally.
func (w WaitStatus) Exited() bool { return w&0x7f == 0 }

// ExitStatus returns the exit status, or -1 if the process did not exit
// normally.
func (w WaitStatus) ExitStatus() int {
	if !w.Exited() {
		return -1
	}
	return int(w>>8) & 0xff
}

// Signaled reports whether a signal ended the process.
func (w WaitStatus) Signaled() bool { return w&0x7f != 0x7f && w&0x7f != 0 }

// Signal returns the signal that ended the process, or -1.
func (w WaitStatus) Signal() int {
	if !w.Signaled() {
		return -1
	}
	return int(w & 0x7f)
}
