// Package os provides files, arguments and process control for hosted
// programs.
package os

import (
	"pluto/rt"
	"pluto/syscall"
	"unsafe"
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
			return 0, eof
		}
		return n, err
	}
}

// Close closes the file.
func (f *File) Close() error { return syscall.Close(f.fd) }

// eof mirrors io.EOF without importing io (io depends on nothing hosted).
var eof = &eofError{}

type eofError struct{}

func (*eofError) Error() string { return "EOF" }

// IsEOF reports whether err marks the end of input.
func IsEOF(err error) bool { return err == error(eof) }

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

// Open opens a file for reading.
func Open(name string) (*File, error) {
	return OpenFile(name, syscall.O_RDONLY, 0)
}

// Create creates or truncates a file for writing.
func Create(name string) (*File, error) {
	return OpenFile(name, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_TRUNC, 0o644)
}

// OpenFile opens a file with the given flags and permissions. The File is
// allocated from the default allocator; free it with alloc.Free after Close.
func OpenFile(name string, flags int, perm uint32) (*File, error) {
	var path [4096]byte
	if len(name) >= len(path) {
		return nil, syscall.EINVAL
	}
	copy(path[:], name)
	fd, err := syscall.Open(path[:len(name)+1], flags, perm)
	if err != nil {
		return nil, err
	}
	f := new(File)
	f.fd = fd
	f.name = name
	return f, nil
}
