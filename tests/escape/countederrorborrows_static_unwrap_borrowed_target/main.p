package main
import (
 "pluto/alloc"
 "pluto/errors"
)
type E struct { n int }
func (e *E) Error() string { return "error" }
func makeError() error { return alloc.Synced[E]() }
var sentinel = &E{n:42}
type root int
func (root) Error() string { return "root" }
func (root) Unwrap() error { return sentinel }
func main() { var p *E; errors.As(root(0), &p); println(p.n) }