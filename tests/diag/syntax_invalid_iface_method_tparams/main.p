package main

// An interface method with type parameters (E1011).

type Mapper interface {
	Map[T any](x T) T
}

func main() {}
