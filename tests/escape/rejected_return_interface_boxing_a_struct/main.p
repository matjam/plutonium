package main

type E struct{ code, line int }
func (e E) Error() string { return "e" }
func f() error { return E{1, 2} }
func main() { _ = f() }
