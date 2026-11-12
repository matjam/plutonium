package main

// take of a value rather than a pointer to a place (E3208).

func main() {
	x := 1
	y := take(x)
	println(y)
}
