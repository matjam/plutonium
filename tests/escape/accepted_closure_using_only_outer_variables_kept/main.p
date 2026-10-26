package main

func main() {
	n := 5
	var f func() int
	for i := 0; i < 3; i++ {
		f = func() int { return n }
	}
	println(f())
}
