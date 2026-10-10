// A kernel-mode program that runs as an ordinary Linux process to test
// per-CPU variables: it points the GS base at one of two "per-CPU blocks"
// with arch_prctl, as a kernel would with wrmsr, and checks that each
// block holds its own copy of every per-CPU variable.
package main

import (
	"pluto/rt"
	"unsafe"
)

// Implemented in start_amd64.s.
func setGS(base uintptr)

//pu:noescape
func write(p unsafe.Pointer, n int)

type pair struct {
	a, b int32
}

//pu:percpu
var counter int64

//pu:percpu
var hist [4]uint32

//pu:percpu
var pr pair

//pu:percpu
var flag bool

//pu:percpu
var big [16]int64

// Two per-CPU blocks. Their first word is the block's own address, which
// taking the address of a per-CPU variable reads.
var blocks [2][64]uint64

func useBlock(i int) {
	b := &blocks[i]
	b[0] = uint64(uintptr(unsafe.Pointer(b)))
	setGS(uintptr(unsafe.Pointer(b)))
}

//pu:noinline
func bump(p *int64) { *p += 100 }

//pu:noinline
func histAt(i int) uint32 { return hist[i] }

func sum(a [16]int64) int64 {
	t := int64(0)
	for _, v := range a {
		t += v
	}
	return t
}

func main() int64 {
	rt.SetConsole(write)

	useBlock(0)
	counter = 10
	hist[2] = 7
	pr.b = -3
	flag = true
	for i := range big {
		big[i] = int64(i)
	}

	useBlock(1)
	counter = 20
	hist[2] = 9
	hist[3] = 4
	bump(&counter)
	saved := big
	big = [16]int64{}
	big[15] = 1

	useBlock(0)
	println("cpu0", counter, hist[2], histAt(3), pr.b, flag, sum(big), saved[15])
	counter++
	bump(&counter)
	p := &hist[1]
	*p = 42

	useBlock(1)
	println("cpu1", counter, hist[2], histAt(3), pr.b, flag, sum(big))
	off := uintptr(unsafe.Pointer(&counter)) - uintptr(unsafe.Pointer(&blocks[1]))
	println("in block 1", off > 0 && off < 512)
	// A pointer keeps referring to the copy it was taken from.
	q := &counter
	h := hist[:]

	useBlock(0)
	println("cpu0 again", counter, histAt(1), *q, h[2])
	return 0
}
