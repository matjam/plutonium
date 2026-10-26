package main

var r ring[*int, 4]
func main() {
	x := 1
	r.Push(&x)
}
