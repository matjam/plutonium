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
func box(p []synced[E]) error { return p[0] }
func (root) Unwrap() error { p := [1]synced[E]{alloc.Synced[E]()}; return box(p[:]) }
func main() { var p *E; errors.As(root(0), &p) }