package main

var kept *int

//pu:noinline
func f() {
	var r ring[*int, 4]
	x := 42
	r.Push(&x)
	for _, v := range r {
		kept = v
	}
}

//pu:noinline
func g() {
	var r ring[*int, 4]
	x := 42
	r.Push(&x)
	for v := range r.Values() {
		kept = v
	}
}

func main() {
	f()
	g()
	println(kept != nil)
}
