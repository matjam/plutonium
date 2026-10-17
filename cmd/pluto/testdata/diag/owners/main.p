package main

// Where owner may and must appear (language spec sections 6.4, 6.9).

type File struct{ fd int32 }

func (f *File) Drop() {}

type Buf owner struct{ b []byte }

type Conn struct {
	name string
	buf  Buf
}

type Bytes owner []byte

type Kind owner enum {
	A
	B
}

type Closer owner struct{ fd int32 }

func (c *Closer) Drop() error { return nil }

type Handle owner struct{ fd int32 }

func (h Handle) Drop() {}

func Fill[T any](s []T, v T) {
	for i := range s {
		s[i] = v
	}
}

func main() {
	var bufs [2]Buf
	Fill(bufs[:], Buf{})
}
