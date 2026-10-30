// Generic code over [T owner any] sees T as an owner in every instance
// (section 8.7), so a container method that copies an element, such as At,
// is an error even in an instance with a value type.
package main

import "pluto/vec"

func First[T owner any](v *vec.Vec[T]) T {
	return v.At(0)
}

func main() {
	v := vec.New[int]()
	v.Push(3)
	println(First(&v))
}
