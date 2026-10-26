package main
import (
 "pluto/alloc"
 "pluto/errors"
)
type E struct { n int }
func (e *E) Error() string { return "error" }
func makeError() error { return alloc.Synced[E]() }
type leaf[T interface{Error() string}] struct { p T }
type inner[T interface{Error() string}] struct { leaf leaf[T] }
type holder[T interface{Error() string}] struct { inner inner[T] }
type root int
func (root) Error() string { return "root" }
func box[T interface{Error() string}](h holder[T]) error { _ = &h.inner.leaf.p; return h.inner.leaf.p }
func (root) Unwrap() error { return box(holder[synced[E]]{inner[synced[E]]{leaf[synced[E]]{alloc.Synced[E]()}}}) }
func main() { var p *E; errors.As(root(0), &p) }