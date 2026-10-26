package main

type S struct{ x int }

func (s S) Apply[U any](f func(int) U) U { return f(s.x) }

func (s *S) Id[U any]() int { return s.x }

type G[T any] struct{ v T }

func (g G[T]) With[U any](u U) (T, U) { return g.v, u }

type I interface{ Id() int }

func f() {
	var s S
	_ = s.Apply[string](func(int) string { return "" })
	_ = s.Apply(func(int) bool { return true })
	_, _ = G[int]{}.With(1.5)
	var h func(func(int) int) int = s.Apply
	_ = h
	m := (*S).Id[bool]
	_ = m
}
