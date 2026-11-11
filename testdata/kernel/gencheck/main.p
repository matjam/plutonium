// A kernel-mode kernel built with generation checks, to test them under
// QEMU. It boots with testdata/kernel/smoke's boot code, which the test
// copies next to it, and the default kernel linker script, which links the
// kernel in the top 2 GiB: its heap, a static array served by an
// alloc.Slab, has upper-half addresses. The runtime's metadata comes from
// another static array, through the function the kernel exports as
// pluto_gen_metadata. The test also writes scenario.p, which sets the
// scenario constant:
//
//   - live: heap pointers carry tags, and checks of live memory pass,
//     including in an interrupt handler that the metadata source enters
//     while the runtime holds the directory's writer lock. The kernel
//     leaves QEMU with status 0.
//   - stale: language spec section 18.11's example, a view into a vector
//     reached through an *Index that grows. The panic handler reports a
//     use of freed memory and leaves QEMU with status 3.
package main

import (
	"pluto/alloc"
	"pluto/arch/x86"
	"pluto/rt"
	"pluto/vec"
	"unsafe"
)

// Implemented in boot_amd64.s.
func percpuImage() uintptr
func percpuSize() uintptr

// softInt raises interrupt 0x40 (int_amd64.s).
func softInt()

const com1 = 0x3F8

func serialWrite(p unsafe.Pointer, n int) {
	for i := 0; i < n; i++ {
		for x86.Inb(com1+5)&0x20 == 0 {
			x86.Pause()
		}
		x86.Outb(com1, *(*uint8)(unsafe.Add(p, i)))
	}
}

// onPanic prints the panic and leaves QEMU with status 3.
//
//pu:panic
func onPanic(p *rt.Panic) {
	p.Print(rt.Console{})
	for {
		x86.Outl(0xf4, 3)
		x86.Cli()
		x86.Hlt()
	}
}

// ----------------------------------------------------------------------------
// Memory: heap pages and metadata from static arrays, never returned.

var heap aligned[[8 << 20]byte, 4096]

// bumpPages hands out the heap's pages in order, from heapNext. Every
// party allocates through the default allocator, so heapLock guards
// heapNext.
type bumpPages struct{}

var (
	heapNext uintptr
	heapLock rt.Lock
)

func (bumpPages) AllocPages(n uintptr) unsafe.Pointer {
	base := uintptr(unsafe.Pointer(&heap))
	heapLock.Lock()
	if heapNext+n*alloc.PageSize > uintptr(len(heap)) {
		heapLock.Unlock()
		return nil
	}
	p := unsafe.Pointer(base + heapNext)
	heapNext += n * alloc.PageSize
	heapLock.Unlock()
	return p
}

func (bumpPages) FreePages(p unsafe.Pointer, n uintptr) {}

var metadata aligned[[4 << 20]byte, 4096]

var metadataUsed uintptr

// metadataCalls counts the calls of genMetadata; armed makes the next one
// raise interrupt 0x40.
var (
	metadataCalls int
	armed         bool
)

// genMetadata is the runtime's source of generation metadata memory. The
// runtime calls it with the directory's writer lock held, and interrupts
// disabled by that lock, which a software interrupt ignores.
//
//pu:export pluto_gen_metadata
func genMetadata(n uintptr) uintptr {
	metadataCalls++
	if armed {
		armed = false
		softInt()
	}
	if metadataUsed+n > uintptr(len(metadata)) {
		return 0
	}
	p := uintptr(unsafe.Pointer(&metadata)) + metadataUsed
	metadataUsed += n
	return p
}

// ----------------------------------------------------------------------------
// Interrupts.

type node struct {
	val  int
	next *node
}

// probe is a live heap object that the interrupt handler reads, and
// handlerSaw what it read; probeLock guards both.
var (
	probe      *node
	handlerSaw int
	probeLock  rt.Lock
)

// check reads probe through a checked pointer. If a check took the
// directory's writer lock, this handler, entered while the lock is held,
// would wait for it for good.
//
//pu:interrupt
func check(f *x86.InterruptFrame) {
	probeLock.Lock()
	handlerSaw = probe.next.val
	probeLock.Unlock()
}

var idt [256]x86.Gate

var idtr x86.DescriptorTablePointer

// block is processor 0's per-CPU block: a copy of the .percpu image whose
// first word is its own address.
var block [64]uint64

// ----------------------------------------------------------------------------
// Section 18.11's example.

type Index owner struct {
	words vec.Vec[string]
}

func (ix *Index) Add(w string) { ix.words.Push(w) }

//pu:noinline
func stale(ix *Index) {
	first := &ix.words.Slice()[0] // ix is a *Index; words is a vec.Vec[string]
	for i := 0; i < 100; i++ {
		ix.Add("x") // grows words and frees the old array
	}
	println(*first) // panic: use of freed memory
}

// tagged reports whether p carries a tag: its bits differ from its address.
func tagged(p unsafe.Pointer) bool {
	return *(*uintptr)(unsafe.Pointer(&p)) != uintptr(p)
}

func main() int64 {
	rt.SetConsole(serialWrite)
	b := unsafe.Pointer(&block)
	image := percpuImage()
	for k := uintptr(0); k < percpuSize(); k++ {
		*(*byte)(unsafe.Add(b, k)) = *(*byte)(unsafe.Pointer(image + k))
	}
	block[0] = uint64(uintptr(b))
	x86.WriteGSBase(uintptr(b))
	rt.SetCPU(0)
	idt[0x40] = x86.InterruptGate(check, 0x08, 0)
	idtr = x86.NewDescriptorTablePointer(uintptr(unsafe.Pointer(&idt)), len(idt)*16)
	x86.Lidt(&idtr)
	alloc.SetDefault(alloc.NewSlab(bumpPages{}))

	println("scenario", scenario)
	switch scenario {
	case "live":
		a := alloc.New[node]()
		a.val = 7
		second := alloc.New[node]()
		second.val = 42
		a.next = second
		probeLock.Lock()
		probe = a
		probeLock.Unlock()
		p := unsafe.Pointer(a)
		println("upper half", uintptr(p) >= 0xFFFF800000000000, "tagged", tagged(p))
		sum := 0
		for n := a; n != nil; n = n.next {
			sum += n.val
		}
		println("sum", sum)
		// A large object's tag table is more than the runtime's pool
		// holds, so registering it asks the source for memory.
		calls := metadataCalls
		armed = true
		big := alloc.New[[1 << 20]byte]()
		big[len(big)-1] = 1
		probeLock.Lock()
		saw := handlerSaw
		probeLock.Unlock()
		println("source called", metadataCalls > calls, "handler saw", saw)
		alloc.Free(big)
		alloc.Free(a.next)
		alloc.Free(a)
		println("metadata used", metadataUsed > 0)
	case "stale":
		var ix Index
		ix.Add("first")
		stale(&ix)
	}
	return 0
}
