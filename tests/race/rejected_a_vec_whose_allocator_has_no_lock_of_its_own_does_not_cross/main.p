package main
import (
	"pluto/vec"
	"unsafe"
)

type Bump struct {
	mem [4096]byte
	off uintptr
}

func (b *Bump) Alloc(size, align uintptr) unsafe.Pointer {
	p := unsafe.Pointer(&b.mem[b.off])
	b.off += size
	return p
}

func (b *Bump) Free(p unsafe.Pointer, size, align uintptr) {}

var bump Bump

func produce(out chan<- vec.Vec[int]) {
	v := vec.New[int](&bump)
	v.Push(1)
	out <- v
}

func main() {
	ch := make(chan vec.Vec[int], 1)
	task produce(ch)
	v := <-ch
	println(v.Len())
}