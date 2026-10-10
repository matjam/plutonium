package bytes

import (
	"pluto/errors"
	"pluto/io"
	"pluto/unicode/utf8"
)

// ErrFull is returned by Buffer writes that do not fit in the buffer.
var ErrFull = errors.New("bytes.Buffer: buffer full")

// A Buffer is a variable-sized buffer of bytes with Read and Write methods,
// like Go's bytes.Buffer, except that its storage is a fixed slice the
// caller provides and it never grows: a write that does not fit stores as
// much as fits and returns ErrFull. Reading consumes from the front; the
// space read is reclaimed when the buffer becomes empty or on Reset.
//
// The zero Buffer has no storage. Create one with NewBuffer.
type Buffer struct {
	buf []byte // contents are buf[off:len(buf)], storage is buf[:cap(buf)]
	off int    // read at buf[off], write at buf[len(buf)]
}

// NewBuffer returns an empty Buffer that stores its contents in buf's
// capacity. The caller must not use buf afterwards.
func NewBuffer(buf []byte) Buffer {
	return Buffer{buf: buf[:0]}
}

// NewBufferString returns a Buffer whose storage is buf's capacity, holding
// a copy of s to read. It panics if s does not fit.
func NewBufferString(buf []byte, s string) Buffer {
	b := Buffer{buf: buf[:0]}
	if len(s) > cap(buf) {
		panic("bytes.NewBufferString: string larger than buffer")
	}
	b.buf = append(b.buf, s...)
	return b
}

// Bytes returns the unread portion of the buffer. The slice aliases the
// buffer content: it is valid only until the next buffer modification.
func (b *Buffer) Bytes() []byte { return b.buf[b.off:] }

// String returns the unread portion of the buffer as a new string, a copy
// from the current allocator, as in Go; Bytes views it without copying.
// Bytes that are not valid UTF-8 become U+FFFD (language spec section
// 6.10).
func (b *Buffer) String() string {
	if b == nil {
		// Special case, useful in debugging.
		return "<nil>"
	}
	return string(b.buf[b.off:])
}

// Len returns the number of bytes of the unread portion of the buffer.
func (b *Buffer) Len() int { return len(b.buf) - b.off }

// Cap returns the capacity of the buffer's underlying storage.
func (b *Buffer) Cap() int { return cap(b.buf) }

// Available returns how many bytes can be written before the buffer is
// full.
func (b *Buffer) Available() int { return cap(b.buf) - len(b.buf) + b.off }

// Reset resets the buffer to be empty, keeping its storage.
func (b *Buffer) Reset() {
	b.buf = b.buf[:0]
	b.off = 0
}

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
	b.buf = b.buf[:b.off+n]
}

// room makes space for n more bytes if possible, moving unread data to the
// front, and returns how many of them fit.
func (b *Buffer) room(n int) int {
	free := cap(b.buf) - len(b.buf)
	if free >= n {
		return n
	}
	if b.off > 0 {
		m := copy(b.buf[:cap(b.buf)], b.buf[b.off:])
		b.buf = b.buf[:m]
		b.off = 0
		free = cap(b.buf) - m
	}
	return min(free, n)
}

// Write appends the contents of p to the buffer. If p does not fit, Write
// stores as much as fits and returns the count with ErrFull.
func (b *Buffer) Write(p []byte) (n int, err error) {
	m := b.room(len(p))
	b.buf = append(b.buf, p[:m]...)
	if m < len(p) {
		return m, ErrFull
	}
	return m, nil
}

// WriteString appends the contents of s to the buffer, like Write.
func (b *Buffer) WriteString(s string) (n int, err error) {
	m := b.room(len(s))
	b.buf = append(b.buf, s[:m]...)
	if m < len(s) {
		return m, ErrFull
	}
	return m, nil
}

// WriteByte appends the byte c to the buffer, or returns ErrFull.
func (b *Buffer) WriteByte(c byte) error {
	if b.room(1) < 1 {
		return ErrFull
	}
	b.buf = append(b.buf, c)
	return nil
}

// WriteRune appends the UTF-8 encoding of r to the buffer and returns its
// length. If the encoding does not fit, nothing is written and the error is
// ErrFull.
func (b *Buffer) WriteRune(r rune) (n int, err error) {
	need := utf8.RuneLen(r)
	if need < 0 {
		need = 3 // the encoding of utf8.RuneError
	}
	if b.room(need) < need {
		return 0, ErrFull
	}
	m := len(b.buf)
	b.buf = utf8.AppendRune(b.buf, r)
	return len(b.buf) - m, nil
}

// Read reads the next len(p) bytes from the buffer or until the buffer is
// drained. The return value n is the number of bytes read. If the buffer has
// no data to return, err is io.EOF (unless len(p) is zero).
func (b *Buffer) Read(p []byte) (n int, err error) {
	if b.off >= len(b.buf) {
		// Buffer is empty, reset to recover space.
		b.Reset()
		if len(p) == 0 {
			return 0, nil
		}
		return 0, io.EOF
	}
	n = copy(p, b.buf[b.off:])
	b.off += n
	return n, nil
}

// ReadByte reads and returns the next byte from the buffer. If no byte is
// available, it returns error io.EOF.
func (b *Buffer) ReadByte() (byte, error) {
	if b.off >= len(b.buf) {
		b.Reset()
		return 0, io.EOF
	}
	c := b.buf[b.off]
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
	data := b.buf[b.off : b.off+n]
	b.off += n
	return data
}

// WriteTo writes data to w until the buffer is drained or an error occurs.
func (b *Buffer) WriteTo(w io.Writer) (n int64, err error) {
	if nBytes := b.Len(); nBytes > 0 {
		m, e := w.Write(b.buf[b.off:])
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
