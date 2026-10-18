package main

import (
	"pluto/alloc"
	"pluto/arch/x86"
	"pluto/rt"
	"unsafe"
)

//pu:panic
func onPanic(p *rt.Panic) {
	p.Print(rt.Console{})
	for {
		x86.Cli()
		x86.Hlt()
	}
}

// bump keeps its counter in the installed object, which the interrupt handler
// and the main task both reach through Alloc.
type bump struct {
	used uintptr
}

func (b *bump) Alloc(size, align uintptr) unsafe.Pointer {
	b.used += size
	return nil
}

func (b *bump) Free(p unsafe.Pointer, size, align uintptr) {}

var heap bump

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	p := new(int)
	_ = p
}

func main() {
	alloc.SetDefault(&heap)
	_ = x86.HandlerAddr(timer)
	q := new(int)
	_ = q
}
