package main

type none struct{}
func (none) Error() string { return "none" }
func f() error { return none{} }
func main() { _ = f() }
