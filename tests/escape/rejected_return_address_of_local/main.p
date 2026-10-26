package main

func f() *int {
	x := 1
	return &x
}
func main() { _ = f() }
