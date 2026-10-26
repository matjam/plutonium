package main

type W struct{ p *int }
func (W) Error() string { return "w" }
func f() error {
	x := 1
	return W{&x}
}
func main() { _ = f() }
