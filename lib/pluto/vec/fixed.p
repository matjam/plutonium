package vec

import "unsafe"

// An Appender is a sequence the library's Append functions write into
// (strconv.AppendInt, fmt.Appendf, utf8.AppendRune and the rest): a *Vec[T],
// which grows from its allocator, or a *Fixed[T], which holds what fits in
// the caller's array and records the rest as truncated. The functions take
// the destination by pointer and update it in place:
//
//	v := vec.New[byte]() // grows
//	defer v.Free()
//	strconv.AppendInt(&v, n, 10)
//
//	var mem [64]byte
//	f := vec.NewFixed(mem[:]) // bounded: never allocates
//	strconv.AppendInt(&f, n, 10)
type Appender[T any] interface {
	Push(x T)
	Append(xs ...T)
	Len() int
	Slice() []T
	Truncate(n int)
}

// A Fixed is a vector over storage the caller provides. It never allocates
// and never panics when full: Push and Append store what fits, drop the
// rest, and record that they did; Truncated reports it. TryPush refuses
// instead, for containers that must not drop elements.
//
// A Fixed holds a view of the caller's array and owns nothing, so it copies
// freely and needs no Free; like any view, it must not outlive the array
// (language spec section 6.3).
type Fixed[T any] struct {
	buf       []T // the storage; len(buf) is the room
	n         int // the elements are buf[:n]
	truncated bool
}

// NewFixed returns an empty Fixed that stores its elements in storage, up
// to len(storage) of them. What storage holds is overwritten.
func NewFixed[T any](storage []T) Fixed[T] {
	return Fixed[T]{buf: storage[:len(storage):len(storage)]}
}

// Push appends x if there is room, and otherwise drops it and records the
// truncation.
//
//pu:inline
func (f *Fixed[T]) Push(x T) {
	// One unsigned comparison is the room check and the bounds check.
	if n := f.n; uint(n) < uint(len(f.buf)) {
		f.buf[n] = x
		f.n = n + 1
		return
	}
	f.truncated = true
}

// TryPush appends x and reports true if there is room, and otherwise
// reports false and leaves f unchanged (not recording a truncation).
func (f *Fixed[T]) TryPush(x T) bool {
	if n := f.n; uint(n) < uint(len(f.buf)) {
		f.buf[n] = x
		f.n = n + 1
		return true
	}
	return false
}

// Append appends as many of xs as fit, and records a truncation if any are
// dropped.
//
//pu:inline
func (f *Fixed[T]) Append(xs ...T) {
	k := copy(f.buf[f.n:], xs)
	f.n += k
	if k < len(xs) {
		f.truncated = true
	}
}

// Len returns the number of elements stored.
func (f *Fixed[T]) Len() int { return f.n }

// Cap returns the number of elements f can hold.
func (f *Fixed[T]) Cap() int { return len(f.buf) }

// Slice returns the elements stored, a view of the caller's storage. Its
// capacity is its length, so reslicing it cannot reach past the elements.
func (f *Fixed[T]) Slice() []T { return f.buf[:f.n:f.n] }

// Truncate shortens f to n elements. It panics if n is negative or greater
// than Len. It does not change Truncated.
func (f *Fixed[T]) Truncate(n int) {
	if n < 0 || n > f.n {
		panic("vec: Truncate out of range")
	}
	clear(f.buf[n:f.n])
	f.n = n
}

// Reset empties f and clears the truncation record.
func (f *Fixed[T]) Reset() {
	f.Truncate(0)
	f.truncated = false
}

// Truncated reports whether a Push or Append has dropped elements since f
// was made or last Reset.
func (f *Fixed[T]) Truncated() bool { return f.truncated }

// AppendString appends the bytes of s to dst: the form of append(b, s...)
// for a destination that may grow (a *Vec[byte]) or is bounded (a
// *Fixed[byte]).
//
//pu:inline
func AppendString[D Appender[byte]](dst D, s string) {
	if len(s) > 0 {
		dst.Append(unsafe.Slice(unsafe.StringData(s), len(s))...)
	}
}
