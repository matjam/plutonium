package main

import "unsafe"

type File owner struct{ fd int32 }

func (f *File) Drop() {}

type Frame owner uintptr
type Pages owner [4]Frame
type Raw owner unsafe.Pointer
type Box owner *int
type Resource owner interface{ Read(p []byte) (int, error) }
type Conn owner struct {
	file File
	name string
}
type Moves owner struct{ n int } // owner without Drop: it still moves

type Vec[T owner any] owner struct{ data []T }

type Plain struct {
	n int
	p *File // a view of a File: not an owner
}

func Fill[T any](s []T, v T) {
	for i := range s {
		s[i] = v
	}
}

func use() {
	var v Vec[File]
	_ = v
	Fill([]int{1}, 2)
}

// "owner" is a keyword only before a type; elsewhere it is a name.
type owner int
type Count owner
type Pkg owner

func id[T owner, U owner any](x T, u U) (T, U) {
	var owner T = x
	return owner, u
}
