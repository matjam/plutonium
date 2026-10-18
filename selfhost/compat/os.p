package compat

import "pluto/os"

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
