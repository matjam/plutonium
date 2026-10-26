package main
import (
 "pluto/alloc"
 "pluto/errors"
)
type E struct { n int }
func (e *E) Error() string { return "error" }
func makeError() error { return alloc.Synced[E]() }
type holder struct { p synced[E] }
type root int
func (root) Error() string { return "root" }
func box(h holder) error { _ = &h.p; return h.p }
func (root) Unwrap() error { return box(holder{alloc.Synced[E]()}) }
func main() { var p *E; errors.As(root(0), &p) }