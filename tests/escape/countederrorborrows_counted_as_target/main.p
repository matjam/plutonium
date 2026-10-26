package main
import (
 "pluto/alloc"
 "pluto/errors"
)
type E struct { n int }
func (e *E) Error() string { return "error" }
func makeError() error { return alloc.Synced[E]() }
func f() error { e := makeError(); var other error; errors.As(e, &other); return other }
func main() { _ = f(); _ = errors.Is(nil, nil) }