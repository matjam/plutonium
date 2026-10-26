package main

func main() {
	for i := 0; i < 3; i++ {
		x := i
		p := &x
		println(*p)
	}
}
