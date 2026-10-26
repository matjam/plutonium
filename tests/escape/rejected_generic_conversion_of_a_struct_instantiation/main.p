package main

type P struct{ x, y int }
func box[T any](x T) any { return x }
func main() { _, _ = box(1), box(P{1, 2}) }
