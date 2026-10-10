// collatz: longest Collatz chain for starting values below N. Signed int64
// n%2 and n/2 are written as-is so the compiler has to strength-reduce them
// (signed division needs a sign fix-up unless it can prove n > 0).
// Prints the best starting value, its chain length, and the sum of all lengths.
package main

import "fmt"

const N = 10000000

func chainLength(n int64) int64 {
	steps := int64(1)
	for n != 1 {
		if n%2 == 0 {
			n = n / 2
		} else {
			n = 3*n + 1
		}
		steps++
	}
	return steps
}

func main() {
	best := int64(1)
	bestLen := int64(1)
	total := int64(0)
	for i := int64(1); i < N; i++ {
		l := chainLength(i)
		total += l
		if l > bestLen {
			best = i
			bestLen = l
		}
	}
	fmt.Printf("%d\n", best)
	fmt.Printf("%d\n", bestLen)
	fmt.Printf("%d\n", total)
}
