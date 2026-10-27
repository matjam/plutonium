package compat

import (
	"pluto/errors"
	"pluto/io"
	"pluto/os"
	"pluto/strconv"
	"pluto/strings"
	"pluto/syscall"
)

// The operating-system name functions of Go's os package return a string
// that holds the bytes the system gave, valid UTF-8 or not. A Plutonium
// string holds only valid UTF-8, so pluto/os returns an error for such a
// name (language spec section 7.3 and F.9). Translated code takes the
// display forms: each invalid byte is U+FFFD. The compiler's own uses read
// settings (PLUTO_SSACHECK and the like), which are ASCII.

// Getenv is os.Getenv with each invalid byte of the value replaced by
// U+FFFD.
func Getenv(key string) string {
	v, _ := LookupEnv(key)
	return v
}

// LookupEnv is os.LookupEnv with each invalid byte of the value replaced by
// U+FFFD.
func LookupEnv(key string) (string, bool) {
	if v, ok := envOverride[key]; ok {
		return v, true
	}
	return os.LookupEnvLossy(key)
}

// envOverride holds the values SetenvForTest set.
var envOverride map[string]string

// SetenvForTest makes Getenv and LookupEnv return value for key, without
// changing the process's environment, which pluto/os cannot change. It is
// the compiler's unit tests' t.Setenv: the test calls the function it
// returns, which restores what Getenv returned before, when it is done.
func SetenvForTest(key, value string) (restore func()) {
	if envOverride == nil {
		envOverride = map[string]string{}
	}
	old, had := envOverride[key]
	envOverride[key] = value
	return func() {
		if had {
			envOverride[key] = old
		} else {
			delete(envOverride, key)
		}
	}
}

// TempDir is os.TempDir with each invalid byte replaced by U+FFFD.
func TempDir() string { return os.TempDirLossy() }

// Args is os.Args: the program name and its arguments, each invalid byte
// of an argument replaced by U+FFFD.
func Args() []string {
	n := os.NArgs()
	args := Make[string](n)
	for i := 0; i < n; i++ {
		args[i] = os.ArgLossy(i)
	}
	return args
}

// ReadFile is os.ReadFile: the contents of the named file in memory from
// alloc.Default(), which nothing frees.
func ReadFile(name string) ([]byte, error) {
	f := os.Open(name) or return nil, err
	data, err := ReadAll(&f)
	_ = f.Close()
	return data, err
}

// ReadAll is io.ReadAll: everything r yields until EOF, in memory from
// alloc.Default().
func ReadAll(r io.Reader) ([]byte, error) {
	var data []byte
	var chunk [16 << 10]byte
	for {
		n, err := r.Read(chunk[:])
		data = AppendPlain(data, chunk[:n]...)
		if err == io.EOF {
			return data, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

// Executable is os.Executable: the path of the running program, as Linux
// gives it.
func Executable() (string, error) {
	return os.Readlink("/proc/self/exe")
}

// A TempFile is the *os.File os.CreateTemp returns, with Go's methods.
type TempFile owner struct {
	f    os.File
	name string
}

// Write writes p to the file.
func (t *TempFile) Write(p []byte) (int, error) { return t.f.Write(p) }

// Close closes the file.
func (t *TempFile) Close() error { return t.f.Close() }

// Name is the file's name.
func (t *TempFile) Name() string { return t.name }

// NameLossy is Name: the name go2pu gives a Go *os.File's Name method.
func (t *TempFile) NameLossy() string { return t.name }

// CreateTemp is os.CreateTemp: a new file in dir whose name is pattern with
// its last "*" (or its end) replaced by a number no other file there has,
// opened for reading and writing.
func CreateTemp(dir, pattern string) (TempFile, error) {
	prefix, suffix := pattern, ""
	if i := strings.LastIndexByte(pattern, '*'); i >= 0 {
		prefix, suffix = strings.Slice(pattern, 0, i), strings.SliceFrom(pattern, i+1)
	}
	seed := uint64(os.Getpid())*2654435761 + tempSeq
	var last error
	for try := 0; try < 10000; try++ {
		tempSeq++
		seed = seed*6364136223846793005 + 1442695040888963407
		name := Concat(dir, "/", prefix, strconv.FormatUint(seed>>16, 10), suffix)
		f, err := os.OpenFile(name, syscall.O_RDWR|syscall.O_CREAT|syscall.O_EXCL, 0o600)
		if err == nil {
			return TempFile{f: f, name: name}, nil
		}
		last = err
		if !errors.Is(err, os.ErrExist) {
			break
		}
	}
	return TempFile{}, last
}

// The flags of Go's os.OpenFile (pluto/os takes the system's).
const (
	O_RDONLY = syscall.O_RDONLY
	O_WRONLY = syscall.O_WRONLY
	O_RDWR   = syscall.O_RDWR
	O_APPEND = syscall.O_APPEND
	O_CREATE = syscall.O_CREAT
	O_EXCL   = syscall.O_EXCL
	O_TRUNC  = syscall.O_TRUNC
)

// tempSeq tells apart the names CreateTemp tries in one run.
var tempSeq uint64

// Caller is runtime.Caller: Plutonium has no way to ask for the caller's file
// and line, so ok is always false. The compiler's assertion messages lose
// their location, which the panic's own trace gives.
func Caller(skip int) (pc uintptr, file string, line int, ok bool) {
	return 0, "", 0, false
}
