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
func (root) Unwrap() error { h := alloc.Synced[holder](); h.p = alloc.Synced[E](); return h.p }
func main() { var p *E; errors.As(root(0), &p) }