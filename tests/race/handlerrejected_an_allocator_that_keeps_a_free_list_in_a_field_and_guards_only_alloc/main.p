package main
import (
	"pluto/alloc"
	"pluto/arch/x86"
	"pluto/rt"
	"unsafe"
)

type pool struct {
	mu   rt.Lock
	free int
	live int
}

func (p *pool) Alloc(size, align uintptr) unsafe.Pointer {
	p.mu.Lock()
	p.free--
	p.mu.Unlock()
	return nil
}

func (p *pool) Free(ptr unsafe.Pointer, size, align uintptr) {
	p.free++
}

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	p := new(int)
	_ = p
}

func main() {
	alloc.SetDefault(&thePool)
	_ = x86.InterruptGate(timer, 0x08, 0)
	q := new(int)
	_ = q
}