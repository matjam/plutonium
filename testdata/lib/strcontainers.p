package main

// Counted strings in the library's containers and in memory from the
// allocator (language spec section 6.10): every string is freed when its
// container drops it, and no string is freed twice. Run under
// --alloc=debug (internal/build/strcount_test.go), the program must end
// with no allocation live.

import (
	"pluto/alloc"
	"pluto/fmt"
	"pluto/hash"
	"pluto/strconv"
	"pluto/vec"
)

type entry struct {
	key   string
	tags  [2]string
	count int
}

func name(i int) string { return "item-" + strconv.Itoa(i) }

// firstWith returns the first key containing c, from inside a loop.
func firstWith(v *vec.Vec[entry], c byte) (string, bool) {
	for i := 0; i < v.Len(); i++ {
		e := v.At(i)
		for j := 0; j < len(e.key); j++ {
			if e.key[j] == c {
				return e.key + "!", true
			}
		}
	}
	return "", false
}

func main() {
	// A vector of strings: growth moves them, Pop, Remove, Insert,
	// SwapRemove and Truncate drop or keep them.
	words := vec.New[string]()
	for i := 0; i < 40; i++ {
		words.Push(name(i))
	}
	last, _ := words.Pop()
	gone := words.Remove(3)
	words.Insert(0, "first"+last)
	words.SwapRemove(5)
	words.Set(1, words.At(2)+"+")
	words.Truncate(30)
	fmt.Println(words.Len(), words.At(0), words.At(1), last, gone)
	words.Free()

	// A vector of structs holding strings.
	ents := vec.New[entry]()
	for i := 0; i < 10; i++ {
		ents.Push(entry{key: name(i * 7), tags: [2]string{"t" + strconv.Itoa(i), "x"}, count: i})
	}
	k, ok := firstWith(&ents, '6')
	fmt.Println(k, ok, ents.At(4).tags[0])
	ents.Clear()
	ents.Push(entry{key: "after clear"})
	fmt.Println(ents.Len(), ents.At(0).key)
	ents.Free()

	// A map from strings to strings: Set replaces, Delete drops, growth
	// moves.
	m := hash.NewMap[string, string](4)
	for i := 0; i < 50; i++ {
		m.Set(name(i%30), "v"+strconv.Itoa(i))
	}
	for i := 0; i < 30; i += 3 {
		m.Delete(name(i))
	}
	v, found := m.Get(name(4))
	fmt.Println(m.Len(), v, found)
	it := m.Iter()
	n := 0
	for {
		key, val, more := it.Next()
		if !more {
			break
		}
		if len(key) > 0 && len(val) > 0 {
			n++
		}
	}
	fmt.Println("iterated", n)
	m.Clear()
	m.Set("again", "yes")
	fmt.Println(m.Len())
	m.Free()

	set := hash.NewSet[string](2)
	for i := 0; i < 20; i++ {
		set.Add(name(i % 7))
	}
	fmt.Println("set", set.Len(), set.Has(name(3)), set.Has("none"))
	set.Free()

	// A struct from the allocator: Free releases its strings.
	p := alloc.New[entry]()
	p.key = name(99)
	p.tags[1] = p.key + "/tag"
	fmt.Println(p.tags[1])
	alloc.Free(p)

	// A ring holds strings only in its live slots.
	var r ring[string, 4]
	for i := 0; i < 3; i++ {
		r.Push(name(i))
	}
	x, _ := r.Pop()
	y, _ := r.Peek()
	fmt.Println(x, y, r.Len())
	r.Reset()
	r.Push("kept")
	z, _ := r.Pop()
	fmt.Println(z, r.Empty())

	// Range over a ring's iterator, popping what the body is given: the
	// element is held for the call.
	for i := 0; i < 4; i++ {
		r.Push(name(i + 10))
	}
	seen := ""
	for s := range r.Values() {
		popped, _ := r.Pop()
		seen += s + "=" + popped + " "
	}
	fmt.Println("ring range:", seen, r.Len())
	r.Reset()

	// Range over a container through its All method, keeping a string
	// from the loop and replacing elements as it goes.
	for i := 0; i < 4; i++ {
		words.Push(name(i))
	}
	last = ""
	for i, w := range words {
		words.Set(i, w+"!")
		last = w
		if i == 2 {
			break
		}
	}
	fmt.Println("vec range:", last, words.At(0), words.At(2), words.At(3))
	words.Free()
}
