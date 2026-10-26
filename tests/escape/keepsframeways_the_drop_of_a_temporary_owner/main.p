package main

type O owner struct{ n int }
var keep *O
func (o *O) Drop() { keep = o }
func mk() O { return O{n: 1} }
func f() int { return mk().n }
func main() { println(f()) }
