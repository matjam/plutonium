// Package pu is the Plutonium side of the compiler's internal/pu: the part
// of that Go mirror with no library counterpart yet. go2pu translates the
// other names of internal/pu to the library types they mirror (vec.Vec,
// hash.Map, hash.Set, strings.Builder), and pu.New and pu.Sprintf to this
// package's.
package pu

import (
	"pluto/alloc"
	"pluto/fmt"
	"pluto/strings"
)

// Sprintf formats according to format and returns the text in a
// strings.Builder that allocates from the default allocator. It stands in
// for the language spec's fmt.Sprintf, which returns a strings.Buf
// (roadmap items 47 and 51): pluto/fmt cannot return a strings type while
// the tests of pluto/strings import pluto/testing, which imports pluto/fmt.
func Sprintf(format string, args ...any) strings.Builder {
	var b strings.Builder
	fmt.Fprintf(&b, format, args...)
	return b
}

// New returns a pointer to a new variable holding v, allocated from the
// default allocator, which the driver points at the arena of the current
// phase (alloc.Use). The variable is a view of that arena: it is never
// freed on its own, and lives until the arena is reset or dropped
// (language spec section 6.6).
func New[T any](v T) *T {
	p := alloc.New[T]()
	*p = v
	return p
}
