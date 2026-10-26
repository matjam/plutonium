package main
import (
 "pluto/alloc"
 "pluto/errors"
)
type E struct { n int }
func (e *E) Error() string { return "error" }
func makeError() error { return alloc.Synced[E]() }
type inner struct { p synced[E] }
type holder struct { inner inner }
type root int
func (root) Error() string { return "root" }
func box(h holder) error { _ = &h.inner.p; return h.inner.p }
func (root) Unwrap() error { return box(holder{inner{alloc.Synced[E]()}}) }
func main() { var p *E; errors.As(root(0), &p) }