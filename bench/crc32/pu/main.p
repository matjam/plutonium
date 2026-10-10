// crc32: byte-at-a-time table-driven CRC-32 (IEEE, reflected polynomial
// 0xEDB88320) over an LCG-filled buffer, repeated. One byte of the buffer is
// changed before each pass so the passes cannot be merged.
// Measures uint32 shifts/xors, table lookups and slice passing.
// Prints the CRC of the final pass and an xor of all pass CRCs.
package main

import "pluto/fmt"

const N = 16000000
const Reps = 36

var table [256]uint32
var buf [N]byte

var seed uint64 = 42

func nextRand() int64 {
	seed = seed*6364136223846793005 + 1442695040888963407
	return int64(seed >> 33)
}

func makeTable() {
	for i := 0; i < 256; i++ {
		c := uint32(i)
		for k := 0; k < 8; k++ {
			if c&1 != 0 {
				c = 0xEDB88320 ^ (c >> 1)
			} else {
				c = c >> 1
			}
		}
		table[i] = c
	}
}

func crc32(b []byte) uint32 {
	c := uint32(0xFFFFFFFF)
	n := len(b)
	for i := 0; i < n; i++ {
		c = table[(c^uint32(b[i]))&0xFF] ^ (c >> 8)
	}
	return c ^ 0xFFFFFFFF
}

func main() int64 {
	makeTable()
	for i := 0; i < N; i++ {
		buf[i] = byte(nextRand() >> 23)
	}
	acc := uint32(0)
	last := uint32(0)
	for r := 0; r < Reps; r++ {
		buf[(r*7919)%N] = byte(r)
		last = crc32(buf[:])
		acc ^= last
	}
	fmt.Printf("%d\n", int64(last))
	fmt.Printf("%d\n", int64(acc))
	return 0
}
