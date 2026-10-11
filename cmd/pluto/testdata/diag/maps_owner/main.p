package main

// Maps cannot hold owners (language spec section 7.10): reading a map
// copies its keys and elements.

type File owner struct{ fd int }

func (f *File) Drop() {}

type byFile map[File]string

func main() {
	open := map[string]File{}
	_ = open
	var keys byFile
	_ = keys
	shared := map[string]synced[File]{} // allowed: counted references copy
	_ = shared
}
