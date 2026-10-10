// sieve: sieve of Eratosthenes over a large bool array, repeated.
// Measures byte stores with a stride, branchy scans, and memory bandwidth.
package main

import "pluto/fmt"

const N = 20000000
const Reps = 50

var flags [N]bool

func sieve() int64 {
	for i := int64(0); i < N; i++ {
		flags[i] = true
	}
	flags[0] = false
	flags[1] = false
	for i := int64(2); i*i < N; i++ {
		if flags[i] {
			for j := i * i; j < N; j += i {
				flags[j] = false
			}
		}
	}
	count := int64(0)
	for i := int64(0); i < N; i++ {
		if flags[i] {
			count++
		}
	}
	return count
}

func main() int64 {
	total := int64(0)
	last := int64(0)
	for r := int64(0); r < Reps; r++ {
		last = sieve()
		total += last
	}
	fmt.Printf("%d\n", last)
	fmt.Printf("%d\n", total)
	return 0
}
