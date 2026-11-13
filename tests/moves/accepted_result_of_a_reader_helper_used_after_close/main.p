package main

// ReadAll's results are a slice of static memory and an error that the
// Read methods return. File.Read waits on the file's poller, and one
// poller's wait asserts on a value it loads through its receiver: that
// assertion yields what the poller holds, not what ReadAll's caller lent
// (the C2 ruling on section 18.3), so ReadAll returns no view of f.
// Closing f after the call does not end data or err.

import (
	"pluto/io"
	"pluto/vec"
)

var buf [64]byte

type poller interface{ wait() int }

type pipe struct{ v any }

func (p *pipe) wait() int {
	if q, ok := p.v.(*pipe); ok && q != p {
		return 1
	}
	return 0
}

type File owner struct {
	raw  vec.Vec[byte]
	poll poller
}

func (f *File) Read(p []byte) (int, error) {
	if f.poll.wait() > 0 {
		return 0, nil
	}
	return 0, io.EOF
}

func (f *File) Close() error {
	f.raw = vec.New[byte]()
	return nil
}

func ReadAll(r io.Reader) ([]byte, error) {
	n, err := r.Read(buf[:])
	if err == io.EOF {
		return buf[:n], nil
	}
	return buf[:n], err
}

var p0 pipe

func main() {
	p0.v = &p0
	f := File{raw: vec.New[byte](), poll: &p0}
	f.raw.Push(1)
	data, err := ReadAll(&f)
	_ = f.Close()
	println(len(data), err == nil)
}
