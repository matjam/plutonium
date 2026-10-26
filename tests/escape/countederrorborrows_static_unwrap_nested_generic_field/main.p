package main
import (
 "pluto/alloc"
 "pluto/errors"
)
type E struct { n int }
func (e *E) Error() string { return "error" }
func makeError() error { return alloc.Synced[E]() }
var sentinel = &E{n:42}
type inner[T interface{Error() string}] struct { p T }
type holder[T interface{Error() string}] struct { inner inner[T] }
type root int
func (root) Error() string { return "root" }
func box[T interface{Error() string}](h holder[T]) error { _ = &h.inner.p; return h.inner.p }
func (root) Unwrap() error { return box(holder[*E]{inner[*E]{sentinel}}) }
func main() { var p *E; errors.As(root(0), &p); println(p.n) }