package compat

import (
	"pluto/fmt"
	"pluto/unicode/utf8"
	"unsafe"
)

// A Buffer is Go's bytes.Buffer and strings.Builder for writing: its zero
// value is ready to use and grows from alloc.Default(). It implements
// io.Writer.
type Buffer struct {
	buf []byte
}

// Builder is strings.Builder.
type Builder = Buffer

// Write appends p.
func (b *Buffer) Write(p []byte) (int, error) {
	b.buf = Append(b.buf, p...)
	return len(p), nil
}

// WriteString appends s.
func (b *Buffer) WriteString(s string) (int, error) {
	b.buf = AppendString(b.buf, s)
	return len(s), nil
}

// WriteByte appends c.
func (b *Buffer) WriteByte(c byte) error {
	b.buf = Append(b.buf, c)
	return nil
}

// WriteRune appends the UTF-8 encoding of r.
func (b *Buffer) WriteRune(r rune) (int, error) {
	var tmp [utf8.UTFMax]byte
	n := utf8.EncodeRune(tmp[:], r)
	b.buf = Append(b.buf, tmp[:n]...)
	return n, nil
}

// Len returns the number of bytes written.
func (b *Buffer) Len() int { return len(b.buf) }

// Bytes returns the contents, a view valid until the next write.
func (b *Buffer) Bytes() []byte { return b.buf }

// String returns the contents. Unlike Bytes it stays valid after later
// writes: a write that grows the buffer copies it, leaving the old bytes
// in place, and writes never change bytes already written.
func (b *Buffer) String() string {
	if len(b.buf) == 0 {
		return ""
	}
	return unsafe.String(unsafe.SliceData(b.buf), len(b.buf))
}

// Truncate discards all but the first n bytes.
func (b *Buffer) Truncate(n int) { b.buf = b.buf[:n] }

// Reset empties the buffer, keeping its storage.
func (b *Buffer) Reset() { b.buf = b.buf[:0] }

// Grow makes room for n more bytes.
func (b *Buffer) Grow(n int) {
	if len(b.buf)+n > cap(b.buf) {
		b.buf = grow(b.buf, len(b.buf)+n)
	}
}

// Sprintf formats into a new string from alloc.Default().
func Sprintf(format string, args ...any) string {
	var b Buffer
	fmt.Fprintf(&b, format, args...)
	return b.String()
}

// Sprint formats with %v into a new string.
func Sprint(args ...any) string {
	var b Buffer
	fmt.Fprint(&b, args...)
	return b.String()
}

// Sprintln formats with %v, spaces and a newline into a new string.
func Sprintln(args ...any) string {
	var b Buffer
	fmt.Fprintln(&b, args...)
	return b.String()
}

// An Error is an error with a message and an optional wrapped error, made
// by Errorf.
type Error struct {
	Msg  string
	Wrap error
}

func (e *Error) Error() string { return e.Msg }

// Unwrap returns the error that %w wrapped, or nil.
func (e *Error) Unwrap() error { return e.Wrap }

// Errorf is fmt.Errorf: it formats a message (%w as %v) and keeps the first
// %w operand for Unwrap. The error lives in alloc.Default().
func Errorf(format string, args ...any) error {
	var f Buffer
	var wrap error
	argi := 0
	for i := 0; i < len(format); i++ {
		c := format[i]
		if c != '%' || i+1 >= len(format) {
			f.WriteByte(c)
			continue
		}
		// Copy the directive, counting the arguments it consumes.
		f.WriteByte(c)
		i++
		for i < len(format) && isFlag(format[i]) {
			if format[i] == '*' {
				argi++
			}
			f.WriteByte(format[i])
			i++
		}
		if i >= len(format) {
			break
		}
		v := format[i]
		if v == 'w' {
			if wrap == nil && argi < len(args) {
				wrap, _ = args[argi].(error)
			}
			v = 'v'
		}
		f.WriteByte(v)
		if v != '%' {
			argi++
		}
	}
	e := new(Error)
	e.Msg = Sprintf(f.String(), args...)
	e.Wrap = wrap
	return e
}

func isFlag(c byte) bool {
	return c == '+' || c == '-' || c == '#' || c == ' ' || c == '0' || c == '.' || c == '*' || '1' <= c && c <= '9'
}

// Unwrap returns the error err wraps, or nil.
func Unwrap(err error) error {
	if e, ok := err.(*Error); ok {
		return e.Wrap
	}
	return nil
}

// Is reports whether err or any error it wraps is target.
func Is(err, target error) bool {
	for err != nil {
		if err == target {
			return true
		}
		err = Unwrap(err)
	}
	return false
}
