package main

func main() {
	for i := 0; i < 3; i++ {
		f := func() int { return i * 10 }
		println(f())
	}
}
