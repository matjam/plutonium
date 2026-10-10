package main

type Shape interface {
	Area() int
	Name() string
}

type Rect struct{ w, h int }

func (r Rect) Area() int     { return r.w * r.h }
func (r Rect) Name() string  { return "rect" }

type Square struct{ s int }

func (s *Square) Area() int    { return s.s * s.s }
func (s *Square) Name() string { return "square" }

type Named interface{ Name() string }

type myErr struct{ code int }

func (e *myErr) Error() string { return "myErr" }

// errFound is package-level: in Plutonium &myErr{42} would live in find's
// frame and could not be returned.
var errFound = myErr{42}

func find(fail bool) error {
	if fail {
		return &errFound
	}
	return nil
}

func describe(v any) string {
	switch x := v.(type) {
	case nil:
		return "nil"
	case int:
		if x > 10 {
			return "big int"
		}
		return "int"
	case string:
		return "string " + "value"
	case bool:
		return "bool"
	case Shape:
		if x.Name() == "rect" {
			return "shape rect"
		}
		return "shape"
	default:
		return "other"
	}
}

type Celsius int

func (c Celsius) String() string { return "celsius" }

type Stringer interface{ String() string }

func main() {
	shapes := []Shape{Rect{3, 4}, &Square{5}, Rect{1, 1}}
	total := 0
	for _, s := range shapes {
		total += s.Area()
		println(s.Name(), s.Area())
	}
	println("total", total)
	var n Named = shapes[1]
	println("named", n.Name())
	if sq, ok := n.(*Square); ok {
		println("is square", sq.s)
	}
	if _, ok := n.(Rect); !ok {
		println("not rect")
	}
	if sh, ok := n.(Shape); ok {
		println("shape via iface", sh.Area())
	}
	println(describe(nil), describe(3), describe(30), describe("x"), describe(true), describe(Rect{2, 2}), describe(1.5))
	err := find(true)
	if err != nil {
		println("error", err.Error())
	}
	if find(false) == nil {
		println("no error")
	}
	var st Stringer = Celsius(30)
	println("stringer", st.String())
	var a, b any = 5, 5
	println("any eq", a == b, a == any(6), a != nil)
	var r1, r2 Shape = Rect{1, 2}, Rect{1, 2}
	println("iface eq", r1 == r2, r1 == shapes[0])
}
