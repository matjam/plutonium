// Package os provides files, arguments and process control for hosted
// programs.
//
// Errors follow pluto/errors. Opening a file reports a *PathError that
// records the operation and the path; it comes from the same allocator as
// the File (the default allocator, or the one given to OpenFile). Reads,
// writes and Close report the syscall.Errno alone, so that the I/O path
// never allocates; Read reports io.EOF at the end of input.
package os

import (
	"pluto/alloc"
	"pluto/internal/oserror"
	"pluto/io"
	"pluto/rt"
	"pluto/syscall"
	"unsafe"
)

// Portable errors. A syscall.Errno matches them with errors.Is, as does any
// error that wraps one: errors.Is(err, os.ErrNotExist) is true for ENOENT.
var (
	ErrInvalid    = oserror.ErrInvalid    // invalid argument
	ErrPermission = oserror.ErrPermission // permission denied
	ErrExist      = oserror.ErrExist      // file already exists
	ErrNotExist   = oserror.ErrNotExist   // file does not exist
	ErrClosed     = oserror.ErrClosed     // file already closed
)

// A File is an open file descriptor.
//
// A read or write that has to wait does not hold up other puroutines: on a
// descriptor in non-blocking mode (a pipe from Pipe) the puroutine parks
// in the scheduler's poller until the descriptor is ready, and on any
// other the system call is announced to the scheduler (rt.EnterSyscall),
// which hands the worker's processor to another worker if the call blocks.
type File struct {
	fd   int
	name string
	// poll waits for the descriptor when it is in non-blocking mode (a
	// pipe from Pipe), or is nil. It is an interface that only Pipe fills
	// in, so that in a program that never makes a pipe, File's methods do
	// not reach the scheduler's parking (the interrupt-context check
	// follows interface calls into the types the program converts).
	poll ioWaiter
}

// An ioWaiter parks the running puroutine until a non-blocking descriptor
// is ready for reading or writing.
type ioWaiter interface {
	waitIO(fd int, write bool)
}

// poller waits in the scheduler's epoll poller.
type poller struct{}

func (poller) waitIO(fd int, write bool) {
	if !rt.PollWait(fd, write) {
		rt.Yield() // no poller (a program's own scheduler): try again later
	}
}

// Standard streams.
var (
	Stdin  = &File{fd: 0, name: "/dev/stdin"}
	Stdout = &File{fd: 1, name: "/dev/stdout"}
	Stderr = &File{fd: 2, name: "/dev/stderr"}
)

// Fd returns the file descriptor.
func (f *File) Fd() int { return f.fd }

// Name returns the name the file was opened with.
func (f *File) Name() string { return f.name }

// Write writes p, retrying until all of it is written or an error occurs.
func (f *File) Write(p []byte) (int, error) {
	n := 0
	for n < len(p) {
		if f.poll == nil {
			rt.EnterSyscall()
		}
		m, err := syscall.Write(f.fd, p[n:])
		if f.poll == nil {
			rt.ExitSyscall()
		}
		if err != nil {
			if err == syscall.EINTR {
				continue
			}
			if err == syscall.EAGAIN && f.poll != nil {
				f.poll.waitIO(f.fd, true)
				continue
			}
			return n, err
		}
		n += m
	}
	return n, nil
}

// WriteString writes s.
func (f *File) WriteString(s string) (int, error) {
	if len(s) == 0 {
		return 0, nil
	}
	return f.Write(unsafe.Slice(unsafe.StringData(s), len(s)))
}

// Read reads up to len(p) bytes.
func (f *File) Read(p []byte) (int, error) {
	for {
		if f.poll == nil {
			rt.EnterSyscall()
		}
		n, err := syscall.Read(f.fd, p)
		if f.poll == nil {
			rt.ExitSyscall()
		}
		if err == syscall.EINTR {
			continue
		}
		if err == syscall.EAGAIN && f.poll != nil {
			f.poll.waitIO(f.fd, false)
			continue
		}
		if err == nil && n == 0 && len(p) > 0 {
			return 0, io.EOF
		}
		return n, err
	}
}

// Close closes the file.
func (f *File) Close() error { return syscall.Close(f.fd) }

// Pipe returns a connected pair of Files, reading from r and writing to w,
// allocated from a (by default the default allocator); free them with
// alloc.Free after Close. Their descriptors are non-blocking: a puroutine
// that reads an empty pipe, or writes a full one, parks in the scheduler's
// poller and leaves its worker free for others.
func Pipe(a alloc.Allocator = alloc.Default()) (r, w *File, err error) {
	var fds [2]int32
	const oNonblock, oCloexec = 0x800, 0x80000
	if _, err := syscallResult(syscall.Syscall(syscall.SYS_PIPE2, uintptr(unsafe.Pointer(&fds[0])), oNonblock|oCloexec, 0)); err != nil {
		return nil, nil, err
	}
	r = alloc.New[File](a)
	r.fd, r.name, r.poll = int(fds[0]), "|0", poller{}
	w = alloc.New[File](a)
	w.fd, w.name, w.poll = int(fds[1]), "|1", poller{}
	return r, w, nil
}

