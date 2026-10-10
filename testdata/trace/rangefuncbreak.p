package main

// A labelled break out of three loops, through an iterator that ignores
// yield's result (TestMultiBreak1 in Go's rangefunc tests).

type Seq2[T1, T2 any] func(yield func(T1, T2) bool)

func OfSliceIndex[T any, S ~[]T](s S) Seq2[int, T] {
	return func(yield func(int, T) bool) {
		for i, v := range s {
			if !yield(i, v) {
				return
			}
		}
	}
}

func BadOfSliceIndex[T any, S ~[]T](s S) Seq2[int, T] {
	return func(yield func(int, T) bool) {
		for i, v := range s {
			yield(i, v)
		}
	}
}

func main() {
W:
	for _, w := range OfSliceIndex([]int{1000, 2000}) {
		println(w)
		for _, x := range BadOfSliceIndex([]int{100, 200}) {
			for _, y := range OfSliceIndex([]int{10, 20}) {
				println(x, y)
				break W
			}
		}
	}
}
