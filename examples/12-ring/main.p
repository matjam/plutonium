package main

import "pluto/fmt"

// ring[T, N]: a fixed-capacity ring buffer stored inline.

func main() {
	var r ring[int64, 4]
	for i := int64(1); i <= 5; i++ {
		if !r.Push(i * 10) {
			fmt.Printf("full at %d\n", i*10)
		}
	}
	fmt.Printf("len=%d cap=%d full=%t\n", r.Len(), r.Cap(), r.Full())
	if v, ok := r.Peek(); ok {
		fmt.Printf("peek=%d\n", v)
	}
	for !r.Empty() {
		v, _ := r.Pop()
		fmt.Printf("pop=%d\n", v)
	}
	_, ok := r.Pop()
	fmt.Printf("pop from empty: ok=%t\n", ok)
}
