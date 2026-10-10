package main
import "pluto/fmt"
import "pluto/alloc"

// Demonstrates automatic reference counting for shared heap pointers.
//
// When a heap-owned pointer is copied (b := a), the refcount is
// incremented. At scope exit, each variable decrements the refcount.
// Only when the refcount reaches zero is the allocation freed.
//
// Without refcounting, the RAII system would try to free twice
// (once for a, once for b), causing a double-free crash.
//
// Build: pluto build -o /tmp/refcount_test ./examples/15-refcount/
// Run:   /tmp/refcount_test
// Expected output:
//   a=42 b=42
//   ok

func shared() {
    a := alloc.New[int64]()
    *a = 42
    b := a          // refcount: 1 → 2
    fmt.Printf("a=%d b=%d\n", *a, *b)
    // b out of scope → refcount: 2 → 1
    // a out of scope → refcount: 1 → 0 → freed
}

func main() int64 {
    shared()
    fmt.Printf("ok\n")
    return 0
}
