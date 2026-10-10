// fasta-lcg: the random-sequence part of Benchmarks Game fasta. An integer
// LCG drives a float64 cumulative-probability lookup for the IUB (3n chars) and
// Homo sapiens (5n chars) alphabets, wrapped at 60 columns. Instead of printing
// the sequences, every output byte (newlines included) is folded into a hash.
// Measures int64 mod, int-to-float conversion, float compares in a short
// search loop, and byte emission.
package main

import "fmt"

const N = 10000000

const IM = 139968
const IA = 3877
const IC = 29573
const LineLength = 60

var last int64 = 42
var hash uint64
var count int64

var iubChars = [15]byte{'a', 'c', 'g', 't', 'B', 'D', 'H', 'K', 'M', 'N', 'R', 'S', 'V', 'W', 'Y'}
var iubProbs = [15]float64{0.27, 0.12, 0.12, 0.27, 0.02, 0.02, 0.02, 0.02, 0.02, 0.02, 0.02, 0.02, 0.02, 0.02, 0.02}
var homoChars = [4]byte{'a', 'c', 'g', 't'}
var homoProbs = [4]float64{0.3029549426680, 0.1979883004921, 0.1975473066391, 0.3015094502008}

var iubCum [15]float64
var homoCum [4]float64

func random(max float64) float64 {
	last = (last*IA + IC) % IM
	return max * float64(last) / IM
}

func makeCumulative(probs []float64, cum []float64) {
	p := 0.0
	for i := 0; i < len(probs); i++ {
		p += probs[i]
		cum[i] = p
	}
}

func emit(c byte) {
	hash = hash*31 + uint64(c)
	count++
}

func randomFasta(chars []byte, cum []float64, n int64) {
	m := int64(len(cum))
	for n > 0 {
		line := int64(LineLength)
		if n < line {
			line = n
		}
		for i := int64(0); i < line; i++ {
			r := random(1.0)
			c := chars[m-1]
			for j := int64(0); j < m; j++ {
				if r < cum[j] {
					c = chars[j]
					break
				}
			}
			emit(c)
		}
		emit('\n')
		n -= line
	}
}

func main() {
	makeCumulative(iubProbs[:], iubCum[:])
	makeCumulative(homoProbs[:], homoCum[:])
	randomFasta(iubChars[:], iubCum[:], 3*N)
	randomFasta(homoChars[:], homoCum[:], 5*N)
	fmt.Printf("%d\n", count)
	fmt.Printf("%d\n", last)
	fmt.Printf("%d\n", int64(hash%1000000007))
}
