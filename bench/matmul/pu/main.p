// matmul: naive i-j-k triple-loop float64 matrix multiply of two NxN
// row-major matrices. The inner loop is a dot product with a strided read of
// B, so it measures float add latency, bounds-check hoisting and cache misses.
// Prints the sum of C and one element, each as int64(x * 1000).
package main

import "pluto/fmt"

const N = 1200

var a [N * N]float64
var b [N * N]float64
var c [N * N]float64

func main() int64 {
	n := int64(N)
	for i := int64(0); i < n; i++ {
		for j := int64(0); j < n; j++ {
			a[i*n+j] = float64((i*31+j*17)%97) / 97.0
			b[i*n+j] = float64((i*13+j*7)%89) / 89.0
		}
	}
	for i := int64(0); i < n; i++ {
		for j := int64(0); j < n; j++ {
			sum := 0.0
			for k := int64(0); k < n; k++ {
				sum += a[i*n+k] * b[k*n+j]
			}
			c[i*n+j] = sum
		}
	}
	total := 0.0
	for i := int64(0); i < n*n; i++ {
		total += c[i]
	}
	fmt.Printf("%d\n", int64(total*1000))
	fmt.Printf("%d\n", int64(c[(n/2)*n+n/3]*1000))
	return 0
}
