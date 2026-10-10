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
	"pluto/sync/atomic"
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
//
// A read and a write may wait on the same descriptor at once (a socket),
// and so may any number of reads. Close wakes the puroutines waiting in the
// poller: their Read or Write reports ErrClosed. A read or write that is
// running on a descriptor in blocking mode cannot be woken; the descriptor
// is closed when it returns. Once closed, every operation reports ErrClosed
// (Close itself too, the second time).
//
// As in Go, a write to a pipe with no reader reports syscall.EPIPE, except
// on descriptors 1 and 2, where it ends the program with SIGPIPE.
type File struct {
	fd   int
	name string
	// fdmu holds the closed bit (fdClosed) and the number of operations
	// in progress, so that the descriptor is closed (and may be reused by
	// the system) only when the last of them has finished.
	fdmu uint32
	// poll is the poller's record of the descriptor if it has one (a pipe
	// from Pipe, or a non-blocking descriptor given to NewFile), or nil.
	// It is an interface that only those fill in, so that in a program
	// that never makes a pipe, File's methods do not reach the
	// scheduler's parking (the interrupt-context check follows interface
	// calls into the types the program converts).
	poll ioWaiter
	// nonblock is set while the descriptor is in non-blocking mode and
	// reads and writes wait in the poller; Fd clears it.
	nonblock bool
}

const fdClosed = 1 << 31

// An ioWaiter is the poller's record of a non-blocking descriptor
// (*rt.PollDesc).
type ioWaiter interface {
	// Wait parks the running puroutine until the descriptor may be ready
	// for reading or writing: rt.PollReady, rt.PollClosed after Evict, or
	// rt.PollUnavailable if there is no poller.
	Wait(write bool) int
	// Evict wakes the waiters and makes later waits fail.
	Evict()
	// Release deregisters the descriptor.
	Release()
}

// waitIO parks until the descriptor may be ready and reports whether the
// operation should be tried again (false: the file was closed).
func (f *File) waitIO(write bool) bool {
	switch f.poll.Wait(write) {
	case rt.PollClosed:
		return false
	case rt.PollUnavailable:
		rt.Yield() // no poller (a program's own scheduler): try again later
	}
	return true
}

// incref registers an operation in progress, and reports false if the file
// is closed.
func (f *File) incref() bool {
	for {
		v := atomic.LoadUint32(&f.fdmu)
		if v&fdClosed != 0 {
			return false
		}
		if atomic.CompareAndSwapUint32(&f.fdmu, v, v+1) {
			return true
		}
	}
}

// decref ends an operation; the one that finds the file closed and nothing
// else in progress closes the descriptor.
func (f *File) decref() error {
	if atomic.AddUint32(&f.fdmu, ^uint32(0)) == fdClosed {
		return f.destroy()
	}
	return nil
}

// destroy closes the descriptor, after the last operation on it.
func (f *File) destroy() error {
	if f.poll != nil {
		f.poll.Release()
	}
	return syscall.Close(f.fd)
}

// Standard streams.
var (
	Stdin  = &File{fd: 0, name: "/dev/stdin"}
	Stdout = &File{fd: 1, name: "/dev/stdout"}
	Stderr = &File{fd: 2, name: "/dev/stderr"}
)

// Fd returns the file descriptor. As in Go, it puts a descriptor in
// non-blocking mode (a pipe from Pipe) into blocking mode first, so that
// it can be handed to another process (os/exec passes it to the child as
// it is); the File's reads and writes then block their worker, announced
// with rt.EnterSyscall, instead of parking in the poller.
func (f *File) Fd() int {
	if f.nonblock {
		if fl, err := syscall.Fcntl(f.fd, syscall.F_GETFL, 0); err == nil {
			syscall.Fcntl(f.fd, syscall.F_SETFL, fl&^syscall.O_NONBLOCK)
		}
		f.nonblock = false
	}
	return f.fd
}

// NewFile returns a File for the descriptor fd, named name and allocated
// from a, which the File owns from now on: Close closes it. A descriptor
// in non-blocking mode (an end of a pipe or socket made with
// O_NONBLOCK, SOCK_NONBLOCK or Pipe2) is waited for in the poller, as for
// Pipe; any other descriptor is used with blocking system calls.
func NewFile(fd int, name string, a alloc.Allocator = alloc.Default()) *File {
	f := alloc.New[File](a)
	f.init(fd, name)
	return f
}

// init sets up a File for fd, which is waited for in the poller if it is in
// non-blocking mode.
func (f *File) init(fd int, name string) {
	f.fd = fd
	f.name = name
	if fl, err := syscall.Fcntl(fd, syscall.F_GETFL, 0); err == nil && fl&syscall.O_NONBLOCK != 0 {
		f.poll = rt.PollOpen(fd)
		f.nonblock = true
	}
}

// Name returns the name the file was opened with.
func (f *File) Name() string { return f.name }

// Write writes p, retrying until all of it is written or an error occurs.
func (f *File) Write(p []byte) (int, error) {
	if !f.incref() {
		return 0, ErrClosed
	}
	n, err := f.write(p)
	f.decref()
	return n, err
}

