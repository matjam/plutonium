package main

type E struct{ code int }
func (e *E) Error() string { return "e" }
var errBad = E{1}
func f() error { return &errBad }
func main() { _ = f() }
