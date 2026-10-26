package main

func main() {
	box := new([4]*int)
	s := box[:]
	x := 1
	src := [1]*int{&x}
	copy(s, src[:])
}
