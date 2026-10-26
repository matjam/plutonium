package main

import "pluto/alloc"
func box[T any](x T) any { return x }
func main() { _, _ = box(1), box(alloc.New[int]()) }
