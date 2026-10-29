package main

import (
	"pluto/alloc"
	"pluto/fmt"
	"pluto/strings"
	"unsafe"
)

// A pointer method that keeps a pointer it loads from its receiver keeps
// what that pointer points at, not the receiver. (*bump).grow loads a
// chunk from b.spare and stores it in b.cur. Printing &sb reaches
// strings.(*Builder).String, which allocates through the current
// allocator, an alloc.Allocator call with (*bump).Alloc among its
// methods. sb is in main's frame and nothing keeps it, so the program is
// accepted. Read as keeping the receiver itself, as escape2 once did
// (d13d9cef's first form), the call keeps the Builder, and the print is
// rejected: "cannot pass &sb to fmt.Println, which keeps it after
// returning". That reading rejected 147 lines of the compiler.

const chunkSize = 4096

// bump fills chunks of its parent's memory in order and keeps freed
// chunks on a spare list, as compiler/internal/scratch's bump does.
type bump struct {
	parent alloc.Allocator
	cur    unsafe.Pointer
	off    uintptr
	size   uintptr
	spare  unsafe.Pointer
}

func (b *bump) Alloc(size, align uintptr) unsafe.Pointer {
	if align < 8 {
		align = 8
	}
	base := uintptr(b.cur)
	off := (base+b.off+align-1)&^(align-1) - base
	if b.cur == nil || off+size > b.size {
		if !b.grow(size + align) {
			return nil
		}
		base = uintptr(b.cur)
		off = (base+b.off+align-1)&^(align-1) - base
	}
	p := unsafe.Add(b.cur, off)
	b.off = off + size
	return p
}

func (b *bump) grow(need uintptr) bool {
	n := uintptr(chunkSize)
	if need+16 > n {
		n = (need + 31) &^ 15
	}
	var c unsafe.Pointer
	if n == chunkSize && b.spare != nil {
		c = b.spare
		b.spare = *(*unsafe.Pointer)(c)
	} else {
		c = b.parent.Alloc(n, 16)
		if c == nil {
			return false
		}
	}
	*(*unsafe.Pointer)(c) = b.cur
	*(*uintptr)(unsafe.Add(c, 8)) = n
	b.cur, b.off, b.size = c, 16, n
	return true
}

func (b *bump) Free(p unsafe.Pointer, size, align uintptr) {}

func main() {
	b := alloc.New[bump]()
	b.parent = alloc.Default()
	u := alloc.Use(b)
	var sb strings.Builder
	sb.WriteString("x")
	fmt.Println(&sb)
	u.Restore()
}
