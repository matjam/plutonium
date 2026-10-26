package main
import (
	"pluto/alloc"
	"pluto/arch/x86"
	"pluto/rt"
	"unsafe"
)

type bump struct {
	mu   rt.Lock
	used uintptr
}

func (b *bump) Alloc(size, align uintptr) unsafe.Pointer {
	b.mu.Lock()
	b.used += size
	b.mu.Unlock()
	return nil
}

func (b *bump) Free(p unsafe.Pointer, size, align uintptr) {
	b.mu.Lock()
	b.used -= size
	b.mu.Unlock()
}

var heap bump

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	p := new(int)
	_ = p
}

func main() {
	alloc.SetDefault(&heap)
	_ = x86.InterruptGate(timer, 0x08, 0)
	q := new(int)
	_ = q
}