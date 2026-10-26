package main

func main() {
	var f func() int
	for i := 0; i < 3; i++ {
		i := i
		f = func() int { return i }
	}
	println(f())
}
