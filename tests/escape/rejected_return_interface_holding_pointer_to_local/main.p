package main

type E struct{ code int }
func (e *E) Error() string { return "e" }
func f() error {
	var e E
	return &e
}
func main() { _ = f() }
