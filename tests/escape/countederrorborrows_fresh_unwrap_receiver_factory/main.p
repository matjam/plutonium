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
func (h holder) boxed() error { _ = &h.p; return h.p }
func (root) Unwrap() error { return holder{alloc.Synced[E]()}.boxed() }
func main() { var p *E; errors.As(root(0), &p) }