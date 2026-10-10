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

// EOF is returned by Read when no more input is available.
var EOF = &errors.Text{Msg: "EOF"}

// ErrShortWrite means a write accepted fewer bytes than requested.
var ErrShortWrite = &errors.Text{Msg: "short write"}

// WriteString writes s to w.
func WriteString(w Writer, s string) (int, error) {
	if len(s) == 0 {
		return 0, nil
	}
	return w.Write(unsafeBytes(s))
}

// ReadFull reads exactly len(buf) bytes from r.
func ReadFull(r Reader, buf []byte) (int, error) {
	n := 0
	for n < len(buf) {
		m, err := r.Read(buf[n:])
		n += m
		if err != nil {
			return n, err
		}
		if m == 0 {
			return n, EOF
		}
	}
	return n, nil
}
