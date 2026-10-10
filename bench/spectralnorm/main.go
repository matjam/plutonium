// spectralnorm: Benchmarks Game spectral-norm (power method on an infinite
// matrix). Measures float64 division/multiply-add in tight nested loops and
// slice passing. Prints int64(result * 1e9).
package main

import (
	"fmt"
	"math"
)

const N = 5500

var u [N]float64
var v [N]float64
var tmp [N]float64

func evalA(i, j int64) float64 {
	return 1.0 / float64((i+j)*(i+j+1)/2+i+1)
}

func multiplyAv(v []float64, av []float64) {
	n := int64(len(v))
	for i := int64(0); i < n; i++ {
		sum := 0.0
		for j := int64(0); j < n; j++ {
			sum += evalA(i, j) * v[j]
		}
		av[i] = sum
	}
}

func multiplyAtv(v []float64, atv []float64) {
	n := int64(len(v))
	for i := int64(0); i < n; i++ {
		sum := 0.0
		for j := int64(0); j < n; j++ {
			sum += evalA(j, i) * v[j]
		}
		atv[i] = sum
	}
}

func multiplyAtAv(v []float64, tmp []float64, atav []float64) {
	multiplyAv(v, tmp)
	multiplyAtv(tmp, atav)
}

func main() {
	for i := 0; i < N; i++ {
		u[i] = 1.0
	}
	for i := 0; i < 10; i++ {
		multiplyAtAv(u[:], tmp[:], v[:])
		multiplyAtAv(v[:], tmp[:], u[:])
	}
	vBv := 0.0
	vv := 0.0
	for i := 0; i < N; i++ {
		vBv += u[i] * v[i]
		vv += v[i] * v[i]
	}
	fmt.Printf("%d\n", int64(math.Sqrt(vBv/vv)*1e9))
}
