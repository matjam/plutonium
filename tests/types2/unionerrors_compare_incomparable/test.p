package main

type Errno enum int32 { OK; EPERM; EIO = 5; EINTR = 4 }

func (e Errno) Error() string { return "errno" }

type DeviceState union {
	Off
	Probing struct{ attempt int }
	Ready   struct {
		irq  uint8
		base uintptr
	}
	Failed Errno
}

type Result[T any] union {
	Ok  T
	Err Errno
}

type Option[T any] union {
	None
	Some T
}

type Kind enum { Small; Large }

type Shape union {
	Circle struct{ r float64 }
	Rect   struct{ w, h float64 }
	Tagged Kind
	Nothing
}

func (s Shape) Area() float64 {
	switch v := s.(type) {
	case Circle:
		return 3 * v.r * v.r
	case Rect:
		return v.w * v.h
	case Tagged, Nothing:
		return 0
	}
}

type Areaer interface{ Area() float64 }

type PTE packed union uint64 {
	present bits[bool, 1]

	Swapped case false {
		slot bits[uint64, 63]
	}
	Mapped case true {
		writable bits[bool, 1]
		user     bits[bool, 1]
		_        bits[uint, 9]
		frame    bits[uint64, 40]
		_        bits[uint, 11]
		nx       bits[bool, 1]
	}
}

type Desc packed union uint8 {
	kind Kind
	S case Kind.Small { n bits[uint8, 7] }
	L case Kind.Large {}
}

type List union {
	Nil
	Cons struct {
		head int
		tail *List
	}
}

func Map[T, U any](r Result[T], f func(T) U) Result[U] {
	switch v := r.(type) {
	case Ok:
		return Result[U].Ok(f(v))
	case Result[T].Err:
		return Result[U].Err(v)
	}
}

type U union { A []int; B }
var x, y U
var b = x == y