package main

// Conversions between non-empty interfaces and assertions to interface
// types look itabs up at run time in a hash table the compiler lays out.
// Many types and interfaces, some implemented and some not, exercise it.

type Namer interface{ Name() string }
type Sizer interface{ Size() int }
type Both interface {
	Namer
	Sizer
}
type Weigher interface{ Weight() int }
type Shouter interface{ Shout() string }

type a struct{ n int }
type b struct{ s string }
type c [2]int
type d int
type e struct{ p *int }
type f struct{ x, y, z int }
type g string
type h uint8

func (a) Name() string   { return "a" }
func (a) Size() int      { return 1 }
func (b) Name() string   { return "b" }
func (c) Size() int      { return 3 }
func (c) Weight() int    { return 30 }
func (d) Name() string   { return "d" }
func (d) Size() int      { return 4 }
func (d) Weight() int    { return 40 }
func (e) Shout() string  { return "E!" }
func (e) Name() string   { return "e" }
func (f) Size() int      { return 6 }
func (f) Name() string   { return "f" }
func (f) Shout() string  { return "F!" }
func (g) Weight() int    { return 70 }
func (h) Name() string   { return "h" }
func (*h) Shout() string { return "H!" }
func (*a) Weight() int   { return 10 }
func (*b) Size() int     { return 2 }

func describe(v any) {
	n := 0
	if x, ok := v.(Namer); ok {
		print(x.Name(), " ")
		n++
	}
	if x, ok := v.(Sizer); ok {
		print(x.Size(), " ")
		n++
	}
	if x, ok := v.(Both); ok {
		print("both:", x.Name(), x.Size(), " ")
		n++
	}
	if x, ok := v.(Weigher); ok {
		print(x.Weight(), " ")
		n++
	}
	if x, ok := v.(Shouter); ok {
		print(x.Shout(), " ")
		n++
	}
	println(n)
}

// widen converts a Both to its embedded interfaces (convI2I).
func widen(x Both) (Namer, Sizer) { return x, x }

func main() {
	var hv h = 8
	av := a{1}
	bv := b{"x"}
	values := []any{a{1}, b{"x"}, c{1, 2}, d(4), e{}, f{1, 2, 3}, g("g"), hv, &hv, &av, &bv, 7, "str", nil}
	for round := 0; round < 3; round++ {
		for _, v := range values {
			describe(v)
		}
	}
	for _, x := range []Both{a{2}, d(5), f{}} {
		n, s := widen(x)
		println(n.Name(), s.Size())
	}
	var w Weigher = d(9)
	if s, ok := w.(Shouter); ok {
		println("d shouts", s.Shout())
	} else {
		println("d does not shout")
	}
	if b, ok := w.(Both); ok {
		println("d is Both", b.Name())
	}
}
