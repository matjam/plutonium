package main
import "pluto/fmt"
import "pluto/alloc"

// Demonstrates RAII auto-free: the compiler automatically inserts
// alloc.freeRaw at scope exit for variables created with alloc.New[T]().
//
// Build: pluto build -o /tmp/raii_test ./examples/14-raii/
// Run:   /tmp/raii_test
// Expected output:
//   value=42
//   returned=99
//   a=10
//   b=20

func createAndUse() {
    ptr := alloc.New[int64]()
    *ptr = 42
    fmt.Printf("value=%d\n", *ptr)
    // NO alloc.Free(ptr) — compiler handles it!
}

func createAndReturn() *int64 {
    ptr := alloc.New[int64]()
    *ptr = 99
    return ptr   // ownership transfers, no free here
}

func main() int64 {
    createAndUse()   // allocation freed automatically at end of createAndUse

    val := createAndReturn()  // main owns it now
    fmt.Printf("returned=%d\n", *val)
    alloc.Free(val)  // explicit free for val (not tracked by RAII since it came from a call)

    // Multiple allocations in same scope — auto-freed at end of main
    a := alloc.New[int64]()
    b := alloc.New[int64]()
    *a = 10
    *b = 20
    fmt.Printf("a=%d\n", *a)
    fmt.Printf("b=%d\n", *b)
    // both freed at end of main

    return 0
}
