// Package atomic provides atomic memory operations, mirroring Go's
// sync/atomic: package-level functions on addresses and the typed values
// Int32, Int64, Uint32, Uint64, Uintptr, Bool and Pointer[T].
//
// All operations are sequentially consistent. They are implemented in
// assembly (atomic_amd64.s) on x86-64.
//
// Unlike volatile[T], which only stops the compiler from removing or
// reordering an access, these operations are atomic with respect to other
// processors and order the surrounding memory accesses.
//
// The functions carry //pu:noescape: they keep no pointer after they
// return, except by storing one argument into *addr, which the escape
// analysis checks at each call.
package atomic

import "unsafe"

// LoadInt32 atomically loads *addr.
//
//pu:noescape
func LoadInt32(addr *int32) (val int32)

// StoreInt32 atomically stores val into *addr.
//
//pu:noescape
func StoreInt32(addr *int32, val int32)

// AddInt32 atomically adds delta to *addr and returns the new value.
//
//pu:noescape
func AddInt32(addr *int32, delta int32) (new int32)

// SwapInt32 atomically stores new into *addr and returns the previous *addr value.
//
//pu:noescape
func SwapInt32(addr *int32, new int32) (old int32)

// CompareAndSwapInt32 executes the compare-and-swap operation for a int32 value.
//
//pu:noescape
func CompareAndSwapInt32(addr *int32, old, new int32) (swapped bool)

// AndInt32 atomically performs a bitwise AND operation on *addr using the bitmask
// provided as mask and returns the old value.
//
//pu:noescape
func AndInt32(addr *int32, mask int32) (old int32)

// OrInt32 atomically performs a bitwise OR operation on *addr using the bitmask
// provided as mask and returns the old value.
//
//pu:noescape
func OrInt32(addr *int32, mask int32) (old int32)

// LoadUint32 atomically loads *addr.
//
//pu:noescape
func LoadUint32(addr *uint32) (val uint32)

// StoreUint32 atomically stores val into *addr.
//
//pu:noescape
func StoreUint32(addr *uint32, val uint32)

// AddUint32 atomically adds delta to *addr and returns the new value.
//
//pu:noescape
func AddUint32(addr *uint32, delta uint32) (new uint32)

// SwapUint32 atomically stores new into *addr and returns the previous *addr value.
//
//pu:noescape
func SwapUint32(addr *uint32, new uint32) (old uint32)

// CompareAndSwapUint32 executes the compare-and-swap operation for a uint32 value.
//
//pu:noescape
func CompareAndSwapUint32(addr *uint32, old, new uint32) (swapped bool)

// AndUint32 atomically performs a bitwise AND operation on *addr using the bitmask
// provided as mask and returns the old value.
//
//pu:noescape
func AndUint32(addr *uint32, mask uint32) (old uint32)

// OrUint32 atomically performs a bitwise OR operation on *addr using the bitmask
// provided as mask and returns the old value.
//
//pu:noescape
func OrUint32(addr *uint32, mask uint32) (old uint32)

// LoadInt64 atomically loads *addr.
//
//pu:noescape
func LoadInt64(addr *int64) (val int64)

// StoreInt64 atomically stores val into *addr.
//
//pu:noescape
func StoreInt64(addr *int64, val int64)

// AddInt64 atomically adds delta to *addr and returns the new value.
//
//pu:noescape
func AddInt64(addr *int64, delta int64) (new int64)

// SwapInt64 atomically stores new into *addr and returns the previous *addr value.
//
//pu:noescape
func SwapInt64(addr *int64, new int64) (old int64)

// CompareAndSwapInt64 executes the compare-and-swap operation for a int64 value.
//
//pu:noescape
func CompareAndSwapInt64(addr *int64, old, new int64) (swapped bool)

// AndInt64 atomically performs a bitwise AND operation on *addr using the bitmask
// provided as mask and returns the old value.
//
//pu:noescape
func AndInt64(addr *int64, mask int64) (old int64)

// OrInt64 atomically performs a bitwise OR operation on *addr using the bitmask
// provided as mask and returns the old value.
//
//pu:noescape
func OrInt64(addr *int64, mask int64) (old int64)

// LoadUint64 atomically loads *addr.
//
//pu:noescape
func LoadUint64(addr *uint64) (val uint64)

// StoreUint64 atomically stores val into *addr.
//
//pu:noescape
func StoreUint64(addr *uint64, val uint64)

// AddUint64 atomically adds delta to *addr and returns the new value.
//
//pu:noescape
func AddUint64(addr *uint64, delta uint64) (new uint64)

// SwapUint64 atomically stores new into *addr and returns the previous *addr value.
//
//pu:noescape
func SwapUint64(addr *uint64, new uint64) (old uint64)

// CompareAndSwapUint64 executes the compare-and-swap operation for a uint64 value.
//
//pu:noescape
func CompareAndSwapUint64(addr *uint64, old, new uint64) (swapped bool)

// AndUint64 atomically performs a bitwise AND operation on *addr using the bitmask
// provided as mask and returns the old value.
//
//pu:noescape
func AndUint64(addr *uint64, mask uint64) (old uint64)

// OrUint64 atomically performs a bitwise OR operation on *addr using the bitmask
// provided as mask and returns the old value.
//
//pu:noescape
func OrUint64(addr *uint64, mask uint64) (old uint64)

// LoadUintptr atomically loads *addr.
//
//pu:noescape
func LoadUintptr(addr *uintptr) (val uintptr)

// StoreUintptr atomically stores val into *addr.
//
//pu:noescape
func StoreUintptr(addr *uintptr, val uintptr)

// AddUintptr atomically adds delta to *addr and returns the new value.
//
//pu:noescape
func AddUintptr(addr *uintptr, delta uintptr) (new uintptr)

// SwapUintptr atomically stores new into *addr and returns the previous *addr value.
//
//pu:noescape
func SwapUintptr(addr *uintptr, new uintptr) (old uintptr)

// CompareAndSwapUintptr executes the compare-and-swap operation for a uintptr value.
//
//pu:noescape
func CompareAndSwapUintptr(addr *uintptr, old, new uintptr) (swapped bool)

// AndUintptr atomically performs a bitwise AND operation on *addr using the bitmask
// provided as mask and returns the old value.
//
//pu:noescape
func AndUintptr(addr *uintptr, mask uintptr) (old uintptr)

// OrUintptr atomically performs a bitwise OR operation on *addr using the bitmask
// provided as mask and returns the old value.
//
//pu:noescape
func OrUintptr(addr *uintptr, mask uintptr) (old uintptr)

// LoadPointer atomically loads *addr.
//
//pu:noescape
func LoadPointer(addr *unsafe.Pointer) (val unsafe.Pointer)

// StorePointer atomically stores val into *addr.
//
//pu:noescape
func StorePointer(addr *unsafe.Pointer, val unsafe.Pointer)

// SwapPointer atomically stores new into *addr and returns the previous *addr value.
//
//pu:noescape
func SwapPointer(addr *unsafe.Pointer, new unsafe.Pointer) (old unsafe.Pointer)

// CompareAndSwapPointer executes the compare-and-swap operation for an
// unsafe.Pointer value.
//
//pu:noescape
func CompareAndSwapPointer(addr *unsafe.Pointer, old, new unsafe.Pointer) (swapped bool)

// Fence is a full memory barrier: no load or store is reordered across it.
func Fence()
