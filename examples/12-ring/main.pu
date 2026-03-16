package main

import "pluto/fmt"

func main() int64 {
	var r ring[int64, 8]
	r.Push(10)
	r.Push(20)
	r.Push(30)
	n := r.Len()
	fmt.Printf("len=%d\n", n)
	v1 := r.Pop()
	fmt.Printf("pop=%d\n", v1)
	v2 := r.Pop()
	fmt.Printf("pop=%d\n", v2)
	n2 := r.Len()
	fmt.Printf("len=%d\n", n2)
	return 0
}
