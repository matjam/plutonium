// sha256: hand-written SHA-256 (FIPS 180-4, including padding) over an
// LCG-filled buffer, repeated. Each digest is written back into the start of
// the buffer, so every pass depends on the previous one.
// Measures uint32 rotates/shifts/adds with high register pressure.
// Prints the eight words of the final digest.
package main

import "fmt"

const N = 1000003
const Reps = 350

var k = [64]uint32{
	0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
	0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
	0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
	0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
	0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
	0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
	0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
	0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
}

var hs [8]uint32
var buf [N]byte

var seed uint64 = 42

func nextRand() int64 {
	seed = seed*6364136223846793005 + 1442695040888963407
	return int64(seed >> 33)
}

func rotr(x uint32, n uint32) uint32 {
	return (x >> n) | (x << (32 - n))
}

// compress runs the compression function over every 64-byte block of p.
func compress(p []byte) {
	var w [64]uint32
	n := len(p)
	for off := 0; off+64 <= n; off += 64 {
		for t := 0; t < 16; t++ {
			j := off + t*4
			w[t] = uint32(p[j])<<24 | uint32(p[j+1])<<16 | uint32(p[j+2])<<8 | uint32(p[j+3])
		}
		for t := 16; t < 64; t++ {
			s0 := rotr(w[t-15], 7) ^ rotr(w[t-15], 18) ^ (w[t-15] >> 3)
			s1 := rotr(w[t-2], 17) ^ rotr(w[t-2], 19) ^ (w[t-2] >> 10)
			w[t] = w[t-16] + s0 + w[t-7] + s1
		}
		a := hs[0]
		b := hs[1]
		c := hs[2]
		d := hs[3]
		e := hs[4]
		f := hs[5]
		g := hs[6]
		h := hs[7]
		for t := 0; t < 64; t++ {
			bigS1 := rotr(e, 6) ^ rotr(e, 11) ^ rotr(e, 25)
			ch := (e & f) ^ (^e & g)
			t1 := h + bigS1 + ch + k[t] + w[t]
			bigS0 := rotr(a, 2) ^ rotr(a, 13) ^ rotr(a, 22)
			maj := (a & b) ^ (a & c) ^ (b & c)
			t2 := bigS0 + maj
			h = g
			g = f
			f = e
			e = d + t1
			d = c
			c = b
			b = a
			a = t1 + t2
		}
		hs[0] += a
		hs[1] += b
		hs[2] += c
		hs[3] += d
		hs[4] += e
		hs[5] += f
		hs[6] += g
		hs[7] += h
	}
}

// sha256 hashes data, leaving the digest in hs.
func sha256(data []byte) {
	hs[0] = 0x6a09e667
	hs[1] = 0xbb67ae85
	hs[2] = 0x3c6ef372
	hs[3] = 0xa54ff53a
	hs[4] = 0x510e527f
	hs[5] = 0x9b05688c
	hs[6] = 0x1f83d9ab
	hs[7] = 0x5be0cd19
	n := len(data)
	full := n - n%64
	compress(data[:full])

	var pad [128]byte
	rem := n - full
	for i := 0; i < rem; i++ {
		pad[i] = data[full+i]
	}
	pad[rem] = 0x80
	padLen := 64
	if rem >= 56 {
		padLen = 128
	}
	bitLen := uint64(n) * 8
	for i := 0; i < 8; i++ {
		pad[padLen-1-i] = byte(bitLen >> (8 * i))
	}
	compress(pad[:padLen])
}

func main() {
	for i := 0; i < N; i++ {
		buf[i] = byte(nextRand() >> 23)
	}
	for r := 0; r < Reps; r++ {
		sha256(buf[:])
		for i := 0; i < 8; i++ {
			buf[4*i] = byte(hs[i] >> 24)
			buf[4*i+1] = byte(hs[i] >> 16)
			buf[4*i+2] = byte(hs[i] >> 8)
			buf[4*i+3] = byte(hs[i])
		}
	}
	for i := 0; i < 8; i++ {
		fmt.Printf("%d\n", int64(hs[i]))
	}
}
