package main
import (
 "pluto/alloc"
 "pluto/errors"
)
type E struct { n int }
func (e *E) Error() string { return "error" }
func makeError() error { return alloc.Synced[E]() }
func f() synced[E] { var e error = alloc.Synced[E](); return e.(synced[E]) }
func main() { _ = f(); _ = errors.Is(nil, nil) }