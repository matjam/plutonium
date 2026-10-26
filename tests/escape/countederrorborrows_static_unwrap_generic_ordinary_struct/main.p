package main
import (
 "pluto/alloc"
 "pluto/errors"
)
type E struct { n int }
func (e *E) Error() string { return "error" }
func makeError() error { return alloc.Synced[E]() }
var sentinel = &E{n:42}
type ordinary struct { p *E }
func (o ordinary) Error() string { return "ordinary" }
type root int
func (root) Error() string { return "root" }
func box[T interface{Error() string}](p T) error { return p }
func (root) Unwrap() error { return box(ordinary{sentinel}) }
func main() { var p ordinary; errors.As(root(0), &p); println(p.p.n) }