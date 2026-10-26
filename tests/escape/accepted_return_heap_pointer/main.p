package main

type T struct{ a int }
func f() owned[T] {
	p := new(T)
	p.a = 1
	return p
}
func main() { _ = f() }
