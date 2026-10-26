package main

type S struct{ x int }

func (s S) Apply[U any](f func(int) U) U { return f(s.x) }

func (s *S) Id[U any]() int { return s.x }

type G[T any] struct{ v T }

func (g G[T]) With[U any](u U) (T, U) { return g.v, u }

type I interface{ Id() int }
var _ = S{}.Apply