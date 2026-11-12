package main

type I interface {
	m()
}
var t I
var _ = t.(type)

func main() {}
