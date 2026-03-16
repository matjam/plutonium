package main

import "pluto/fmt"
import "pluto/alloc"

// Demonstrates the slab allocator via the alloc package.
// Multiple small allocations share a single mmap'd page rather than
// each consuming a separate 4KB page.
//
// Build: pluto build -o /tmp/alloc_test ./examples/11-alloc/
// Run:   /tmp/alloc_test
// Expected output:
//   heap value=42
//   heap value2=99
//   a=10 b=20
//   c=30 d=40
//   reuse=999

func main() int64 {
    // --- basic alloc/free ---
    ptr := alloc.New[int64]()
    *ptr = 42
    fmt.Printf("heap value=%d\n", *ptr)
    alloc.Free(ptr)

    // --- second alloc (reuses freed slot from slab) ---
    ptr2 := alloc.New[int64]()
    *ptr2 = 99
    fmt.Printf("heap value2=%d\n", *ptr2)
    alloc.Free(ptr2)

    // --- multiple simultaneous allocations from the slab ---
    a := alloc.New[int64]()
    b := alloc.New[int64]()
    *a = 10
    *b = 20
    fmt.Printf("a=%d b=%d\n", *a, *b)
    alloc.Free(a)
    alloc.Free(b)

    c := alloc.New[int64]()
    d := alloc.New[int64]()
    *c = 30
    *d = 40
    fmt.Printf("c=%d d=%d\n", *c, *d)
    alloc.Free(c)
    alloc.Free(d)

    // --- verify reuse: freed slots are recycled ---
    p := alloc.New[int64]()
    *p = 999
    fmt.Printf("reuse=%d\n", *p)
    alloc.Free(p)

    return 0
}
