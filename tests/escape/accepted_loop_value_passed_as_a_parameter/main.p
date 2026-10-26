package main

func main() {
	var f func(int) int
	for i := 0; i < 3; i++ {
		f = func(i int) int { return i * 10 }
		println(f(i))
	}
}
