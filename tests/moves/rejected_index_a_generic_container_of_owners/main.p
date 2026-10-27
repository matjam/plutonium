package main

type Buf owner struct{ n int }

func (b *Buf) Len() int { return b.n }

type Bag[T owner any] owner struct{ data [4]T }

func (b *Bag[T]) Index(i int) T { return take(&b.data[i]) }

func newBag[T owner any]() Bag[T] { return Bag[T]{} }

// The type arguments Buf and Bag[Buf] are types, not reads. bag[0] reads an
// element of a container of owners, which would be a copy.
func main() {
	bag := newBag[Buf]()
	nested := newBag[Bag[Buf]]()
	_ = nested
	b := bag[0]
	println(b.Len())
}
