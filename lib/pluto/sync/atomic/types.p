package atomic

import "unsafe"

// A Int32 is an atomic int32. The zero value is zero. A Int32 must not be
// copied after first use.
type Int32 struct {
	v int32
}

// Load atomically loads and returns the value stored in x.
func (x *Int32) Load() int32 { return LoadInt32(&x.v) }

// Store atomically stores val into x.
func (x *Int32) Store(val int32) { StoreInt32(&x.v, val) }

// Swap atomically stores new into x and returns the previous value.
func (x *Int32) Swap(new int32) (old int32) { return SwapInt32(&x.v, new) }

// CompareAndSwap executes the compare-and-swap operation for x.
func (x *Int32) CompareAndSwap(old, new int32) (swapped bool) {
	return CompareAndSwapInt32(&x.v, old, new)
}

// Add atomically adds delta to x and returns the new value.
func (x *Int32) Add(delta int32) (new int32) { return AddInt32(&x.v, delta) }

// And atomically performs a bitwise AND operation on x using the bitmask
// provided as mask and returns the old value.
func (x *Int32) And(mask int32) (old int32) { return AndInt32(&x.v, mask) }

// Or atomically performs a bitwise OR operation on x using the bitmask
// provided as mask and returns the old value.
func (x *Int32) Or(mask int32) (old int32) { return OrInt32(&x.v, mask) }

// A Uint32 is an atomic uint32. The zero value is zero. A Uint32 must not be
// copied after first use.
type Uint32 struct {
	v uint32
}

// Load atomically loads and returns the value stored in x.
func (x *Uint32) Load() uint32 { return LoadUint32(&x.v) }

// Store atomically stores val into x.
func (x *Uint32) Store(val uint32) { StoreUint32(&x.v, val) }

// Swap atomically stores new into x and returns the previous value.
func (x *Uint32) Swap(new uint32) (old uint32) { return SwapUint32(&x.v, new) }

// CompareAndSwap executes the compare-and-swap operation for x.
func (x *Uint32) CompareAndSwap(old, new uint32) (swapped bool) {
	return CompareAndSwapUint32(&x.v, old, new)
}

// Add atomically adds delta to x and returns the new value.
func (x *Uint32) Add(delta uint32) (new uint32) { return AddUint32(&x.v, delta) }

// And atomically performs a bitwise AND operation on x using the bitmask
// provided as mask and returns the old value.
func (x *Uint32) And(mask uint32) (old uint32) { return AndUint32(&x.v, mask) }

// Or atomically performs a bitwise OR operation on x using the bitmask
// provided as mask and returns the old value.
func (x *Uint32) Or(mask uint32) (old uint32) { return OrUint32(&x.v, mask) }

// A Int64 is an atomic int64. The zero value is zero. A Int64 must not be
// copied after first use.
type Int64 struct {
	v int64
}

// Load atomically loads and returns the value stored in x.
func (x *Int64) Load() int64 { return LoadInt64(&x.v) }

// Store atomically stores val into x.
func (x *Int64) Store(val int64) { StoreInt64(&x.v, val) }

// Swap atomically stores new into x and returns the previous value.
func (x *Int64) Swap(new int64) (old int64) { return SwapInt64(&x.v, new) }

// CompareAndSwap executes the compare-and-swap operation for x.
func (x *Int64) CompareAndSwap(old, new int64) (swapped bool) {
	return CompareAndSwapInt64(&x.v, old, new)
}

// Add atomically adds delta to x and returns the new value.
func (x *Int64) Add(delta int64) (new int64) { return AddInt64(&x.v, delta) }

// And atomically performs a bitwise AND operation on x using the bitmask
// provided as mask and returns the old value.
func (x *Int64) And(mask int64) (old int64) { return AndInt64(&x.v, mask) }

// Or atomically performs a bitwise OR operation on x using the bitmask
// provided as mask and returns the old value.
func (x *Int64) Or(mask int64) (old int64) { return OrInt64(&x.v, mask) }

// A Uint64 is an atomic uint64. The zero value is zero. A Uint64 must not be
// copied after first use.
type Uint64 struct {
	v uint64
}

// Load atomically loads and returns the value stored in x.
func (x *Uint64) Load() uint64 { return LoadUint64(&x.v) }

