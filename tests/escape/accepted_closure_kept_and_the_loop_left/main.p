package main

func main() {
	var f func() int
	for i := 0; i < 3; i++ {
		if i == 1 {
			f = func() int { return i }
			break
		}
	}
	println(f())
}
