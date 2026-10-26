package main
import (
 "pluto/alloc"
 "pluto/errors"
)
type E struct { n int }
func (e *E) Error() string { return "error" }
func makeError() error { return alloc.Synced[E]() }
func f() *E { e := makeError(); var p *E; as := errors.As[*E]; as(e, &p); return p }
func main() { _ = f() }