// Store atomically stores val into x.
func (x *Uint64) Store(val uint64) { StoreUint64(&x.v, val) }

// Swap atomically stores new into x and returns the previous value.
func (x *Uint64) Swap(new uint64) (old uint64) { return SwapUint64(&x.v, new) }

// CompareAndSwap executes the compare-and-swap operation for x.
func (x *Uint64) CompareAndSwap(old, new uint64) (swapped bool) {
	return CompareAndSwapUint64(&x.v, old, new)
}

// Add atomically adds delta to x and returns the new value.
func (x *Uint64) Add(delta uint64) (new uint64) { return AddUint64(&x.v, delta) }

// And atomically performs a bitwise AND operation on x using the bitmask
// provided as mask and returns the old value.
func (x *Uint64) And(mask uint64) (old uint64) { return AndUint64(&x.v, mask) }

// Or atomically performs a bitwise OR operation on x using the bitmask
// provided as mask and returns the old value.
func (x *Uint64) Or(mask uint64) (old uint64) { return OrUint64(&x.v, mask) }

// A Uintptr is an atomic uintptr. The zero value is zero. A Uintptr must not be
// copied after first use.
type Uintptr struct {
	v uintptr
}

// Load atomically loads and returns the value stored in x.
func (x *Uintptr) Load() uintptr { return LoadUintptr(&x.v) }

// Store atomically stores val into x.
func (x *Uintptr) Store(val uintptr) { StoreUintptr(&x.v, val) }

// Swap atomically stores new into x and returns the previous value.
func (x *Uintptr) Swap(new uintptr) (old uintptr) { return SwapUintptr(&x.v, new) }

// CompareAndSwap executes the compare-and-swap operation for x.
func (x *Uintptr) CompareAndSwap(old, new uintptr) (swapped bool) {
	return CompareAndSwapUintptr(&x.v, old, new)
}

// Add atomically adds delta to x and returns the new value.
func (x *Uintptr) Add(delta uintptr) (new uintptr) { return AddUintptr(&x.v, delta) }

// And atomically performs a bitwise AND operation on x using the bitmask
// provided as mask and returns the old value.
func (x *Uintptr) And(mask uintptr) (old uintptr) { return AndUintptr(&x.v, mask) }

// Or atomically performs a bitwise OR operation on x using the bitmask
// provided as mask and returns the old value.
func (x *Uintptr) Or(mask uintptr) (old uintptr) { return OrUintptr(&x.v, mask) }

// A Bool is an atomic boolean value. The zero value is false. A Bool must
// not be copied after first use.
type Bool struct {
	v uint32
}

func b32(b bool) uint32 {
	if b {
		return 1
	}
	return 0
}

// Load atomically loads and returns the value stored in x.
func (x *Bool) Load() bool { return LoadUint32(&x.v) != 0 }

// Store atomically stores val into x.
func (x *Bool) Store(val bool) { StoreUint32(&x.v, b32(val)) }

// Swap atomically stores new into x and returns the previous value.
func (x *Bool) Swap(new bool) (old bool) { return SwapUint32(&x.v, b32(new)) != 0 }

// CompareAndSwap executes the compare-and-swap operation for the boolean
// value x.
func (x *Bool) CompareAndSwap(old, new bool) (swapped bool) {
	return CompareAndSwapUint32(&x.v, b32(old), b32(new))
}

// A Pointer is an atomic pointer of type *T. The zero value is a nil *T. A
// Pointer must not be copied after first use.
type Pointer[T any] struct {
	v unsafe.Pointer
}

// Load atomically loads and returns the value stored in x.
func (x *Pointer[T]) Load() *T { return (*T)(LoadPointer(&x.v)) }

// Store atomically stores val into x.
func (x *Pointer[T]) Store(val *T) { StorePointer(&x.v, unsafe.Pointer(val)) }

// Swap atomically stores new into x and returns the previous value.
func (x *Pointer[T]) Swap(new *T) (old *T) {
	return (*T)(SwapPointer(&x.v, unsafe.Pointer(new)))
}

// CompareAndSwap executes the compare-and-swap operation for x.
func (x *Pointer[T]) CompareAndSwap(old, new *T) (swapped bool) {
	return CompareAndSwapPointer(&x.v, unsafe.Pointer(old), unsafe.Pointer(new))
}
