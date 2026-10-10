package main

func recurse(n int) int {
	var buf [64]int
	buf[n&63] = n
	return recurse(n+1) + buf[(n+1)&63]
}

func main() {
	println(recurse(0))
}
