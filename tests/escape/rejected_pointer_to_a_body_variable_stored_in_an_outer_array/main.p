package main

func main() {
	var ps [3]*int
	for i := 0; i < 3; i++ {
		x := i
		ps[i] = &x
	}
	println(*ps[0])
}
