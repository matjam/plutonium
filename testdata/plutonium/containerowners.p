package main

import (
	"pluto/hash"
	"pluto/vec"
)

// Containers that hold owners (language spec sections 7.18, 10.9 and 18.6):
// a vec.Vec and a hash.Map of Owner, whose Drop is counted. Dropping
// a container drops its elements in order, replacing one drops the old one,
// and Pop, Remove and Pop move one out.

var drops int

type Owner owner struct {
	id   int
	name string
}

func (r *Owner) Drop() {
	if r.id != 0 { // the zero value is empty
		drops++
		println("  drop", r.id, r.name)
	}
}

func mk(id int) Owner { return Owner{id: id, name: "res" + string(rune('0'+id%10))} }

func report(what string, want int) {
	if drops != want {
		println("FAIL", what, "drops", drops, "want", want)
	} else {
		println("ok", what)
	}
}

func vecs() {
	println("vec")
	drops = 0
	v := vec.New[Owner]()
	for i := 1; i <= 20; i++ { // grows several times
		v.Push(mk(i))
	}
	report("push", 0)
	println(v.Len(), v.Cap() >= 20)

	last, ok := v.Pop()
	println("pop", last.id, ok, v.Len())
	report("pop", 0)

	mid := v.Remove(2)
	println("remove", mid.id, v.Len())
	sw := v.SwapRemove(0)
	println("swapremove", sw.id, v.Len(), v.Ptr(0).id)
	report("remove", 0)

	v.Set(1, mk(100)) // drops the element it replaces
	report("set", 1)
	v[2] = mk(101) // SetIndex
	report("setindex", 2)

	sum := 0
	for i, p := range v {
		sum += p.id * (i + 1)
	}
	println("sum", sum)
	for i, p := range v.Backward() {
		if i == v.Len()-1 {
			println("last", p.id)
		}
	}
	v.Ptr(3).id = 300 // a view of an element

	drops = 0
	v.Truncate(15)
	println("truncated", v.Len())
	report("truncate", 2)

	drops = 0
	v.Clear()
	println("cleared", v.Len())
	report("clear", 15)

	v.Push(mk(1))
	v.Push(mk(2))
	v.Push(mk(3))
	drops = 0
	v.Free()
	report("free", 3)
	println("freed", v.Len())

	v.Push(mk(4)) // reusable after Free
	v.Push(mk(5))
	drops = 0
}

func vecDropped() {
	println("vecDropped")
	drops = 0
	v := vec.New[Owner]()
	v.Push(mk(1))
	v.Push(mk(2))
	v.Push(mk(3))
	println("end of scope")
}

func vecMoved() vec.Vec[Owner] {
	v := vec.New[Owner]()
	v.Push(mk(7))
	v.Push(mk(8))
	return v
}

func maps() {
	println("map")
	drops = 0
	m := hash.NewMap[int, Owner](0)
	for i := 1; i <= 30; i++ { // grows
		m.Set(i, mk(i))
	}
	report("set", 0)
	println(m.Len())

	m.Set(5, mk(50)) // drops the value it replaces
	report("replace", 1)
	m[6] = mk(60)
	report("setindex", 2)

	n := 0
	for k, p := range m {
		n += k + p.id
	}
	println("walk", n)
	println(m.Ptr(7).id, m.Has(7), m.Has(99))

	println(m.Delete(8), m.Delete(8))
	report("delete", 3)

	r, ok := m.Remove(9)
	println("remove", r.id, ok)
	_, ok = m.Remove(9)
	println("remove again", ok)
	report("remove", 3)

	k, v, ok := m.Pop()
	println("pop", k == v.id || v.id > 0, ok, m.Len())
	report("pop", 3)

	drops = 0
	m.Clear()
	println("cleared", m.Len())
	report("clear", 27)

	m.Set(1, mk(1))
	m.Set(2, mk(2))
	drops = 0
	m.Free()
	report("free", 2)
}

func mapDropped() {
	println("mapDropped")
	drops = 0
	m := hash.NewMap[string, Owner](0)
	m.Set("a", mk(1))
	m.Set("b", mk(2))
	println("end of scope")
}

func mapLoop() {
	println("mapLoop")
	drops = 0
	m := hash.NewMap[string, Owner](0)
	for i := 1; i <= 6; i++ {
		m.Set("k"+string(rune('0'+i)), mk(i))
	}
	got := 0
	for _, v, ok := m.Pop(); ok; _, v, ok = m.Pop() {
		got += v.id
	}
	println("popped", got, m.Len())
	report("popall", 6) // each value dropped at the end of its iteration
}

// Values of a plain type still behave as before.
func plain() {
	s := vec.New[string]()
	s.Push("a")
	s.Push("b")
	x, _ := s.Pop()
	println(x, s.Len(), s[0])
	m := hash.NewMap[string, string](0)
	m.Set("k", "v")
	println(m["k"], m.Len())
}

// Generic code over the containers of plain values ranges as before: the
// iterators yield the elements themselves (rt.View of a value type).
func sum[T int | float64](v *vec.Vec[T]) T {
	var t T
	for _, x := range v {
		t += x
	}
	return t
}

func total[K comparable, V int | float64](m *hash.Map[K, V]) V {
	var t V
	for _, x := range m {
		t += x
	}
	return t
}

func generic() {
	v := vec.New[int]()
	v.Push(1)
	v.Push(2)
	f := vec.New[float64]()
	f.Push(1.5)
	m := hash.NewMap[string, int](0)
	m.Set("a", 5)
	println("generic", sum(&v), sum(&f), total(&m))
}

func main() {
	vecs()
	println("after vecs")
	vecDropped()
	println("after vecDropped")
	mv := vecMoved()
	println("moved", mv.Len())
	mv.Free()
	mapsRun()
	plain()
	generic()
	println("done")
}

func mapsRun() {
	maps()
	mapDropped()
	println("after mapDropped")
	mapLoop()
}
