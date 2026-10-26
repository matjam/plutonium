package main

func put(fs []func() int, i int, f func() int) { fs[i] = f }
func main() {
	var fs [3]func() int
	for i := 0; i < 3; i++ {
		f := func() int { return i * 10 }
		put(fs[:], i, f)
	}
	println(fs[0]())
}
