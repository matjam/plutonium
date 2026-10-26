package main

var kept *int
func f() {
	x := 42
	var r ring[*int, 4]
	r.Push(&x)
	for _, v := range r {
		kept = v
	}
}
func main() { f() }
