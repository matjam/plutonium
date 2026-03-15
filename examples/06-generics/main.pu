package main

// Demonstrates compile-time monomorphization of generic functions.
// Max[int64] is expanded to a concrete Max__int64 function at compile time.
//
// Build: pluto build -o gen ./examples/06-generics/main.pu
// Run:   ./gen; echo $?
// Expected output: 42

func Max[T int64](a, b T) T {
	if a > b {
		return a
	}
	return b
}

func main() int64 {
	return Max[int64](42, 17)
}
