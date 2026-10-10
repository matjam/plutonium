// A kernel built with --mode kernel and the default linker script. It runs
// with SSE disabled (boot_amd64.s leaves CR4.OSFXSR clear, so an SSE
// instruction anywhere would fault), prints over the first serial port
// with x86.Outb (println and pluto/fmt), sets up two per-CPU blocks from the .percpu image and
// switches between them with WriteGSBase and Swapgs, then enables SSE for
// one //pu:fpu call bracketed by FPUBegin and FPUEnd.
package main

import (
	"pluto/arch/x86"
	"pluto/fmt"
	"pluto/rt"
	"unsafe"
)

// Implemented in boot_amd64.s.
func percpuImage() uintptr
func percpuSize() uintptr

const com1 = 0x3F8

func serialWrite(p unsafe.Pointer, n int) {
	for i := 0; i < n; i++ {
		for x86.Inb(com1+5)&0x20 == 0 {
			x86.Pause()
		}
		x86.Outb(com1, *(*uint8)(unsafe.Add(p, i)))
	}
}

//pu:percpu
var cpuID int = -1

//pu:percpu
var ticks = [4]uint32{7, 0, 0, 9}

// Two per-CPU blocks, each a copy of the .percpu image whose first word
// is the block's own address.
var blocks [2][64]uint64

func initBlock(i int) uintptr {
	b := unsafe.Pointer(&blocks[i])
	image := percpuImage()
	for k := uintptr(0); k < percpuSize(); k++ {
		*(*byte)(unsafe.Add(b, k)) = *(*byte)(unsafe.Pointer(image + k))
	}
	*(*uintptr)(b) = uintptr(b)
	return uintptr(b)
}

type vec struct{ X, Y float64 }

type pair struct{ A, B int32 }

var v = vec{1.5, 2}

var fpuState x86.FPUState

//pu:fpu
//pu:noinline
func scale(p *vec, k int) {
	p.X = p.X*float64(k) + p.Y
}

// describe formats a float with pluto/fmt, whose kernel build prints
// floats as their bits.
//
//pu:fpu
//pu:noinline
func describe(p *vec, buf []byte) []byte {
	return fmt.Bprintf(buf, "fmt float %v\n", p.X)
}

func main() int64 {
	rt.SetConsole(serialWrite)
	println("hello from kernel mode")
	var buf [64]byte
	out := fmt.Bprintf(buf[:0], "fmt %d %s %#x %v\n", -42, "works", 255, true)
	serialWrite(unsafe.Pointer(&out[0]), len(out))
	// Composite values; kernel builds leave field names out of type
	// descriptors, so %+v prints none.
	out = fmt.Bprintf(buf[:0], "fmt %v %+v %v\n", pair{1, -2}, pair{3, 4}, []pair{{5, 6}})
	serialWrite(unsafe.Pointer(&out[0]), len(out))

	b0, b1 := initBlock(0), initBlock(1)
	x86.WriteGSBase(b0)
	println("cpu0 initial", cpuID, ticks[0], ticks[3])
	cpuID = 0
	ticks[1] = 11
	x86.Wrmsr(x86.KernelGSBaseMSR, uint64(b1))
	x86.Swapgs()
	println("cpu1 initial", cpuID, ticks[1], x86.ReadGSBase() == b1)
	cpuID = 1
	ticks[1]++
	p := &ticks[1]
	x86.Swapgs()
	println("cpu0 again", cpuID, ticks[1], *p, x86.ReadGSBase() == b0)

	flags := x86.SaveFlagsCli()
	x86.RestoreFlags(flags)
	_, ebx, _, _ := x86.Cpuid(0, 0)
	cr4 := x86.ReadCR4()
	println("cpuid", ebx != 0, "sse off", cr4&(1<<9) == 0)

	// Enable SSE (OSFXSR, OSXMMEXCPT) for the FPU region.
	x86.WriteCR4(cr4 | 1<<9 | 1<<10)
	x86.FPUBegin(&fpuState)
	scale(&v, 4)
	out = describe(&v, buf[:0])
	x86.FPUEnd(&fpuState)
	serialWrite(unsafe.Pointer(&out[0]), len(out))
	println("fpu result bits", *(*uint64)(unsafe.Pointer(&v.X)))
	return 0
}
