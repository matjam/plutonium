package main

import "pluto/fmt"
import "unsafe"

// Test of volatile[T], aligned[T, N], and unsafe package intrinsics.
//
// Build: pluto build -o volatile_test ./examples/10-volatile-aligned-unsafe/
// Run:   ./volatile_test
// Expected output:
//   volatile=42
//   sizeof(int64)=8
//   sizeof(uint32)=4
//   roundtrip=99

func main() int64 {
    // volatile: demonstrate type acceptance
    var v volatile[uint32]
    v = 42
    x := v
    fmt.Printf("volatile=%d\n", x)

    // unsafe.Sizeof
    s := unsafe.Sizeof[int64]()
    fmt.Printf("sizeof(int64)=%d\n", s)

    s2 := unsafe.Sizeof[uint32]()
    fmt.Printf("sizeof(uint32)=%d\n", s2)

    // unsafe pointer round-trip
    var val int64
    val = 99
    ptr := unsafe.From[int64](&val)
    back := unsafe.As[int64](ptr)
    fmt.Printf("roundtrip=%d\n", *back)

    return 0
}
