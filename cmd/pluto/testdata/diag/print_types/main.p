package main

// print and println write booleans, integers, floats, strings and addresses
// (language spec section 13.12). An owner is not printed, even over an
// integer, and neither are types they have no form for.

type Fd owner int32

func (f *Fd) Drop() {}

type Pair struct{ a, b int }

type Count int

func main() {
	var fd Fd
	println(int32(fd), Count(3)) // accepted
	println(fd)                  // an owner over an integer
	println(Pair{1, 2})          // a struct
	s := []int{1}
	println(s)  // a slice
	var e error
	println(e) // an interface
}
