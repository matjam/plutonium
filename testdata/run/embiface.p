// Methods promoted from an embedded interface field: calls, method values,
// deferred calls and conversion of the struct to the interface.
package main

type Stringer interface{ String() string }

type I int

func (i I) String() string {
	if i > 0 {
		return "pos"
	}
	return "nonpos"
}

type E struct {
	n int
	Stringer
}

func main() {
	e := E{1, I(3)}
	println(e.String())
	pe := &e
	println(pe.String())
	m := e.String
	println(m())
	var s Stringer = e
	println(s.String())
	s = pe
	println(s.String())
	defer e.String()
}
