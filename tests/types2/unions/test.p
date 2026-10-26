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

var st DeviceState

func use(st DeviceState, r Result[int], pte PTE, d Desc) (int, error) {
	st = DeviceState.Off
	st = DeviceState.Probing{attempt: 1}
	st = DeviceState.Ready{irq: 11, base: 0xFEB0_0000}
	st = DeviceState.Failed(Errno.EIO)
	mk := Result[string].Ok
	_ = mk("x")
	switch s := st.(type) {
	case DeviceState.Off:
	case Probing:
		_ = s.attempt
	case Ready:
		_ = s.irq
	case Failed:
		var e error = s
		_ = e
	}
	switch st.(type) {
	case Off, Probing:
	default:
	}
	n := 0
	switch v := r.(type) {
	case Ok:
		n = v
	case Err:
		if v != Errno.EINTR {
			return 0, v
		}
	}
	if v, ok := r.(Result[int].Ok); ok {
		n += v
	}
	p, ok := st.(DeviceState.Probing)
	_, _ = p.attempt, ok
	_ = st == DeviceState.Off && r != Result[int].Ok(1)
	var a Areaer = Shape.Circle{r: 1}
	var x any = st
	switch x.(type) {
	case DeviceState:
	}
	switch e := pte.(type) {
	case PTE.Mapped:
		n += int(e.frame)
		e.frame++
		pte = PTE.Mapped(e)
	case Swapped:
		_ = e.slot
	}
	_ = pte.present
	pte = PTE(uint64(1))
	pte = PTE.Mapped{writable: true, frame: 7}
	_ = uint64(pte)
	switch e := d.(type) {
	case S:
		_ = e.n
	case L:
		_ = e.kind
	}
	o := Option[Result[int]].Some(r)
	_ = Map(r, func(i int) string { return "" })
	var tail List
	l := List.Cons{head: 1, tail: &tail}
	_, _, _ = a, o, l
	return n, nil
}

// Unions with identical variants convert explicitly.
type A union { X int; Y }
type B union { X int; Y }

func conv(a A) B { return B(a) }

func last(o Option[int]) int {
	switch v := o.(type) {
	case Some:
		return v
	case None:
		return 0
	}
}
