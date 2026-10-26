package main

var kept []int
func keep(xs ...int) { kept = xs }
func main() { task keep(1, 2) }
