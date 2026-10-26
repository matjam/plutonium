package main

type E struct{ a, b int }
func (E) Error() string { return "e" }
var Err error
func main() {
	Err = E{1, 2}
}
