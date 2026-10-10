package main

type Vec struct{ X, Y float64 }

func scale(v *Vec) { v.X = v.X * 2 }

func main() {
	var v Vec
	scale(&v)
}
