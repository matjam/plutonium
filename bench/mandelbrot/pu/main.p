// mandelbrot: Benchmarks Game mandelbrot (50 iterations, escape radius 2),
// packing 8 pixels per byte exactly as the PBM writer would. Instead of writing
// the bitmap, prints the number of set bits and a hash of the packed bytes.
// Measures scalar float64 loops with a data-dependent early exit.
package main

import "pluto/fmt"

const N = 5000

var limit float64 = 2.0

var hash uint64
var setBits int64

func emit(b int64) {
	hash = hash*31 + uint64(b)
	for k := 0; k < 8; k++ {
		setBits += (b >> k) & 1
	}
}

func main() int64 {
	w := int64(N)
	h := int64(N)
	iter := 50
	for y := int64(0); y < h; y++ {
		byteAcc := int64(0)
		bitNum := int64(0)
		ci := 2*float64(y)/float64(h) - 1.0
		for x := int64(0); x < w; x++ {
			cr := 2*float64(x)/float64(w) - 1.5
			zr := 0.0
			zi := 0.0
			tr := 0.0
			ti := 0.0
			for i := 0; i < iter && tr+ti <= limit*limit; i++ {
				zi = 2*zr*zi + ci
				zr = tr - ti + cr
				tr = zr * zr
				ti = zi * zi
			}
			byteAcc <<= 1
			if tr+ti <= limit*limit {
				byteAcc |= 1
			}
			bitNum++
			if bitNum == 8 {
				emit(byteAcc)
				byteAcc = 0
				bitNum = 0
			} else if x == w-1 {
				byteAcc <<= 8 - w%8
				emit(byteAcc)
				byteAcc = 0
				bitNum = 0
			}
		}
	}
	fmt.Printf("%d\n", setBits)
	fmt.Printf("%d\n", int64(hash%1000000007))
	return 0
}
