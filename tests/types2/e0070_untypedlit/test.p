package main

type outer struct{
	inner struct { i int }
}

var _ = outer{inner: {1}}

func main() {}
