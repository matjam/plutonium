package main

// Bit-exact results of the math package over special values and random
// inputs, folded into one FNV-1a digest per function so the expected output
// stays small. The digests match Go compiling the same source.

import (
	"pluto/fmt"
	"pluto/math"
)

var seed uint64 = 7

func rnd() uint64 {
	seed = seed*6364136223846793005 + 1442695040888963407
	return seed
}

const (
	fExp = iota
	fExp2
	fExpm1
	fLog
	fLog2
	fLog10
	fLog1p
	fSin
	fCos
	fTan
	fAsin
	fAcos
	fAtan
	fSinh
	fCosh
	fTanh
	fCbrt
	fNextUp
	fNextDown
	fPow
	fAtan2
	fHypot
	fMax
	fMin
	fDim
	fRemainder
	fNextafter
	fFMA
	nFuncs
)

var names = [nFuncs]string{"Exp", "Exp2", "Expm1", "Log", "Log2", "Log10", "Log1p", "Sin", "Cos", "Tan",
	"Asin", "Acos", "Atan", "Sinh", "Cosh", "Tanh", "Cbrt", "NextUp", "NextDown",
	"Pow", "Atan2", "Hypot", "Max", "Min", "Dim", "Remainder", "Nextafter", "FMA"}

var digests [nFuncs]uint64

func fold(f int, r float64) {
	h := digests[f]
	if h == 0 {
		h = 14695981039346656037
	}
	b := math.Float64bits(r)
	for i := 0; i < 8; i++ {
		h ^= b & 0xff
		h *= 1099511628211
		b >>= 8
	}
	digests[f] = h
}

func main() {
	negz := math.Copysign(0, -1)
	specials := []float64{0, negz, 1, -1, 0.5, -0.5, 2, -2, 3, 10, -10, 0.1, 1e-300, -1e-300, 5e-324, 1e300, -1e300, 709.7, 710, -745, -746, 1023.9, 1024, -1074, -1075,
		math.Pi, math.Pi / 2, math.Pi / 4, -math.Pi, 1e10, 1e20, 1e300 * 1e8, 0.9999999, 1.0000001,
		math.Inf(1), math.Inf(-1), math.NaN(), math.MaxFloat64, math.SmallestNonzeroFloat64, 2.5e-308, 1e15 + 0.3, 1 << 53, 4503599627370495.5}
	var buf [400]float64
	n := copy(buf[:], specials)
	for i := 0; i < 300; i++ {
		var x float64
		switch i % 4 {
		case 0:
			x = float64(int64(rnd()>>11)) / float64(1<<53) * 4 // [0,4)
		case 1:
			x = (float64(int64(rnd()>>11))/float64(1<<53) - 0.5) * 100
		case 2:
			x = math.Float64frombits(rnd()) // any bits
		case 3:
			x = (float64(int64(rnd()>>11))/float64(1<<53) - 0.5) * 2 // (-1,1)
		}
		buf[n] = x
		n++
	}
	xs := buf[:n]
	for _, x := range xs {
		fold(fExp, math.Exp(x))
		fold(fExp2, math.Exp2(x))
		fold(fExpm1, math.Expm1(x))
		fold(fLog, math.Log(x))
		fold(fLog2, math.Log2(x))
		fold(fLog10, math.Log10(x))
		fold(fLog1p, math.Log1p(x))
		fold(fSin, math.Sin(x))
		fold(fCos, math.Cos(x))
		fold(fTan, math.Tan(x))
		fold(fAsin, math.Asin(x))
		fold(fAcos, math.Acos(x))
		fold(fAtan, math.Atan(x))
		fold(fSinh, math.Sinh(x))
		fold(fCosh, math.Cosh(x))
		fold(fTanh, math.Tanh(x))
		fold(fCbrt, math.Cbrt(x))
		fold(fNextUp, math.Nextafter(x, math.Inf(1)))
		fold(fNextDown, math.Nextafter(x, math.Inf(-1)))
	}
	for i := 0; i < len(xs); i += 3 {
		for j := 0; j < len(xs); j += 7 {
			x, y := xs[i], xs[j]
			fold(fPow, math.Pow(x, y))
			fold(fAtan2, math.Atan2(x, y))
			fold(fHypot, math.Hypot(x, y))
			fold(fMax, math.Max(x, y))
			fold(fMin, math.Min(x, y))
			fold(fDim, math.Dim(x, y))
			fold(fRemainder, math.Remainder(x, y))
			fold(fNextafter, math.Nextafter(x, y))
			fold(fFMA, math.FMA(x, y, xs[(i+j)%len(xs)]))
		}
	}
	for f := 0; f < nFuncs; f++ {
		fmt.Printf("%s %016x\n", names[f], digests[f])
	}
}
