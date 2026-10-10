// ring[T, N] iterators: All yields the index and element of each element,
// Values only the element, oldest first. The loop reads the ring as it goes,
// so the body may pop and push (language spec section 5.4).
package main

import "pluto/iter"

type Number interface{ ~int | ~int64 | ~float64 }

type Item struct {
	id   int
	name string
	pad  [3]int64
}

type Buf ring[int, 4]

type Holder struct {
	tag   int
	items ring[int, 4]
}

func show(r *ring[int, 4]) {
	for i, x := range r.All() {
		println(" ", i, x)
	}
}

// sum ranges over a ring of any number type.
func sum[T Number](r *ring[T, 4]) T {
	var total T
	for x := range r.Values() {
		total += x
	}
	return total
}

// firstOver returns the index of the first element above limit.
func firstOver(r *ring[int, 8], limit int) int {
	for i, x := range r.All() {
		if x > limit {
			return i
		}
	}
	return -1
}

func empty() {
	var r ring[int, 4]
	n := 0
	for range r.All() {
		n++
	}
	for range r.Values() {
		n++
	}
	println("empty:", n)
}

// order checks FIFO order across the wrap of the indexes.
func order() {
	var r ring[int, 4]
	for i := 1; i <= 4; i++ {
		r.Push(i)
	}
	r.Pop()
	r.Pop()
	r.Push(5)
	r.Push(6)
	println("order, all:")
	show(&r)
	println("order, values:")
	for x := range r.Values() {
		println(" ", x)
	}
	println("len after:", r.Len())
}

func exits() {
	var r ring[int, 8]
	for i := 1; i <= 6; i++ {
		r.Push(i * 10)
	}
	for i, x := range r.All() {
		if x == 20 {
			continue
		}
		if x == 50 {
			break
		}
		println("exit:", i, x)
	}
	println("firstOver 25:", firstOver(&r, 25), "firstOver 99:", firstOver(&r, 99))
outer:
	for i := range r.Values() {
		for j := range r.Values() {
			if j > i {
				continue outer
			}
			if i == 30 {
				break outer
			}
			println("pair:", i, j)
			if j == 20 {
				break
			}
		}
	}
}

// popping removes elements the loop has not reached from its path, and
// shifts the index of the ones that remain.
func popping() {
	var r ring[int, 4]
	for i := 1; i <= 4; i++ {
		r.Push(i * 10)
	}
	println("pop one each:")
	for i, x := range r.All() {
		println(" ", i, x)
		r.Pop()
	}
	println("len:", r.Len(), r.Empty())

	for i := 1; i <= 4; i++ {
		r.Push(i * 10)
	}
	println("pop two at the first:")
	for i, x := range r.All() {
		println(" ", i, x)
		if x == 10 {
			r.Pop()
			r.Pop()
		}
	}
	println("len:", r.Len())
	x, ok := r.Peek()
	println("peek:", x, ok)
	r.Reset()
}

// pushing makes the loop visit what it pushed, as long as there is room.
func pushing() {
	var r ring[int, 8]
	for i := 1; i <= 3; i++ {
		r.Push(i)
	}
	println("push while below 10:")
	for i, x := range r.All() {
		println(" ", i, x)
		if x < 10 {
			r.Push(x * 10)
		}
	}
	r.Reset()

	var f ring[int, 4]
	for i := 1; i <= 3; i++ {
		f.Push(i)
	}
	println("push to a full ring:")
	for i, x := range f.All() {
		ok := false
		if x < 10 {
			ok = f.Push(x * 10)
		}
		println(" ", i, x, ok)
	}
}

// rotating pops the element the loop is at and pushes another: the ring
// never grows, and the loop goes on until nothing is pushed.
func rotating() {
	var r ring[int, 4]
	for i := 1; i <= 3; i++ {
		r.Push(i)
	}
	println("rotate:")
	for i, x := range r.All() {
		println(" ", i, x)
		r.Pop()
		if x < 6 {
			r.Push(x + 3)
		}
	}
	println("len:", r.Len())
}

