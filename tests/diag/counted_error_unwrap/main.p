package main

import (
	"pluto/alloc"
	"pluto/errors"
)

type detail struct{ n int }

func (*detail) Error() string { return "detail" }

type root int

func (root) Error() string { return "root" }
func (root) Unwrap() error { return alloc.Synced[detail]() }

func main() {
	var p *detail
	errors.As(root(0), &p)
	println(p.n)
}
