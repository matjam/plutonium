package race_test

// An allocator's state reached through its receiver is data like any other
// (language spec sections 19.3 and 20.10): when a handler can reach Alloc or
// Free, a plain write to a field of the installed object races with the main
// task's own allocations. Allocators written in unsafe code are the
// programmer's responsibility (section 18.12).

func init() {
	type rej = struct {
		name, src string
		want      []string
	}
	type acc = struct{ name, src string }
	handlerRejected = append(handlerRejected,
		rej{"an allocator with a plain counter in a field that a handler reaches", `
import (
	"pluto/alloc"
	"pluto/arch/x86"
	"unsafe"
)

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
}`, []string{"main.p:13:2: b.used is written here with no lock that guards it", "the interrupt handler timer", "E3502"}},
		rej{"an allocator that keeps a free list in a field and guards only Alloc", `
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
	alloc.SetDefault(&pool{})
	_ = x86.HandlerAddr(timer)
	q := new(int)
	_ = q
}`, []string{"main.p:23:2: p.free is written here with no lock that guards it", "E3502"}},
	)
	handlerAccepted = append(handlerAccepted,
		acc{"an allocator with an atomic counter in a field", `
import (
	"pluto/alloc"
	"pluto/arch/x86"
	"pluto/sync/atomic"
	"unsafe"
)

type bump struct {
	used atomic.Uintptr
}

func (b *bump) Alloc(size, align uintptr) unsafe.Pointer {
	b.used.Add(size)
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
}`},
		acc{"an allocator whose fields an rt.Lock guards at every access", `
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
	_ = x86.HandlerAddr(timer)
	q := new(int)
	_ = q
}`},
		acc{"an allocator with a value receiver and no state", `
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
	_ = x86.HandlerAddr(timer)
	q := new(int)
	_ = q
}`},
		acc{"an allocator written in unsafe code with plain state", `
import (
	"pluto/alloc"
	"pluto/arch/x86"
	"unsafe"
)

type bump struct {
	base unsafe.Pointer
	used uintptr
}

func (b *bump) Alloc(size, align uintptr) unsafe.Pointer {
	p := unsafe.Add(b.base, b.used)
	b.used += size
	return p
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
}`},
		acc{"a handler that never allocates beside an allocator with plain state", `
import (
	"pluto/alloc"
	"pluto/arch/x86"
	"unsafe"
)

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
func timer(f *x86.InterruptFrame) {}

func main() {
	alloc.SetDefault(&heap)
	_ = x86.HandlerAddr(timer)
	q := new(int)
	_ = q
}`},
	)
}
