package main
import (
 "pluto/alloc"
 "pluto/errors"
)
type E struct { n int }
func (e *E) Error() string { return "error" }
func makeError() error { return alloc.Synced[E]() }
func f(e error) *E { var p *E; errors.As(e, &p); return p }
func main() { e := makeError(); _ = f(e) }