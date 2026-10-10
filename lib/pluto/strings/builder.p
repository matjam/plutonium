package strings

import (
	"pluto/alloc"
	"pluto/unicode/utf8"
	"unsafe"
)

// A Builder builds a string by appending to a byte buffer. It is how
// Plutonium code joins strings at run time, since s + t is not allowed for
// non-constant strings.
//
// The buffer is either the caller's (NewBuilder) or comes from an allocator
// (Grow). A Builder without an allocator never allocates: a write that does
// not fit in the buffer's capacity panics, as append does. A Builder with an
// allocator grows by allocating a buffer twice as large from it, copying,
// and freeing the old buffer if the allocator owned it.
//
// The zero Builder has no buffer and no allocator; call Grow, which gives
// it one (the default allocator unless Grow is passed another), or start
// from NewBuilder. A Builder must not be copied after first use.
//
//	var b strings.Builder
//	b.Grow(64, &arena) // the buffer and later growth come from arena
//	defer b.Free()
type Builder struct {
	buf   []byte
	a     alloc.Allocator // allocator that grows the buffer, or nil
	owned bool            // buf came from a and must be freed to it
}

// NewBuilder returns a Builder that writes into buf, starting empty. It
// never allocates: writes beyond cap(buf) panic.
func NewBuilder(buf []byte) Builder {
	return Builder{buf: buf[:0]}
}

// String returns the accumulated string. It is a view of the buffer, not a
// copy: it stays valid until the Builder is Reset, grown or freed, or the
// caller's buffer is reused.
func (b *Builder) String() string {
	if len(b.buf) == 0 {
		return ""
	}
	return unsafe.String(unsafe.SliceData(b.buf), len(b.buf))
}

// Bytes returns the accumulated bytes, a view of the buffer like String.
func (b *Builder) Bytes() []byte { return b.buf }

// Len returns the number of accumulated bytes; b.Len() == len(b.String()).
func (b *Builder) Len() int { return len(b.buf) }

// Cap returns the capacity of the builder's underlying byte slice.
func (b *Builder) Cap() int { return cap(b.buf) }

// Reset empties the Builder, keeping its buffer and allocator.
func (b *Builder) Reset() { b.buf = b.buf[:0] }

// Free returns an allocated buffer to its allocator and leaves the Builder
// empty with no buffer. A caller-provided buffer is not freed.
func (b *Builder) Free() {
	if b.owned {
		b.a.Free(unsafe.Pointer(unsafe.SliceData(b.buf)), uintptr(cap(b.buf)), 1)
	}
	b.buf = nil
	b.owned = false
}

// Grow makes room for at least n more bytes. If the Builder has no
// allocator it takes a, by default alloc.Default(), which then grows it from
// here on; a Builder that has an allocator keeps it. This is the one place a
// Builder picks up an allocator. It panics if n is negative.
func (b *Builder) Grow(n int, a alloc.Allocator = alloc.Default()) {
	if n < 0 {
		panic("strings.Builder.Grow: negative count")
	}
	if b.a == nil {
		b.a = a
	}
	if cap(b.buf)-len(b.buf) < n {
		b.grow(n)
	}
}

// grow makes room for n more bytes or panics if the Builder cannot grow.
func (b *Builder) grow(n int) {
	if b.a == nil {
		panic("strings.Builder: out of room and no allocator (call Grow first)")
	}
	c := 2*cap(b.buf) + n
	if c < 16 {
		c = 16
	}
	b.realloc(c)
}

// realloc moves the contents into a new buffer of capacity c from b.a.
func (b *Builder) realloc(c int) {
	p := b.a.Alloc(uintptr(c), 1)
	if p == nil {
		panic("strings.Builder: out of memory")
	}
	nb := unsafe.Slice((*byte)(p), c)[:len(b.buf)]
	copy(nb, b.buf)
	if b.owned {
		b.a.Free(unsafe.Pointer(unsafe.SliceData(b.buf)), uintptr(cap(b.buf)), 1)
	}
	b.buf = nb
	b.owned = true
}

// Write appends the contents of p to b's buffer. It always returns len(p),
// nil, so Builder implements io.Writer.
func (b *Builder) Write(p []byte) (int, error) {
	if cap(b.buf)-len(b.buf) < len(p) {
		b.grow(len(p))
	}
	b.buf = append(b.buf, p...)
	return len(p), nil
}

// WriteByte appends the byte c to b's buffer. The returned error is always
// nil.
func (b *Builder) WriteByte(c byte) error {
	if len(b.buf) == cap(b.buf) {
		b.grow(1)
	}
	b.buf = append(b.buf, c)
	return nil
}

// WriteRune appends the UTF-8 encoding of Unicode code point r to b's
// buffer. It returns the length of r and a nil error.
func (b *Builder) WriteRune(r rune) (int, error) {
	need := utf8.RuneLen(r)
	if need < 0 {
		need = 3 // the encoding of utf8.RuneError
	}
	if cap(b.buf)-len(b.buf) < need {
		b.grow(need)
	}
	n := len(b.buf)
	b.buf = utf8.AppendRune(b.buf, r)
	return len(b.buf) - n, nil
}

// WriteString appends the contents of s to b's buffer. It returns the
// length of s and a nil error.
func (b *Builder) WriteString(s string) (int, error) {
	if cap(b.buf)-len(b.buf) < len(s) {
		b.grow(len(s))
	}
	b.buf = append(b.buf, s...)
	return len(s), nil
}
