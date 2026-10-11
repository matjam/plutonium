// Package os provides files, arguments and process control for hosted
// programs.
//
// Errors follow pluto/errors. Opening a file reports a *PathError that
// records the operation and the path; it comes from the same allocator as
// the File (the default allocator, or the one given to OpenFile). Reads,
// writes and Close report the syscall.Errno alone, so that the I/O path
// never allocates; Read reports io.EOF at the end of input, and an
// operation on a closed File reports ErrClosed. An Errno that the table
// does not know reports "unknown error".
//
// # Names from the operating system
//
// A string holds only valid UTF-8 (language spec section 7.3), but Linux
// does not promise it: a file name is any bytes except NUL and '/', and so
// are a symbolic link's target, the working directory, a command-line
// argument and an environment variable. Bytes that the system hands back
// become a string only through validation, which replaces each byte that
// does not begin a valid UTF-8 sequence with U+FFFD, as
// strings.FromUTF8Lossy does. A name that is valid UTF-8 comes back as it
// is. The conversion is lossy: two different files can give the same
// string, and a lossy name must not be used to open, rename or remove the
// file it came from.
//
// The exact bytes are available next to every string that can be lossy:
//
//	DirEntry.Name, FileInfo.Name      DirEntry.RawName
//	Readlink                          AppendReadlink, ReadlinkBytes
//	Getwd (and filepath.Abs)          AppendWd (and filepath.AppendAbs)
//	Arg, Args (and flag.Arg)          ArgBytes, ArgsBytes (and flag.ArgBytes)
//	Getenv, LookupEnv, Environ,       GetenvBytes, LookupEnvBytes,
//	TempDir                           EnvironBytes, TempDirBytes
//	MkdirTemp (and exec.LookPath)     AppendMkdirTemp (and exec.AppendLookPath)
//
// filepath.WalkDirBytes passes each path as its exact bytes. PathError.Path
// and File.Name have no byte form: the caller has the bytes it passed. The
// views RawName, ArgBytes and GetenvBytes are valid as long as what they
// view: the DirEntries that holds the entry, or the life of the program for
// the arguments and the environment.
//
// The core operations on a path have a form that takes the path as a
// []byte, named with the suffix Bytes: OpenBytes, OpenFileBytes,
// CreateBytes, StatBytes, LstatBytes, ReadDirBytes, ReadlinkBytes,
// ReadFileBytes, WriteFileBytes, MkdirBytes, MkdirAllBytes, RemoveBytes,
// RemoveAllBytes, RenameBytes, ChmodBytes and ChdirBytes. A program that
// must handle any file writes os.OpenBytes(e.RawName()), joined to the
// directory, where it would write os.Open(e.Name()). ReadDir sorts by the
// exact bytes of the names, and os/exec passes the environment to a child
// unchanged. The functions that must not store a partial path (AppendWd,
// AppendReadlink, ReadlinkBytes, AppendMkdirTemp, exec.AppendLookPath)
// leave the destination as it was and return ENAMETOOLONG (ERANGE for
// AppendWd) when the path does not fit in a vec.Fixed.
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
	// reads and writes wait in the poller; Fd clears it, possibly while
	// another task reads or writes, so it is atomic.
	nonblock atomic.Bool
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
	if f.nonblock.Load() {
		if fl, err := syscall.Fcntl(f.fd, syscall.F_GETFL, 0); err == nil {
			syscall.Fcntl(f.fd, syscall.F_SETFL, fl&^syscall.O_NONBLOCK)
		}
		f.nonblock.Store(false)
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
		f.nonblock.Store(true)
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
		if !f.nonblock.Load() {
			rt.EnterSyscall()
		}
		m, err := syscall.Write(f.fd, p[n:])
		if !f.nonblock.Load() {
			rt.ExitSyscall()
		}
		if err != nil {
			if err == syscall.EINTR {
				continue
			}
			if err == syscall.EAGAIN && f.nonblock.Load() {
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
		if !f.nonblock.Load() {
			rt.EnterSyscall()
		}
		n, err := syscall.Read(f.fd, p)
		if !f.nonblock.Load() {
			rt.ExitSyscall()
		}
		if err == syscall.EINTR {
			continue
		}
		if err == syscall.EAGAIN && f.nonblock.Load() {
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
func Exit(code int) { rt.Exit(code) }

// NArgs returns the number of command-line arguments, including the
// program name.
func NArgs() int {
	n, _ := rt.Args()
	return n
}

// Arg returns command-line argument i (Arg(0) is the program name). An
// argument is any bytes but NUL, so it need not be valid UTF-8; a string
// must be (language spec section 18.8), so Arg replaces each invalid byte
// with U+FFFD, as strings.FromUTF8Lossy does. ArgBytes returns the exact bytes.
func Arg(i int) string { return lossyView(ArgBytes(i)) }

// ArgBytes returns the exact bytes of command-line argument i (ArgBytes(0)
// is the program name), or nil if there is no such argument. The result is
// a view of the process's argument memory, valid for the life of the
// program; do not modify it.
func ArgBytes(i int) []byte {
	n, argv := rt.Args()
	if i < 0 || i >= n {
		return nil
	}
	return cbytes(*(**byte)(unsafe.Add(argv, i*8)))
}

// Getenv returns the value of the environment variable key, or "". The
// value is made a valid string as Arg makes arguments (invalid bytes become
// U+FFFD); GetenvBytes returns the exact bytes.
func Getenv(key string) string {
	v, _ := lookupEnv(strBytes(key))
	return lossyView(v)
}

// GetenvBytes returns the exact bytes of the value of the environment
// variable named key, or nil. Like ArgBytes, the result is a view of the
// process's environment, valid for the life of the program.
func GetenvBytes(key []byte) []byte {
	v, _ := lookupEnv(key)
	return v
}

// LookupEnvBytes is GetenvBytes that also reports whether the variable is
// set, which tells an empty value from an unset variable.
func LookupEnvBytes(key []byte) ([]byte, bool) { return lookupEnv(key) }

// lookupEnv finds the value of the variable named key in the environment.
func lookupEnv(key []byte) ([]byte, bool) {
	env := rt.Environ()
	for i := 0; ; i++ {
		p := *(**byte)(unsafe.Add(env, i*8))
		if p == nil {
			return nil, false
		}
		kv := cbytes(p)
		if len(kv) > len(key) && kv[len(key)] == '=' && bytesEqual(kv[:len(key)], key) {
			return kv[len(key)+1:], true
		}
	}
}

// bytesEqual reports whether a and b hold the same bytes. (pluto/bytes
// imports this package's dependencies, so os cannot use it.)
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// strBytes views a string's bytes. The caller must not modify them.
func strBytes(s string) []byte {
	if len(s) == 0 {
		return nil
	}
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

// cbytes returns the bytes, without the NUL, of a C string in the process's
// arguments or environment, which live as long as the program.
func cbytes(p *byte) []byte {
	n := 0
	for *(*byte)(unsafe.Add(unsafe.Pointer(p), n)) != 0 {
		n++
	}
	if n == 0 {
		return nil
	}
	return unsafe.Slice(p, n)
}

// lossyView returns b as a string. If b is valid UTF-8 the string is an
// uncounted view of b, which must outlive it and not change; otherwise it
// is a counted copy with U+FFFD for each invalid byte, as strings.FromUTF8Lossy makes,
// since a string must hold valid UTF-8. Every os function that turns bytes
// from the system (a name, a path, an argument, a variable) into a string
// does so through here, through lossyCopy or through strings.FromUTF8Lossy.
func lossyView(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	valid, ascii := rt.ValidUTF8(&b[0], len(b))
	if !valid {
		return rt.StringFromBytes(&b[0], len(b), alloc.Default())
	}
	return rt.MakeString(&b[0], len(b), 0, ascii)
}

// lossyCopy returns b as a counted string from a, with U+FFFD for each
// invalid byte.
func lossyCopy(b []byte, a alloc.Allocator) string {
	if len(b) == 0 {
		return ""
	}
	return rt.StringFromBytes(&b[0], len(b), a)
}

// Open opens a file for reading, allocating as OpenFile does.
func Open(name string, a alloc.Allocator = alloc.Default()) (*File, error) {
	return OpenFile(name, syscall.O_RDONLY, 0, a)
}

// OpenBytes is Open for a file name that is bytes, which need not be valid
// UTF-8 (language spec section 18.8). A name on Linux is any bytes but NUL
// and a string can hold only valid UTF-8, so a file named "\xff.txt" can
// be opened only this way. The core operations of this package that take a
// path have a Bytes form like this one (OpenFileBytes, CreateBytes,
// StatBytes, LstatBytes, ReadDirBytes, ReadlinkBytes, ReadFileBytes,
// WriteFileBytes, MkdirBytes, MkdirAllBytes, RemoveBytes, RemoveAllBytes,
// RenameBytes, ChmodBytes, ChdirBytes). The error it reports and the File's
// Name hold the path with each invalid byte replaced by U+FFFD; the caller
// still has the exact bytes.
func OpenBytes(name []byte, a alloc.Allocator = alloc.Default()) (*File, error) {
	return OpenFileBytes(name, syscall.O_RDONLY, 0, a)
}

// Create creates or truncates a file for writing, allocating as OpenFile
// does.
func Create(name string, a alloc.Allocator = alloc.Default()) (*File, error) {
	return OpenFile(name, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_TRUNC, 0o644, a)
}

// CreateBytes is Create for a name that is bytes (see OpenBytes).
func CreateBytes(name []byte, a alloc.Allocator = alloc.Default()) (*File, error) {
	return OpenFileBytes(name, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_TRUNC, 0o644, a)
}

// OpenFile opens a file with the given flags and permissions. The File, or
// on failure the *PathError, is allocated from a, by default the default
// allocator; free the File with alloc.Free after Close, and the error with
// errors.Free.
func OpenFile(name string, flags int, perm uint32, a alloc.Allocator = alloc.Default()) (*File, error) {
	f, err := openFile(strBytes(name), name, flags, perm, a)
	if f != nil {
		// A name made with unsafe.String may view memory that does not
		// live as long as the File: keep a counted copy of it.
		f.name = rt.KeepString(name, a)
	}
	return f, err
}

// OpenFileBytes is OpenFile for a name that is bytes (see OpenBytes).
func OpenFileBytes(name []byte, flags int, perm uint32, a alloc.Allocator = alloc.Default()) (*File, error) {
	f, err := openFile(name, "", flags, perm, a)
	if f != nil {
		f.name = lossyCopy(name, a)
	}
	return f, err
}

// openFile opens name, which sname is the string form of (or "" if the
// caller has only bytes), and returns a File without its name.
func openFile(name []byte, sname string, flags int, perm uint32, a alloc.Allocator) (*File, error) {
	var buf [maxPath]byte
	path, err := cpathBytes(name, &buf)
	if err != nil {
		return nil, newPathError("open", name, sname, err, a)
	}
	fd, err := syscall.Open(path, flags|syscall.O_CLOEXEC, perm)
	if err != nil {
		return nil, newPathError("open", name, sname, err, a)
	}
	f := alloc.New[File](a)
	f.fd = fd
	return f, nil
}

// PathError records an error and the operation and file path that caused
// it. Op and Path are strings the error keeps (language spec section 18.8),
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
	e.Path = rt.KeepString(path, a)
	e.Err = err
	e.a = a
	return e
}

// NewPathErrorBytes is NewPathError for a path that is bytes. The error
// holds the path as a string, with each invalid byte replaced by U+FFFD
// (a copy from a, which the error keeps).
func NewPathErrorBytes(op string, path []byte, err error, a alloc.Allocator = alloc.Default()) *PathError {
	return NewPathError(op, lossyCopy(path, a), err, a)
}

// newPathError makes the error for an operation on name, whose string form
// sname is "" if the caller has only bytes. A string path is kept (or
// copied) as NewPathError does; bytes are copied with lossy replacement.
func newPathError(op string, name []byte, sname string, err error, a alloc.Allocator) *PathError {
	if sname == "" && len(name) > 0 {
		sname = lossyCopy(name, a)
	}
	return NewPathError(op, sname, err, a)
}