func resetting() {
	var r ring[int, 4]
	for i := 1; i <= 4; i++ {
		r.Push(i)
	}
	println("reset:")
	for i, x := range r.All() {
		println(" ", i, x)
		r.Reset()
	}
	println("len:", r.Len())

	for i := 1; i <= 3; i++ {
		r.Push(i)
	}
	println("reset and push:")
	for i, x := range r.All() {
		println(" ", i, x)
		if x == 1 {
			r.Reset()
			r.Push(7)
			r.Push(8)
			r.Push(9)
		}
	}
}

func elements() {
	var r ring[Item, 4]
	for i := 1; i <= 3; i++ {
		r.Push(Item{id: i, name: "item", pad: [3]int64{int64(i), 2, 3}})
	}
	r.Pop()
	r.Push(Item{id: 4, name: "last", pad: [3]int64{40, 41, 42}})
	for i, it := range r.All() {
		println("item:", i, it.id, it.name, it.pad[0], it.pad[2])
	}
	var b ring[byte, 8]
	for i := 0; i < 8; i++ {
		b.Push(byte('a' + i))
	}
	for i := 0; i < 5; i++ {
		b.Pop()
	}
	b.Push('x')
	for i, c := range b.All() {
		println("byte:", i, c)
	}
	var s ring[string, 2]
	s.Push("one")
	s.Push("two")
	for v := range s.Values() {
		println("string:", v)
	}
}

func generic() {
	var ri ring[int, 4]
	var rf ring[float64, 4]
	var rl ring[int64, 4]
	for i := 1; i <= 4; i++ {
		ri.Push(i)
		rf.Push(float64(i) / 2)
		rl.Push(int64(i) << 40)
	}
	println("sums:", sum(&ri), int(sum(&rf)*10), sum(&rl)>>40)
}

func named() {
	var b Buf
	b.Push(1)
	b.Push(2)
	var h Holder
	h.items.Push(7)
	h.items.Push(8)
	h.tag = 5
	p := &h
	for i, x := range b.All() {
		println("named:", i, x)
	}
	for i, x := range p.items.All() {
		println("field:", i, x, p.tag)
	}
}

// ranging over a ring ranges over its All method.
func ranging() {
	var r ring[int, 4]
	for i := 1; i <= 3; i++ {
		r.Push(i * 5)
	}
	for i, x := range r {
		println("range:", i, x)
	}
	for i := range &r {
		println("range index:", i)
	}
	for range r {
		println("range none")
	}
	var h Holder
	h.items.Push(4)
	p := &h
	for i, x := range p.items {
		println("range field:", i, x)
	}
	var b Buf
	b.Push(9)
	for i, x := range b {
		println("range named:", i, x)
	}
	for _, x := range r {
		if x > 5 {
			r.Pop()
		}
	}
	println("len:", r.Len())
}

// iterators are ordinary values of the caller's frame: they can be kept in
// a variable, ranged over twice, and passed to pluto/iter.
func values() {
	var r ring[int, 8]
	for i := 1; i <= 6; i++ {
		r.Push(i)
	}
	seq := r.Values()
	n := 0
	for x := range seq {
		n += x
	}
	for x := range seq {
		n += x
	}
	println("twice:", n)
	println("count:", iter.Count(r.Values()))
	even := iter.Filter(func(x int) bool { return x%2 == 0 }, r.Values())
	println("even sum:", iter.Reduce(func(s, x int) int { return s + x }, 0, even))
	for i, x := range iter.Enumerate(iter.Limit(r.Values(), 2)) {
		println("limit:", i, x)
	}
	var out [3]int
	println("fill:", iter.Fill(out[:], r.Values()), out[0], out[1], out[2])
	for i, x := range r.All() {
		if i == 1 {
			r.Pop()
			r.Pop()
			println("popped", r.Len())
		}
		println("mixed:", i, x)
	}
}

func main() {
	empty()
	order()
	exits()
	popping()
	pushing()
	rotating()
	resetting()
	elements()
	generic()
	named()
	ranging()
	values()
}
