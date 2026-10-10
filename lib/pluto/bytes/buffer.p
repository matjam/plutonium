package bytes

import (
	"pluto/alloc"
	"pluto/io"
	"pluto/strings"
	"pluto/unicode/utf8"
	"pluto/vec"
)

// A Buffer is a variable-sized buffer of bytes with Read and Write methods,
// Go's bytes.Buffer: writes grow it, and reads consume from the front. Its
// storage is a vec.Vec[byte] from an allocator, by default the default
// allocator, taken the first time the Buffer grows; Init chooses another.
// The zero Buffer is empty and ready to use.
//
// A Buffer owns its storage, so it is an owner (language spec section 6.4):
// Free (or Drop) returns the storage to its allocator. It must not be
// copied after first use; pass a *Buffer. To write into a caller's array
// without allocating, use a vec.Fixed[byte] and the Append functions
// instead.
type Buffer owner struct {
	buf vec.Vec[byte] // contents are buf[off:]
	off int           // read at buf[off], write at the end
}

// NewBuffer returns a Buffer holding a copy of buf to read, in storage from
// a (by default the default allocator). Unlike Go's NewBuffer, it copies:
// a slice is a view, so the Buffer cannot take over its array.
func NewBuffer(buf []byte, a alloc.Allocator = alloc.Default()) Buffer {
	var b Buffer
	b.buf.Init(a)
	b.buf.Append(buf...)
	return b
}

// NewBufferString returns a Buffer holding a copy of s to read, in storage
// from a (by default the default allocator).
func NewBufferString(s string, a alloc.Allocator = alloc.Default()) Buffer {
	var b Buffer
	b.buf.Init(a)
	vec.AppendString(&b.buf, s)
	return b
}

// Init sets the allocator the Buffer grows from, by default the default
// allocator. It panics if the Buffer already holds storage from another
// allocator.
func (b *Buffer) Init(a alloc.Allocator = alloc.Default()) { b.buf.Init(a) }

// Bytes returns the unread portion of the buffer. The slice aliases the
// buffer content: it is valid only until the next buffer modification.
func (b *Buffer) Bytes() []byte { return b.buf.Slice()[b.off:] }

// String returns the unread portion of the buffer as a new string, a copy
// from the current allocator, as in Go; Bytes views it without copying.
// Bytes that are not valid UTF-8 become U+FFFD (language spec section
// 6.10).
func (b *Buffer) String() string {
	if b == nil {
		// Special case, useful in debugging.
		return "<nil>"
	}
	return strings.FromUTF8Lossy(b.Bytes())
}

// Len returns the number of bytes of the unread portion of the buffer.
func (b *Buffer) Len() int { return b.buf.Len() - b.off }

// Cap returns the capacity of the buffer's storage.
func (b *Buffer) Cap() int { return b.buf.Cap() }

// Available returns how many bytes can be written before the buffer grows.
func (b *Buffer) Available() int { return b.buf.Cap() - b.buf.Len() }

// Reset resets the buffer to be empty, keeping its storage.
func (b *Buffer) Reset() {
	b.buf.Truncate(0)
	b.off = 0
}

// Free returns the buffer's storage to its allocator and leaves it empty.
// The allocator is kept, so the Buffer can be reused.
func (b *Buffer) Free() {
	b.buf.Free()
	b.off = 0
}

// Drop releases the buffer's storage: the owner's release (Free under its
// spec name).
func (b *Buffer) Drop() { b.Free() }

// Truncate discards all but the first n unread bytes from the buffer. It
// panics if n is negative or greater than the length of the buffer.
func (b *Buffer) Truncate(n int) {
	if n == 0 {
		b.Reset()
		return
	}
	if n < 0 || n > b.Len() {
		panic("bytes.Buffer: truncation out of range")
	}
	b.buf.Truncate(b.off + n)
}

// Grow makes room for at least n more bytes without another allocation. It
// panics if n is negative.
func (b *Buffer) Grow(n int) {
	if n < 0 {
		panic("bytes.Buffer.Grow: negative count")
	}
	b.room(n)
}

