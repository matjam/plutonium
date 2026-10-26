package main

type File struct{ fd int32 }

func (f *File) Drop() {}
