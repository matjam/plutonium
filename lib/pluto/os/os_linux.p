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
type File struct {
	fd   int
	name string
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
		m, err := syscall.Write(f.fd, p[n:])
		if err != nil {
			if err == syscall.EINTR {
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
		n, err := syscall.Read(f.fd, p)
		if err == syscall.EINTR {
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
// allocated from a, by default the default allocator. It keeps op and path
// without copying their bytes.
func NewPathError(op, path string, err error, a alloc.Allocator = alloc.Default()) *PathError {
	e := alloc.New[PathError](a)
	e.Op = op
	e.Path = path
	e.Err = err
	e.a = a
	return e
}
