package main

func ratio(a, b int) int {
	return a / b
}

func main() {
	d := 0
	for i := 0; i < 3; i++ {
		d += i - 1
	}
	println(ratio(10, d))
}
