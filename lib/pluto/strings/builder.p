package strings

import (
	"pluto/alloc"
	"pluto/rt"
	"pluto/unicode/utf8"
	"unsafe"
)

// A Builder builds a string by appending to a buffer, and String returns the
// result without copying it: the string shares the Builder's buffer, which
// is a counted string buffer (language spec section 18.8), and keeps it
// alive after the Builder grows, is reset or is freed.
//
// A Builder gets its allocator once: from Init, or, if Init was never
// called, from alloc.Default() the first time it has to grow. The zero
// Builder is ready to use and allocates from the default allocator:
//
//	var b strings.Builder
//	defer b.Free()
//	b.WriteString("hello, ")
//	b.WriteString(name)
//	greeting := b.String()
//
// A Builder owns the buffer it grows, so it is an owner (language spec
// section 6.4): it moves instead of being copied, and Drop releases the
// buffer. To build text in a caller's array without allocating, use a
// vec.Fixed[byte] and the Append functions (strconv.AppendInt, fmt.Appendf,
// vec.AppendString), then string(f.Slice()).
//
// The bytes written need not be valid UTF-8 one at a time, but String
// replaces each byte that does not begin a valid UTF-8 sequence with U+FFFD,
// as strings.FromUTF8Lossy does; it checks only the bytes written since the last call.
type Builder owner struct {
	buf []byte          // the bytes so far, within the buffer
	ctl uintptr         // reference to the counted buffer, or 0
	a   alloc.Allocator // allocator that grows the buffer, or nil

	// String has shared buf[:len(buf)] with a string, so those bytes must
	// not change: a reset starts a new buffer.
	shared bool
	// buf[:valid] is known to be valid UTF-8, ending at a rune boundary,
	// and nonASCII says whether a byte >= 0x80 is among it.
	valid    int
	nonASCII bool
}

// Init sets the allocator the Builder grows from, by default the default
// allocator. Call it before the Builder first grows: a Builder's allocator
// cannot be changed once it has one, and Init panics if it does, or if a
// is nil.
func (b *Builder) Init(a alloc.Allocator = alloc.Default()) {
	if a == nil {
		panic("strings.Builder.Init: nil allocator")
	}
	if b.a != nil {
		panic("strings.Builder.Init: the Builder already has an allocator")
	}
	b.a = a
}

// String returns the accumulated string. It shares the Builder's buffer
// rather than copying it, unless the bytes are not valid UTF-8; either way
// the string stays valid after the Builder changes or is freed.
func (b *Builder) String() string {
	n := len(b.buf)
	if n == 0 {
		return ""
	}
	b.check()
	if b.valid < n {
		// Not valid UTF-8: a copy with U+FFFD in place of the bad bytes.
		a := b.a
		if a == nil {
			a = alloc.Default()
		}
		return rt.StringFromBytes(unsafe.SliceData(b.buf), n, a)
	}
	b.shared = true
	return rt.MakeString(unsafe.SliceData(b.buf), n, b.ctl, !b.nonASCII)
}

// check extends the prefix of the buffer known to be valid UTF-8 over the
// bytes written since, stopping at the first invalid sequence.
func (b *Builder) check() {
	for b.valid < len(b.buf) {
		c := b.buf[b.valid]
		if c < utf8.RuneSelf {
			b.valid++
			continue
		}
		r, size := utf8.DecodeRune(b.buf[b.valid:])
		if r == utf8.RuneError && size == 1 {
			return
		}
		b.nonASCII = true
		b.valid += size
	}
}

// Bytes returns the accumulated bytes, a view of the buffer that is valid
// until the next change to the Builder.
func (b *Builder) Bytes() []byte { return b.buf }

// Len returns the number of accumulated bytes; b.Len() == len(b.String())
// when the bytes are valid UTF-8.
func (b *Builder) Len() int { return len(b.buf) }

