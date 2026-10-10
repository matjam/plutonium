// Enums and default parameters in generic code: enums as type arguments
// for comparable, ordered and ~uint8 constraints, their String method
// through a Stringer constraint and through a method value, type switches
// on any(x) with enum cases, local enums in generic functions, and default
// values that use the type parameters of a generic function or method.
package main

type Color enum {
	Red
	Green
	Blue
}

type Level enum int16 {
	Low  = -1
	Mid
	High = 10
}

type Stringer interface{ String() string }

func Name[T Stringer](x T) string { return x.String() }

func NameLater[T Stringer](x T) string {
	f := x.String
	return f()
}

func Index[T comparable](xs []T, v T) int {
	for i, x := range xs {
		if x == v {
			return i
		}
	}
	return -1
}

func Max[T interface{ ~uint8 | ~int16 }](a, b T) T {
	if a > b {
		return a
	}
	return b
}

func Kind[T any](x T) string {
	switch v := any(x).(type) {
	case Color:
		return v.String()
	case Level:
		if v == Level.High {
			return "high level"
		}
		return "level"
	}
	return "other"
}

// Each instance of a generic function has its own copy of a local enum.
func Local[T any](x T) string {
	type Mode enum {
		Off
		On
	}
	type pair struct {
		m Mode
		v T
	}
	p := pair{Mode.On, x}
	return p.m.String()
}

func Fill[T any](xs []T, v T = zero[T]()) int {
	for i := range xs {
		xs[i] = v
	}
	return len(xs)
}

func zero[T any]() T {
	var z T
	return z
}

type Vec[T any] struct {
	items [4]T
	n     int
}

func (v *Vec[T]) Push(x T, times int = 1) {
	for i := 0; i < times; i++ {
		v.items[v.n] = x
		v.n++
	}
}

func (v *Vec[T]) Find[K comparable](k K, key func(T) K, missing int = -1) int {
	for i := 0; i < v.n; i++ {
		if key(v.items[i]) == k {
			return i
		}
	}
	return missing
}

func main() {
	println(Name(Color.Blue), Name(Level.Low), NameLater(Color.Green))
	println(Index([]Color{Color.Red, Color.Blue}, Color.Blue), Index([]Level{Level.Mid}, Level.High))
	println(Max(Color.Red, Color.Blue).String(), Max(Level.Low, Level.Mid).String())
	println(Kind(Color.Green), Kind(Level.High), Kind(Level.Mid), Kind(3))
	println(Local(1), Local("s"))

	var csArr [3]Color
	cs := csArr[:]
	println(Fill(cs, Color.Blue), cs[2].String())
	var nsArr [2]int
	ns := nsArr[:]
	ns[0] = 5
	println(Fill(ns), ns[0])

	var v Vec[Color]
	v.Push(Color.Green)
	v.Push(Color.Blue, 2)
	println(v.n, v.items[2].String())
	byName := func(c Color) string { return c.String() }
	println(v.Find("Blue", byName), v.Find("Red", byName), v.Find("Red", byName, 99))
}
