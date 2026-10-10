package main

// All is a closure constructor on a generic type, called for two
// instantiations: each one's closure is named after its instance of All. The
// string instance panics inside the closure.
type Bag[T any] struct {
	items []T
	limit int
}

func (b *Bag[T]) All() func(yield func(int, T) bool) {
	return func(yield func(int, T) bool) {
		for i := 0; i < b.limit; i++ {
			if !yield(i, b.items[i]) {
				return
			}
		}
	}
}

func main() {
	ints := &Bag[int]{items: []int{7, 8}, limit: 2}
	for i, x := range ints.All() {
		println(i, x)
	}
	words := &Bag[string]{items: []string{"a", "b"}, limit: 3}
	for i, w := range words.All() {
		println(i, w)
	}
}