// room makes space for n more bytes, first by moving the unread bytes to
// the front, and then by growing.
func (b *Buffer) room(n int) {
	if b.buf.Cap()-b.buf.Len() >= n {
		return
	}
	if b.off > 0 {
		s := b.buf.Slice()
		m := copy(s, s[b.off:])
		b.buf.Truncate(m)
		b.off = 0
	}
	b.buf.Reserve(n)
}

// Write appends the contents of p to the buffer, growing it as needed. It
// returns len(p) and a nil error.
func (b *Buffer) Write(p []byte) (n int, err error) {
	b.room(len(p))
	b.buf.Append(p...)
	return len(p), nil
}

// WriteString appends the contents of s to the buffer, growing it as
// needed. It returns len(s) and a nil error.
func (b *Buffer) WriteString(s string) (n int, err error) {
	b.room(len(s))
	vec.AppendString(&b.buf, s)
	return len(s), nil
}

// WriteByte appends the byte c to the buffer, growing it as needed. The
// returned error is always nil.
func (b *Buffer) WriteByte(c byte) error {
	b.room(1)
	b.buf.Push(c)
	return nil
}

// WriteRune appends the UTF-8 encoding of r to the buffer and returns its
// length and a nil error.
func (b *Buffer) WriteRune(r rune) (n int, err error) {
	b.room(utf8.UTFMax)
	m := b.buf.Len()
	utf8.AppendRune(&b.buf, r)
	return b.buf.Len() - m, nil
}

// Read reads the next len(p) bytes from the buffer or until the buffer is
// drained. The return value n is the number of bytes read. If the buffer has
// no data to return, err is io.EOF (unless len(p) is zero).
func (b *Buffer) Read(p []byte) (n int, err error) {
	if b.off >= b.buf.Len() {
		// Buffer is empty, reset to recover space.
		b.Reset()
		if len(p) == 0 {
			return 0, nil
		}
		return 0, io.EOF
	}
	n = copy(p, b.Bytes())
	b.off += n
	return n, nil
}

// ReadByte reads and returns the next byte from the buffer. If no byte is
// available, it returns error io.EOF.
func (b *Buffer) ReadByte() (byte, error) {
	if b.off >= b.buf.Len() {
		b.Reset()
		return 0, io.EOF
	}
	c := b.buf.At(b.off)
	b.off++
	return c, nil
}

// Next returns a slice containing the next n bytes from the buffer,
// advancing the buffer as if the bytes had been returned by Read. If there
// are fewer than n bytes in the buffer, Next returns the entire buffer. The
// slice is only valid until the next call to a read or write method.
func (b *Buffer) Next(n int) []byte {
	m := b.Len()
	if n > m {
		n = m
	}
	data := b.Bytes()[:n]
	b.off += n
	return data
}

// WriteTo writes data to w until the buffer is drained or an error occurs.
func (b *Buffer) WriteTo(w io.Writer) (n int64, err error) {
	if nBytes := b.Len(); nBytes > 0 {
		m, e := w.Write(b.Bytes())
		if m > nBytes {
			panic("bytes.Buffer.WriteTo: invalid Write count")
		}
		b.off += m
		n = int64(m)
		if e != nil {
			return n, e
		}
		if m != nBytes {
			return n, io.ErrShortWrite
		}
	}
	b.Reset()
	return n, nil
}

// ReadFrom reads data from r until io.EOF and appends it to the buffer,
// growing it as needed. It returns the number of bytes read and any error
// other than io.EOF.
func (b *Buffer) ReadFrom(r io.Reader) (n int64, err error) {
	var chunk [512]byte
	for {
		m, e := r.Read(chunk[:])
		if m < 0 {
			panic("bytes.Buffer: reader returned negative count from Read")
		}
		b.Write(chunk[:m])
		n += int64(m)
		if e == io.EOF {
			return n, nil
		}
		if e != nil {
			return n, e
		}
	}
}
