package main

type Dev[T ~uint8 | ~uint32 | *int] struct{ reg volatile[T] }

func (d *Dev[T]) Set(v T) T {
	d.reg = v
	return d.reg
}
