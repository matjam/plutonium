package main

func f(a, b int = 1) int { return a + b }

func g(a int = 1, b int = 2) int { return a + b }

func main() {
	println(f(1, 2), g())
}
