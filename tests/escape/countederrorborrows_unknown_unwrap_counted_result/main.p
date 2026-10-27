package main
import (
 "pluto/alloc"
 "pluto/errors"
)
type E struct { n int }
func (e *E) Error() string { return "error" }
func makeError() error { return alloc.Synced[E]() }
type root int
func (root) Error() string { return "root" }
func child() error
func (root) Unwrap() error { return child() }
func main() { var p *E; errors.As(root(0), &p) }