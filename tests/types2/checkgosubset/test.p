package main

type Pair[K comparable, V any] struct {
	k K
	v V
}

func (p *Pair[K, V]) Key() K { return p.k }

type Shape interface{ Area() float64 }

type Rect struct{ w, h float64 }

func (r *Rect) Area() float64 { return r.w * r.h }

func Max[T ~int | ~int64 | ~float64](a, b T) T {
	if a > b {
		return a
	}
	return b
}

const (
	A = iota
	B
	C = 1 << 62
)

func f() int64 {
	p := Pair[int, string]{k: 1, v: "x"}
	var s Shape = &Rect{2, 3}
	_ = s.Area()
	x := Max(3, 4)
	var arr [4]int
	sl := arr[1:3]
	for i, v := range sl {
		_ = i + v
	}
	return int64(p.Key() + x + C)
}
