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
func as[T any](err error, target *T) bool {
 if e, ok := any(err).(T); ok { *target = e; return true }
 if x, ok := err.(interface{Unwrap() error}); ok { return as(x.Unwrap(), target) }
 return false
}
func main() { var p *E; as(root(0), &p); _ = errors.Is(nil, nil) }