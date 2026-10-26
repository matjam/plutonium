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

func f(v *Vec) {
	for x := range v {
		_ = x
	}
}