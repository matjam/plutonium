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

//pu:noreturn
func exit(code int)

// onPanic reports a panic on standard error and exits with status 2.
//
//pu:panic
func onPanic(p *rt.Panic) {
	p.Print(rt.Console{})
	exit(2)
}

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

	// The program stands in for several processors by changing the GS base
	// itself, and uses per-CPU variables in ways that need a region where
	// the task cannot move (language spec section 20.9): all of main is one.
	rt.DisablePreemption()
	defer rt.EnablePreemption()

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
	print("cpu0 ")
	print(counter)
	sp()
	print(hist[2])
	sp()
	print(histAt(3))
	sp()
	print(pr.b)
	sp()
	print(flag)
	sp()
	print(sum(big))
	sp()
	print(saved[15])
	nl()
	counter++
	bump(&counter)
	p := &hist[1]
	*p = 42

	useBlock(1)
	print("cpu1 ")
	print(counter)
	sp()
	print(hist[2])
	sp()
	print(histAt(3))
	sp()
	print(pr.b)
	sp()
	print(flag)
	sp()
	print(sum(big))
	nl()
	off := uintptr(unsafe.Pointer(&counter)) - uintptr(unsafe.Pointer(&blocks[1]))
	print("in block 1 ")
	print(off > 0 && off < 512)
	nl()
	// A pointer keeps referring to the copy it was taken from.
	q := &counter
	h := hist[:]

	useBlock(0)
	print("cpu0 again ")
	print(counter)
	sp()
	print(histAt(1))
	sp()
	print(*q)
	sp()
	print(h[2])
	nl()
	return 0
}

// A print statement of several values takes the runtime's print lock, which
// in kernel mode masks interrupts (cli), and this program runs as an
// ordinary process, where cli faults. print of a single value writes
// directly, so the lines are built from those.
func sp() { print(" ") }
func nl() { print("\n") }
