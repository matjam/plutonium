// quicksort: recursive Hoare-partition quicksort (middle pivot) of N
// pseudo-random int64 values from a 64-bit LCG. Measures recursion,
// unpredictable branches, and sequential memory scans.
// Prints a sortedness flag, the median, and a hash of the sorted array.
package main

import "pluto/fmt"

const N = 10000000

var data [N]int64

var seed uint64 = 42

func nextRand() int64 {
	seed = seed*6364136223846793005 + 1442695040888963407
	return int64(seed >> 33)
}

func quicksort(lo int64, hi int64) {
	if lo >= hi {
		return
	}
	pivot := data[lo+(hi-lo)/2]
	i := lo
	j := hi
	for i <= j {
		for data[i] < pivot {
			i++
		}
		for data[j] > pivot {
			j--
		}
		if i <= j {
			t := data[i]
			data[i] = data[j]
			data[j] = t
			i++
			j--
		}
	}
	if lo < j {
		quicksort(lo, j)
	}
	if i < hi {
		quicksort(i, hi)
	}
}

func main() int64 {
	for i := int64(0); i < N; i++ {
		data[i] = nextRand()
	}
	quicksort(0, N-1)
	sorted := int64(1)
	hash := uint64(0)
	for i := int64(0); i < N; i++ {
		if i > 0 && data[i-1] > data[i] {
			sorted = 0
		}
		hash = hash*31 + uint64(data[i])
	}
	fmt.Printf("%d\n", sorted)
	fmt.Printf("%d\n", data[N/2])
	fmt.Printf("%d\n", int64(hash%1000000007))
	return 0
}
