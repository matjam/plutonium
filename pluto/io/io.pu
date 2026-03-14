package io

// Reader is the interface for reading bytes.
// Implementations must not allocate; they may write to a caller-supplied buffer.
type Reader interface {
	Read(p []byte) (n int, err error)
}

// Writer is the interface for writing bytes.
type Writer interface {
	Write(p []byte) (n int, err error)
}

// ReadWriter is the interface for types that implement both Read and Write.
type ReadWriter interface {
	Reader
	Writer
}

// EOF is the error returned when no more data is available.
// It is not a real error; callers should check for it explicitly.
type EOFError struct{}

func (e *EOFError) Error() string { return "EOF" }

var EOF = &EOFError{}
