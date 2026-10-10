package types2_test

import (
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

// TestRemovedConversions checks the conversions of Go that Plutonium does
// not have (language spec section 3), and that the neighbours that stay are
// still accepted.
func TestRemovedConversions(t *testing.T) {
	testguard.Parallel(t)
	const decls = `package p

type Bytes []byte
type MyByte byte
type Name string

func use(...any) {}

`
	tests := []struct {
		name string
		body string
		want string // part of the error; empty if the conversion is accepted
	}{
		// string(b) of a byte slice.
		{"bytes", "func f(b []byte) string { return string(b) }", "string(b) of a byte slice is not permitted"},
		{"uint8", "func f(b []uint8) string { return string(b) }", "string(b) of a byte slice is not permitted"},
		{"named slice", "func f(b Bytes) string { return string(b) }", "string(b) of a byte slice is not permitted"},
		{"named byte", "func f(b []MyByte) string { return string(b) }", "string(b) of a byte slice is not permitted"},
		{"named string", "func f(b []byte) Name { return Name(b) }", "string(b) of a byte slice is not permitted"},
		{"array slice", "func f(a *[4]byte) string { return string(a[:]) }", "string(b) of a byte slice is not permitted"},
		{"type parameter", "func f[S ~[]byte](b S) string { return string(b) }", "string(b) of a byte slice is not permitted"},
		{"type parameter union", "func f[S ~[]byte | ~string](b S) string { return string(b) }", "string(b) of a byte slice is not permitted"},
		{"message names the fix", "func f(b []byte) string { return string(b) }", "use strings.FromUTF8 or strings.FromUTF8Lossy"},

		// []byte(s) and []rune(s).
		{"to bytes", "func f(s string) []byte { return []byte(s) }", "[]byte(s) is not permitted"},
		{"to bytes from a constant", `func f() []byte { return []byte("x") }`, "[]byte(s) is not permitted"},
		{"to named bytes", "func f(s string) Bytes { return Bytes(s) }", "[]byte(s) is not permitted"},
		{"to bytes from a named string", "func f(s Name) []byte { return []byte(s) }", "[]byte(s) is not permitted"},
		{"to bytes in a type parameter", "func f[S ~string, B ~[]byte](s S) B { return B(s) }", "[]byte(s) is not permitted"},
		{"to runes", "func f(s string) []rune { return []rune(s) }", "[]rune(s) is not permitted"},
		{"to runes from a constant", `func f() []rune { return []rune("x") }`, "[]rune(s) is not permitted"},
		{"to int32s", "func f(s string) []int32 { return []int32(s) }", "[]rune(s) is not permitted"},
		{"bytes message names the fix", "func f(s string) []byte { return []byte(s) }", "use strings.Bytes, or copy into a buffer"},
		{"runes message names the fix", "func f(s string) []rune { return []rune(s) }", "use strings.Runes, or range over the string"},

		// string(i) of an integer other than a rune.
		{"int", "func f(i int) string { return string(i) }", "string(i) of an integer other than a rune is not permitted"},
		{"byte", "func f(b byte) string { return string(b) }", "string(i) of an integer other than a rune is not permitted"},
		{"int64", "func f(i int64) string { return string(i) }", "string(i) of an integer other than a rune is not permitted"},
		{"uint32", "func f(i uint32) string { return string(i) }", "string(i) of an integer other than a rune is not permitted"},
		{"uintptr", "func f(i uintptr) string { return string(i) }", "string(i) of an integer other than a rune is not permitted"},
		{"named int32", "type R int32\nfunc f(r R) string { return string(r) }", "string(i) of an integer other than a rune is not permitted"},
		{"typed constant", "const c int = 65\nfunc f() string { return string(c) }", "string(i) of an integer other than a rune is not permitted"},
		{"to a named string", "func f(i int) Name { return Name(i) }", "string(i) of an integer other than a rune is not permitted"},
		{"in a type parameter", "func f[T ~int32 | ~int](x T) string { return string(x) }", "string(i) of an integer other than a rune is not permitted"},
		{"message names the fix", "func f(i int) string { return string(i) }", "use string(rune(i)) or strconv.Itoa(i)"},

		// What stays.
		{"rune type", "func f(r rune) string { return string(r) }", ""},
		{"int32 is rune", "func f(r int32) string { return string(r) }", ""},
		{"untyped constant", "func f() string { return string(65) }", ""},
		{"untyped rune constant", "func f() string { return string('A') }", ""},
		{"typed rune constant", "const c rune = 65\nfunc f() string { return string(c) }", ""},
		{"converted to a rune", "func f(i int) string { return string(rune(i)) }", ""},
		{"type parameter of runes", "func f[T ~int32](x T) string { return string(x) }", ""},
		{"integer to integer", "func f(i int) int64 { return int64(i) }", ""},
		{"bytes of nil", "func f() []byte { return []byte(nil) }", ""},
		{"runes to bytes of other types", "func f(r []uint16) []uint16 { return []uint16(r) }", ""},
		{"copy from a string", "func f(s string, b []byte) int { return copy(b, s) }", ""},
		{"range over a string", "func f(s string) int { n := 0; for _, r := range s { n += int(r) }; return n }", ""},
		{"runes", "func f(r []rune) string { return string(r) }", ""},
		{"rune", "func f(r rune) string { return string(r) }", ""},
		{"string", "func f(s Name) string { return string(s) }", ""},
		{"bytes to bytes", "func f(b []byte) Bytes { return Bytes(b) }", ""},
		{"type parameter of strings", "func f[S ~string](s S) string { return string(s) }", ""},
		{"no specific types", "func f[T any](x T) any { return any(x) }", ""},
		{"byte array to array", "func f(b [4]byte) [4]byte { return [4]byte(b) }", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testguard.Parallel(t)
			if tt.want == "" {
				mustCheck(t, decls+tt.body)
				return
			}
			_, _, err := checkSrc(t, decls+tt.body)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %v, want one containing %q", err, tt.want)
			}
		})
	}
}
