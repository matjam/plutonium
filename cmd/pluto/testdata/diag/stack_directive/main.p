package main

//pu:stack 1M
func helper() int { return 1 }

//pu:stack 2T
func main() {
	println(helper())
}
