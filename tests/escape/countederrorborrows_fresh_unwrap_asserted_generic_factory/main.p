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
func box[T interface{Error() string}](p T) error {
 if s, ok := any(p).(synced[E]); ok { return s }
 return p
}
func (root) Unwrap() error { return box(alloc.Synced[E]()) }
func main() { var p *E; errors.As(root(0), &p) }