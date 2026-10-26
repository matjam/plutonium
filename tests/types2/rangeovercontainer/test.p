package main

type Seq2[K, V any] func(yield func(K, V) bool)

type M struct{}

func (*M) All() Seq2[string, float64] { return nil }

type L struct{}

func (L) All() func(func(int) bool) { return nil }

func f(m M, p *M, l L) {
	for k, v := range m {
		var _ string = k
		var _ float64 = v
	}
	for k := range p {
		var _ string = k
	}
	for x := range l {
		var _ int = x
	}
}

func g[C interface{ All() Seq2[int, bool] }](c C) {
	for i, b := range c {
		var _ int = i
		var _ bool = b
	}
}
