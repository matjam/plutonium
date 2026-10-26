package main

var F func()
func main() {
	x := 1
	F = func() { x++ }
}
