package main
import (
 "pluto/alloc"
 "pluto/errors"
)
type E struct { n int }
func (e *E) Error() string { return "error" }
func makeError() error { return alloc.Synced[E]() }
type root struct { p synced[E] }
func (r *root) Error() string { return "root" }
func (r *root) Unwrap() error { return r.p }
func main() { r := alloc.Synced[root](); r.p = alloc.Synced[E](); var e error = r; var p *E; errors.As(e, &p); println(p.n) }