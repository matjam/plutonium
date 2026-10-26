package main

type Vec struct{ data []int }

func (v *Vec) Index(i int) int          { return v.data[i] }
func (v *Vec) SetIndex(i int, x int)    { v.data[i] = x }
func (v *Vec) IndexOk(i int) (int, bool) { return 0, false }
func (v *Vec) Len() int                 { return len(v.data) }

type RO struct{}

func (RO) Index(k string) float64 { return 0 }

type WO struct{}

func (*WO) SetIndex(k string, v int) {}

type P struct{ x, y int }

type PV struct{}

func (*PV) Index(i int) P       { return P{} }
func (*PV) SetIndex(i int, p P) {}

type Indexable[K, V any] interface {
	Index(k K) V
	SetIndex(k K, v V)
}

type Ints []int

func (Ints) Index(i int) string { return "" }


func f(v *Vec, r RO, w WO) {
	var u Vec
	a := u[0]
	u[1] = a
	u[2] += 3
	u[3]++
	b, ok := u[4]
	_, _ = b, ok
	c := v[5]
	v[6] = c
	n := len(u) + len(v)
	_ = n
	var x float64 = r["k"]
	_ = x
	w["k"] = 1
	var s Ints
	var e int = s[0] // built-in indexing, not Index
	_ = e
}

func g[M Indexable[K, V], K, V any](m M, k K, v V) V {
	m[k] = v
	return m[k]
}

func h[C interface{ Len() int }](c C) int { return len(c) }

var _ = g[*Vec, int, int]
var _ = h[*Vec]
