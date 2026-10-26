package main
import (
 "pluto/alloc"
 "pluto/errors"
)
type E struct { n int }
func (e *E) Error() string { return "error" }
func makeError() error { return alloc.Synced[E]() }
type holder struct { err error }
func keep(e error, h *holder) { h.err = e }
func f() error { e := makeError(); var h holder; keep(e, &h); return h.err }
func main() { _ = f(); _ = errors.Is(nil, nil) }