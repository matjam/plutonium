// Method expressions: through pointer types, promoted methods, generic
// receiver types and interfaces, called directly and used as values.
package main

type Stringer interface{ String() string }

type S struct{ a, b int }

func (s S) Sum() int     { return s.a + s.b }
func (s *S) Inc(n int)   { s.a += n }
func (s S) String() string {
	if s.a > s.b {
		return "a"
	}
	return "b"
}

type Outer struct {
	x int
	S
}

type Vec[E any] struct {
	items [4]E
	n     int
}

func (v Vec[E]) Len() int      { return v.n }
func (v *Vec[E]) Push(x E)     { v.items[v.n] = x; v.n++ }
func (v *Vec[E]) Get(i int) E  { return v.items[i] }

func main() {
	s := S{1, 2}
	println(S.Sum(s))
	(*S).Inc(&s, 5)
	println(s.a, (*S).Sum(&s))
	f := (*S).Sum
	println(f(&s))
	g := S.Sum
	println(g(s))
	println(Outer.Sum(Outer{x: 1, S: S{3, 4}}))
	o := &Outer{S: S{7, 1}}
	(*Outer).Inc(o, 2)
	println(o.a, (*Outer).Sum(o))
	h := (*Outer).Sum
	println(h(o))
	var v Vec[int]
	(*Vec[int]).Push(&v, 4)
	(*Vec[int]).Push(&v, 9)
	println(Vec[int].Len(v), (*Vec[int]).Len(&v), (*Vec[int]).Get(&v, 1))
	push := (*Vec[int]).Push
	push(&v, 11)
	println(v.n)
	println(Stringer.String(s))
	str := Stringer.String
	println(str(S{5, 1}))
	var st Stringer = s
	defer st.String()
	defer println("deferred", st.String())
}
