package main

// Unlike the reader helper whose poller asserts on a value loaded through
// its receiver, this ReadAll asserts on its own parameter, so its result
// is a view of the file lent to it (the C2 ruling on section 18.3).
// Closing the file gives its buffer a new value, which ends the view.

import (
	"pluto/io"
	"pluto/vec"
)

type File owner struct {
	raw vec.Vec[byte]
}

func (f *File) Read(p []byte) (int, error) { return 0, io.EOF }

func (f *File) Close() error {
	f.raw = vec.New[byte]()
	return nil
}

func ReadAll(r io.Reader) ([]byte, error) {
	if f, ok := r.(*File); ok {
		return f.raw.Slice(), nil
	}
	return nil, io.EOF
}

func main() {
	f := File{raw: vec.New[byte]()}
	f.raw.Push(1)
	data, err := ReadAll(&f)
	_ = f.Close()
	println(len(data), err == nil)
}
