package main

import (
	"pluto/fmt"
	"pluto/sync/atomic"
	"unsafe"
)

type node struct{ v int }

func main() {
	var i32 atomic.Int32
	i32.Store(-5)
	fmt.Printf("%d %d %d ", i32.Load(), i32.Add(-10), i32.Swap(100))
	fmt.Printf("%t %t %d ", i32.CompareAndSwap(1, 2), i32.CompareAndSwap(100, -2147483648), i32.Load())
	fmt.Printf("%d %d %d\n", i32.Add(-1), i32.Or(0x0f), i32.And(0x7f))
	var u32 atomic.Uint32
	u32.Store(0xFFFFFFF0)
	fmt.Printf("%d %d %d %t %d %d\n", u32.Load(), u32.Add(0x20), u32.Swap(7), u32.CompareAndSwap(7, 0x80000000), u32.Load(), u32.Or(1))
	var i64 atomic.Int64
	fmt.Printf("%d %d %d %t %d %d %d\n", i64.Add(1<<40), i64.Add(-3<<40), i64.Swap(-1), i64.CompareAndSwap(-1, 9), i64.Load(), i64.And(12), i64.Load())
	var u64 atomic.Uint64
	u64.Store(1 << 63)
	fmt.Printf("%d %d %t %d\n", u64.Add(1<<63), u64.Or(0xff00), u64.CompareAndSwap(0, 1), u64.Load())
	var up atomic.Uintptr
	fmt.Printf("%d %d\n", up.Add(4096), up.Swap(0))
	var b atomic.Bool
	fmt.Printf("%t %t %t %t %t %t\n", b.Load(), b.Swap(true), b.Load(), b.CompareAndSwap(false, true), b.CompareAndSwap(true, false), b.Load())
	b.Store(true)
	fmt.Printf("%t\n", b.Load())
	var p atomic.Pointer[node]
	n1 := &node{1}
	n2 := &node{2}
	fmt.Printf("%t ", p.Load() == nil)
	p.Store(n1)
	fmt.Printf("%d %t %t ", p.Load().v, p.CompareAndSwap(n2, n1), p.CompareAndSwap(n1, n2))
	fmt.Printf("%d %d\n", p.Swap(n1).v, p.Load().v)

	var raw int32 = 3
	atomic.AddInt32(&raw, 4)
	atomic.StoreInt32(&raw, atomic.LoadInt32(&raw)*2)
	var r64 uint64
	atomic.StoreUint64(&r64, 77)
	var rp unsafe.Pointer
	atomic.StorePointer(&rp, unsafe.Pointer(n2))
	fmt.Printf("%d %d %d %t %d\n", atomic.LoadInt32(&raw), atomic.SwapUint64(&r64, 5), atomic.LoadUint64(&r64), atomic.CompareAndSwapInt32(&raw, 14, 15), (*node)(atomic.LoadPointer(&rp)).v)
	var sum int64
	for k := int32(-3); k < 3; k++ {
		var x atomic.Int32
		x.Store(k)
		sum += int64(x.Load()) + int64(x.Add(k))
	}
	fmt.Printf("%d\n", sum)
}