// Cap returns the capacity of the builder's underlying byte slice.
func (b *Builder) Cap() int { return cap(b.buf) }

// Reset empties the Builder. It keeps its buffer and allocator, unless a
// string from String shares the buffer: then the next write starts a new
// one, and the string keeps the old.
func (b *Builder) Reset() {
	if b.shared {
		b.Free()
		return
	}
	b.buf = b.buf[:0]
	b.valid, b.nonASCII = 0, false
}

// Free drops the Builder's reference to its buffer and leaves it empty with
// no buffer; strings made with String keep the buffer alive. It keeps its
// allocator for later writes.
func (b *Builder) Free() {
	if b.ctl != 0 {
		rt.ReleaseString(b.ctl)
	}
	b.buf, b.ctl = nil, 0
	b.shared = false
	b.valid, b.nonASCII = 0, false
}

// Drop releases the Builder's buffer, as Free does: the owner's release
// under its spec name (strings.Buf, roadmap item 51).
func (b *Builder) Drop() { b.Free() }

// Grow makes room for at least n more bytes, growing the buffer from the
// Builder's allocator if it has to (see Init). It panics if n is negative.
func (b *Builder) Grow(n int) {
	if n < 0 {
		panic("strings.Builder.Grow: negative count")
	}
	if cap(b.buf)-len(b.buf) < n {
		b.grow(n)
	}
}

// grow makes room for n more bytes or panics if the Builder cannot grow. A
// Builder without an allocator takes the default allocator here.
func (b *Builder) grow(n int) {
	if b.a == nil {
		b.a = alloc.Default()
		if b.a == nil {
			panic("strings.Builder: no allocator: call alloc.SetDefault or Init before writing")
		}
	}
	c := 2*cap(b.buf) + n
	if c < 16 {
		c = 16
	}
	if len(b.buf) == 0 && n > c/2 {
		c = n // a first write of a known size gets exactly that
	}
	b.realloc(c)
}

// realloc moves the contents into a new counted buffer of capacity c from
// b.a. The old buffer is released; a string from String keeps it alive.
func (b *Builder) realloc(c int) {
	p, ctl := rt.NewStringBuffer(c, b.a)
	nb := unsafe.Slice(p, c)[:len(b.buf)]
	copy(nb, b.buf)
	if b.ctl != 0 {
		rt.ReleaseString(b.ctl)
	}
	b.buf, b.ctl = nb, ctl
	b.shared = false
}

// Write appends the contents of p to b's buffer. It always returns len(p),
// nil, so Builder implements io.Writer.
func (b *Builder) Write(p []byte) (int, error) {
	n := len(b.buf)
	if cap(b.buf)-n < len(p) {
		b.grow(len(p))
	}
	b.buf = b.buf[:n+len(p)]
	copy(b.buf[n:], p)
	return len(p), nil
}

// WriteByte appends the byte c to b's buffer. The returned error is always
// nil.
func (b *Builder) WriteByte(c byte) error {
	n := len(b.buf)
	if n == cap(b.buf) {
		b.grow(1)
	}
	b.buf = b.buf[:n+1]
	b.buf[n] = c
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
	b.buf = b.buf[:n+need]
	m := utf8.EncodeRune(b.buf[n:], r)
	b.buf = b.buf[:n+m]
	return m, nil
}

// WriteString appends the contents of s to b's buffer. It returns the
// length of s and a nil error. A string is valid UTF-8, so when the bytes
// before it are, the check that String makes skips it.
func (b *Builder) WriteString(s string) (int, error) {
	n := len(b.buf)
	if cap(b.buf)-n < len(s) {
		b.grow(len(s))
	}
	whole := b.valid == n
	b.buf = b.buf[:n+len(s)]
	copy(b.buf[n:], s)
	if whole {
		b.valid = len(b.buf)
		b.nonASCII = b.nonASCII || !rt.KnownASCII(s)
	}
	return len(s), nil
}
