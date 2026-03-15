package main

// Demonstrates pointer operations and pass-by-reference.
// Increments a value through a pointer.
//
// Build: pluto build -o ptrs ./examples/04-pointers/main.pu
// Run:   ./ptrs; echo $?
// Expected output: 11

func inc(p *int64) {
	*p = *p + 1
}

func main() int64 {
	x := int64(10)
	inc(&x)
	return x
}