// syscallResult splits a raw system call result into a value and an
// error.
func syscallResult(r uintptr) (int, error) {
	if r > ^uintptr(4095) {
		return -1, syscall.Errno(-r)
	}
	return int(r), nil
}

// IsEOF reports whether err is io.EOF.
func IsEOF(err error) bool { return err == io.EOF }

// Exit ends the process with the given status. Deferred calls do not run.
//
//pu:noreturn
func Exit(code int) { rt.Exit(code) }

// NArgs returns the number of command-line arguments, including the
// program name.
func NArgs() int {
	n, _ := rt.Args()
	return n
}

// Arg returns command-line argument i (Arg(0) is the program name).
func Arg(i int) string {
	n, argv := rt.Args()
	if i < 0 || i >= n {
		return ""
	}
	p := *(**byte)(unsafe.Add(argv, i*8))
	return cstring(p)
}

// Getenv returns the value of the environment variable key, or "".
func Getenv(key string) string {
	env := rt.Environ()
	for i := 0; ; i++ {
		p := *(**byte)(unsafe.Add(env, i*8))
		if p == nil {
			return ""
		}
		kv := cstring(p)
		if len(kv) > len(key) && kv[len(key)] == '=' && kv[:len(key)] == key {
			return kv[len(key)+1:]
		}
	}
}

// cstring views a NUL-terminated C string.
func cstring(p *byte) string {
	n := 0
	for *(*byte)(unsafe.Add(unsafe.Pointer(p), n)) != 0 {
		n++
	}
	return unsafe.String(p, n)
}

// Open opens a file for reading, allocating as OpenFile does.
func Open(name string, a alloc.Allocator = alloc.Default()) (*File, error) {
	return OpenFile(name, syscall.O_RDONLY, 0, a)
}

// Create creates or truncates a file for writing, allocating as OpenFile
// does.
func Create(name string, a alloc.Allocator = alloc.Default()) (*File, error) {
	return OpenFile(name, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_TRUNC, 0o644, a)
}

// OpenFile opens a file with the given flags and permissions. The File, or
// on failure the *PathError, is allocated from a, by default the default
// allocator; free the File with alloc.Free after Close, and the error with
// errors.Free.
func OpenFile(name string, flags int, perm uint32, a alloc.Allocator = alloc.Default()) (*File, error) {
	var path [4096]byte
	if len(name) >= len(path) {
		return nil, NewPathError("open", name, syscall.ENAMETOOLONG, a)
	}
	copy(path[:], name)
	fd, err := syscall.Open(path[:len(name)+1], flags, perm)
	if err != nil {
		return nil, NewPathError("open", name, err, a)
	}
	f := alloc.New[File](a)
	f.fd = fd
	f.name = name
	return f, nil
}

// PathError records an error and the operation and file path that caused
// it.
//
// Its message, "op path: err", is formatted once, when the error is made,
// into the same block of memory as the PathError, and Op and Path view that
// copy; so the error stays valid however long the caller's path string
// lives. Make one with NewPathError, and release it with
// errors.Free (or by resetting the arena it came from).
type PathError struct {
	Op   string
	Path string
	Err  error

	msg  string
	a    alloc.Allocator // allocator that owns the PathError, or nil
	size uintptr
}

// Error returns "op path: err". A PathError not made by NewPathError has no
// stored message and reports Err's message.
func (e *PathError) Error() string {
	if e.msg != "" {
		return e.msg
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Op
}

// Unwrap returns the underlying error.
func (e *PathError) Unwrap() error { return e.Err }

// Timeout reports whether this error represents a timeout.
func (e *PathError) Timeout() bool {
	t, ok := e.Err.(interface{ Timeout() bool })
	return ok && t.Timeout()
}

// Free returns the PathError's memory to the allocator it came from. It
// does not free Err; errors.Free frees the whole chain.
func (e *PathError) Free() {
	if e.a == nil {
		return
	}
	var hdr PathError
	e.a.Free(unsafe.Pointer(e), e.size, unsafe.Alignof(hdr))
}

// NewPathError returns a PathError for op on path failing with err, with
// its message, "op path: err", allocated in one block from a, by default
// the default allocator.
func NewPathError(op, path string, err error, a alloc.Allocator = alloc.Default()) *PathError {
	var hdr PathError
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	n := len(op) + 1 + len(path) + 2 + len(errMsg)
	off := unsafe.Sizeof(hdr)
	total := off + uintptr(n)
	p := alloc.Raw(total, unsafe.Alignof(hdr), a)
	text := unsafe.Slice((*byte)(unsafe.Add(p, off)), n)
	k := copy(text, op)
	text[k] = ' '
	k++
	k += copy(text[k:], path)
	text[k], text[k+1] = ':', ' '
	copy(text[k+2:], errMsg)
	msg := unsafe.String(unsafe.SliceData(text), n)
	e := (*PathError)(p)
	e.Op = msg[:len(op)]
	e.Path = msg[len(op)+1 : len(op)+1+len(path)]
	e.Err = err
	e.msg = msg
	e.a = a
	e.size = total
	return e
}
