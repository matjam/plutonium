package main
func id[T any](x T) T { return x }
var _ = id(2i)