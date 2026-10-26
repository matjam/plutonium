package main
import (
	"pluto/alloc"
	"pluto/arch/x86"
	"unsafe"
)

var next uintptr

type bump struct{}

func (bump) Alloc(size, align uintptr) unsafe.Pointer {
	next += size
	return nil
}

func (bump) Free(p unsafe.Pointer, size, align uintptr) {}

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	p := new(int)
	_ = p
}

func main() {
	alloc.SetDefault(bump{})
	_ = x86.InterruptGate(timer, 0x08, 0)
	q := new(int)
	_ = q
}