func (f *File) write(p []byte) (int, error) {
	n := 0
	for n < len(p) {
		if !f.nonblock {
			rt.EnterSyscall()
		}
		m, err := syscall.Write(f.fd, p[n:])
		if !f.nonblock {
			rt.ExitSyscall()
		}
		if err != nil {
			if err == syscall.EINTR {
				continue
			}
			if err == syscall.EAGAIN && f.nonblock {
				if !f.waitIO(true) {
					return n, ErrClosed
				}
				continue
			}
			if err == syscall.EPIPE && (f.fd == 1 || f.fd == 2) {
				// As in Go: a broken pipe on standard output or standard
				// error ends the program with SIGPIPE; any other
				// descriptor reports EPIPE.
				rt.DieFromSigpipe()
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
	if !f.incref() {
		return 0, ErrClosed
	}
	n, err := f.read(p)
	f.decref()
	return n, err
}

func (f *File) read(p []byte) (int, error) {
	for {
		if !f.nonblock {
			rt.EnterSyscall()
		}
		n, err := syscall.Read(f.fd, p)
		if !f.nonblock {
			rt.ExitSyscall()
		}
		if err == syscall.EINTR {
			continue
		}
		if err == syscall.EAGAIN && f.nonblock {
			if !f.waitIO(false) {
				return 0, ErrClosed
			}
			continue
		}
		if err == nil && n == 0 && len(p) > 0 {
			return 0, io.EOF
		}
		return n, err
	}
}

// Close closes the file. Puroutines waiting to read or write it in the
// poller are woken and report ErrClosed. If a read or write is running in a
// system call, the descriptor is closed when it returns. Closing a closed
// file reports ErrClosed.
func (f *File) Close() error {
	for {
		v := atomic.LoadUint32(&f.fdmu)
		if v&fdClosed != 0 {
			return ErrClosed
		}
		// Mark the file closed and hold a reference while waking the
		// waiters, so that an operation finishing now does not release
		// the poller's record under us.
		if atomic.CompareAndSwapUint32(&f.fdmu, v, (v|fdClosed)+1) {
			break
		}
	}
	if f.poll != nil {
		f.poll.Evict()
	}
	return f.decref()
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

// cstring returns a NUL-terminated C string from the process's arguments or
// environment, which live as long as the program. If it is valid UTF-8 the
// string views it, uncounted; otherwise it is a copy with U+FFFD for each
// invalid byte, as string(b) makes, since a string must hold valid UTF-8.
func cstring(p *byte) string {
	n := 0
	for *(*byte)(unsafe.Add(unsafe.Pointer(p), n)) != 0 {
		n++
	}
	valid, ascii := rt.ValidUTF8(p, n)
	if !valid {
		return rt.StringFromBytes(p, n, alloc.Default())
	}
	return rt.MakeString(p, n, 0, ascii)
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
	fd, err := syscall.Open(path[:len(name)+1], flags|syscall.O_CLOEXEC, perm)
	if err != nil {
		return nil, NewPathError("open", name, err, a)
	}
	f := alloc.New[File](a)
	f.fd = fd
	f.name = name
	return f, nil
}

// PathError records an error and the operation and file path that caused
// it. Op and Path are strings the error keeps (language spec section 6.10),
// so the error stays valid however long the caller's own copies live. Make
// one with NewPathError, and release it with errors.Free (or by resetting
// the arena it came from).
type PathError struct {
	Op   string
	Path string
	Err  error

	a alloc.Allocator // allocator that owns the PathError, or nil
}

// Error returns "op path: err", a new string, as Go's does.
//
//pu:alloc
func (e *PathError) Error() string {
	if e.Err == nil {
		return e.Op + " " + e.Path
	}
	return e.Op + " " + e.Path + ": " + e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *PathError) Unwrap() error { return e.Err }

// Timeout reports whether this error represents a timeout.
func (e *PathError) Timeout() bool {
	t, ok := e.Err.(interface{ Timeout() bool })
	return ok && t.Timeout()
}

// Free releases the PathError's strings and returns its memory to the
// allocator it came from. It does not free Err; errors.Free frees the whole
// chain.
func (e *PathError) Free() {
	if e.a == nil {
		return
	}
	alloc.Free(e, e.a)
}

// NewPathError returns a PathError for op on path failing with err,
// allocated from a, by default the default allocator. It keeps op, and
// keeps a counted path without copying it; a path nobody counts (a literal,
// or a view made with unsafe.String, which may be of memory that is about
// to change) is copied into a string from a, so that the error stays valid
// however long the caller's path lives.
func NewPathError(op, path string, err error, a alloc.Allocator = alloc.Default()) *PathError {
	e := alloc.New[PathError](a)
	e.Op = op
	if path != "" && rt.StringControl(path) < 8 {
		path = rt.StringFromBytes(unsafe.StringData(path), len(path), a)
	}
	e.Path = path
	e.Err = err
	e.a = a
	return e
}
