package compat

import (
	"pluto/io"
	"pluto/os"
)

// The operating-system name functions of Go's os package return a string
// that holds the bytes the system gave, valid UTF-8 or not. A Plutonium
// string holds only valid UTF-8, so pluto/os returns an error for such a
// name (language spec section 7.3 and F.9). Translated code takes the
// display forms: each invalid byte is U+FFFD. The compiler's own uses read
// settings (PLUTO_SSACHECK and the like), which are ASCII.

// Getenv is os.Getenv with each invalid byte of the value replaced by
// U+FFFD.
func Getenv(key string) string { return os.GetenvLossy(key) }

// LookupEnv is os.LookupEnv with each invalid byte of the value replaced by
// U+FFFD.
func LookupEnv(key string) (string, bool) { return os.LookupEnvLossy(key) }

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

// Caller is runtime.Caller: Plutonium has no way to ask for the caller's file
// and line, so ok is always false. The compiler's assertion messages lose
// their location, which the panic's own trace gives.
func Caller(skip int) (pc uintptr, file string, line int, ok bool) {
	return 0, "", 0, false
}
