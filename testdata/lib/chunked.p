package main

// arena.Chunked: a growable arena that takes chunks from a parent
// allocator, with Mark/Release, Reset, Trim and Drop, used through
// alloc.Allocator by new, make, vec.Vec and hash.Map.

import (
	"pluto/alloc"
	"pluto/arena"
	"pluto/hash"
	"pluto/vec"
	"unsafe"
)

// counting passes allocations to another allocator and counts the blocks
// and bytes outstanding.
type counting struct {
	under         alloc.Allocator
	blocks, bytes int
}

func (c *counting) Alloc(size, align uintptr) unsafe.Pointer {
	c.blocks++
	c.bytes += int(size)
	return c.under.Alloc(size, align)
}

func (c *counting) Free(p unsafe.Pointer, size, align uintptr) {
	c.blocks--
	c.bytes -= int(size)
	c.under.Free(p, size, align)
}

type node struct {
	next *node
	val  int
	pad  [6]int
}

func main() {
	parent := counting{under: alloc.Default()}
	a := arena.NewChunked(1024, &parent)
	println("empty", a.Used(), a.Held(), parent.blocks)

	// Fill several chunks with linked nodes (64 bytes each).
	var head *node
	for i := 0; i < 100; i++ {
		n := new(node, &a)
		n.val = i
		n.next = head
		head = n
	}
	sum := 0
	for n := head; n != nil; n = n.next {
		sum += n.val
	}
	println("nodes", sum, "chunks", parent.blocks, "used", a.Used() >= 6400)

	// Alignment is honoured across chunks.
	for _, al := range []uintptr{1, 8, 16, 64, 256} {
		p := a.Alloc(24, al)
		if uintptr(p)%al != 0 {
			println("misaligned", al)
		}
	}

	// Scratch work between a mark and its release.
	m := a.Mark()
	usedAtMark := a.Used()
	chunksAtMark := parent.blocks
	scratch := make([]int, 0, 2000, &a) // 16000 bytes: a chunk of its own
	for i := 0; i < 2000; i++ {
		scratch = append(scratch, i)
	}
	for i := 0; i < 50; i++ {
		_ = new(node, &a)
	}
	println("scratch", scratch[1999], "more chunks", parent.blocks > chunksAtMark)
	bytesBefore := parent.bytes
	blocksBefore := parent.blocks
	a.Release(m)
	// The 16000-byte chunk goes back to the parent; the standard ones stay
	// as spares.
	println("released", a.Used() == usedAtMark, "large chunk returned", bytesBefore-parent.bytes >= 16000, blocksBefore-parent.blocks)
	// The nodes allocated before the mark are intact.
	sum = 0
	for n := head; n != nil; n = n.next {
		sum += n.val
	}
	println("nodes after release", sum)

	// Spare chunks are reused: allocating the same scratch again takes no
	// standard chunks from the parent.
	before := parent.blocks
	for i := 0; i < 50; i++ {
		_ = new(node, &a)
	}
	println("reused spares", parent.blocks == before)

	// Freeing the most recent allocation gives its space back.
	u := a.Used()
	p := a.Alloc(100, 8)
	a.Free(p, 100, 8)
	println("free last", a.Used() == u)

	// Owners from the library grow inside the arena.
	v := vec.New[int](&a)
	for i := 0; i < 500; i++ {
		v.Push(i * i)
	}
	mp := hash.NewMap[int, int](0, &a)
	for i := 0; i < 300; i++ {
		mp.Set(i, -i)
	}
	got, _ := mp.Get(123)
	println("vec", v.Len(), v.At(499), "map", mp.Len(), got)

	// Through alloc.Use, new and make without an allocator use the arena.
	restore := alloc.Use(&a)
	q := new(node)
	q.val = 7
	s := make([]byte, 10)
	restore.Restore()
	println("use", q.val, len(s), a.Used() > u)

	// Reset keeps the standard chunks; Trim returns them; the arena is
	// usable again afterwards.
	held := a.Held()
	a.Reset()
	println("reset", a.Used(), a.Held() <= held, parent.blocks > 0)
	a.Trim()
	println("trim", a.Held(), parent.blocks, parent.bytes)
	r := new(node, &a)
	r.val = 42
	println("again", r.val, r.next == nil, parent.blocks)

	// Poisoning overwrites released memory.
	a.SetPoison(true)
	m2 := a.Mark()
	b := (*[16]byte)(a.Alloc(16, 1))
	b[0] = 1
	a.Release(m2)
	println("poisoned", b[0] == 0xDD, b[15] == 0xDD)
	c := (*[16]byte)(a.Alloc(16, 1))
	println("zeroed again", c[0], c[15])

	// Typed allocation: views of the arena's memory.
	tn := a.New[node]()
	tn.val = 5
	ts := a.Slice[int](300)
	ts[299] = 7
	println("typed", tn.val, tn.next == nil, len(ts), ts[0], ts[299], uintptr(unsafe.Pointer(&ts[0]))%8 == 0)

	// Drop ends a's life (a is an owner, so it cannot be used afterwards)
	// and gives every chunk back to the parent.
	a.Drop()
	println("drop", parent.blocks, parent.bytes)

	// The same on a fixed Arena.
	var mem [256]byte
	fa := arena.New(mem[:])
	fn := fa.New[node]()
	fn.val = 9
	fs := fa.Slice[uint16](8)
	fs[7] = 3
	println("fixed", fn.val, len(fs), fs[7], fa.Used())
}
