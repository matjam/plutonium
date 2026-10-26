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
func (root) Unwrap() error { e := makeError(); p := &e; return *p }
func main() { var p *E; errors.As(root(0), &p) }