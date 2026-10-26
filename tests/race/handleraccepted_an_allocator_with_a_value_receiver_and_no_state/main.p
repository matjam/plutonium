package main
import (
	"pluto/alloc"
	"pluto/arch/x86"
	"unsafe"
)

type none struct{}

func (none) Alloc(size, align uintptr) unsafe.Pointer { return nil }

func (none) Free(p unsafe.Pointer, size, align uintptr) {}

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	p := new(int)
	_ = p
}

func main() {
	alloc.SetDefault(none{})
	_ = x86.InterruptGate(timer, 0x08, 0)
	q := new(int)
	_ = q
}