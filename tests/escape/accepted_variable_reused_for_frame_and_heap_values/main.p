package main

import "pluto/alloc"
type E struct{ code int }
func (e *E) Error() string { return "e" }
var saved error
func main() {
	var local E
	var err error = &local
	_ = err.Error()
	err = alloc.New[E]()
	saved = err
}
