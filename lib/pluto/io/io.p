// Package io provides basic I/O interfaces.
package io

import "pluto/errors"

// A Writer writes bytes.
type Writer interface {
	Write(p []byte) (n int, err error)
}

// A Reader reads bytes.
type Reader interface {
	Read(p []byte) (n int, err error)
}

// A ReadWriter is both.
type ReadWriter interface {
	Reader
	Writer
}

// A Closer can be closed.
type Closer interface {
	Close() error
}

// Whence values for Seek.
const (
	SeekStart   = 0 // relative to the start of the file
	SeekCurrent = 1 // relative to the current offset
	SeekEnd     = 2 // relative to the end
)

// EOF is the error returned by Read when no more input is available.
// Functions return EOF itself, never wrapped, so callers may test for it
// with ==.
var EOF = errors.New("EOF")

// ErrUnexpectedEOF means that EOF was encountered in the middle of reading
// a fixed-size block or data structure.
var ErrUnexpectedEOF = errors.New("unexpected EOF")

// ErrShortWrite means a write accepted fewer bytes than requested.
var ErrShortWrite = errors.New("short write")

// ErrShortBuffer means that a read required a longer buffer than was
// provided.
var ErrShortBuffer = errors.New("short buffer")

// WriteString writes s to w.
func WriteString(w Writer, s string) (int, error) {
	if len(s) == 0 {
		return 0, nil
	}
	return w.Write(unsafeBytes(s))
}

// ReadAtLeast reads from r into buf until it has read at least min bytes.
// It returns the number of bytes copied and an error if fewer bytes were
// read. The error is EOF only if no bytes were read. If an EOF happens after
// reading fewer than min bytes, ReadAtLeast returns ErrUnexpectedEOF. If min
// is greater than the length of buf, ReadAtLeast returns ErrShortBuffer.
func ReadAtLeast(r Reader, buf []byte, min int) (n int, err error) {
	if len(buf) < min {
		return 0, ErrShortBuffer
	}
	for n < min && err == nil {
		var nn int
		nn, err = r.Read(buf[n:])
		n += nn
		if nn == 0 && err == nil {
			// A Reader that returns neither data nor an error would loop
			// forever; treat it as the end of input.
			err = EOF
		}
	}
	if n >= min {
		err = nil
	} else if n > 0 && err == EOF {
		err = ErrUnexpectedEOF
	}
	return n, err
}

// ReadFull reads exactly len(buf) bytes from r. The error is EOF only if no
// bytes were read; ErrUnexpectedEOF if some but not all were.
func ReadFull(r Reader, buf []byte) (int, error) {
	return ReadAtLeast(r, buf, len(buf))
}
