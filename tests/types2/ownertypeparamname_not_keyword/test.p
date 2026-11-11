package main

type File owner struct{ fd int32 }

func (f *File) Drop() {}

// Here owner names the type parameter, so it does not admit owner types
// (language spec section 4.6).
type Box[owner any] struct{ v owner }

var b Box[File]
