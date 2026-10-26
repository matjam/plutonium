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
func (root) Unwrap() error { return makeError() }
func main() { var p *E; as := errors.As[*E]; as(root(0), &p) }