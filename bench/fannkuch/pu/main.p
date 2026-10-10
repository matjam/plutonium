// fannkuch: Benchmarks Game fannkuch-redux (sequential permutation walk).
// Measures small fixed-size array shuffling and tight integer loops.
// Prints the checksum and the maximum number of flips.
package main

import "pluto/fmt"

const N = 11

func fannkuch(n int64, maxFlipsOut *int64) int64 {
	var perm [16]int64
	var perm1 [16]int64
	var count [16]int64
	for i := int64(0); i < n; i++ {
		perm1[i] = i
	}
	maxFlips := int64(0)
	checksum := int64(0)
	permCount := int64(0)
	r := n
	for {
		for r != 1 {
			count[r-1] = r
			r--
		}
		for i := int64(0); i < n; i++ {
			perm[i] = perm1[i]
		}
		flips := int64(0)
		for k := perm[0]; k != 0; k = perm[0] {
			i := int64(0)
			j := k
			for i < j {
				t := perm[i]
				perm[i] = perm[j]
				perm[j] = t
				i++
				j--
			}
			flips++
		}
		if flips > maxFlips {
			maxFlips = flips
		}
		if permCount%2 == 0 {
			checksum += flips
		} else {
			checksum -= flips
		}
		// Generate the next permutation.
		for {
			if r == n {
				*maxFlipsOut = maxFlips
				return checksum
			}
			perm0 := perm1[0]
			for i := int64(0); i < r; i++ {
				perm1[i] = perm1[i+1]
			}
			perm1[r] = perm0
			count[r]--
			if count[r] > 0 {
				break
			}
			r++
		}
		permCount++
	}
}

func main() int64 {
	maxFlips := int64(0)
	checksum := fannkuch(N, &maxFlips)
	fmt.Printf("%d\n", checksum)
	fmt.Printf("%d\n", maxFlips)
	return 0
}
