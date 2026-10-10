package main

// Range over the standard library's iterators: pluto/iter's adaptors,
// vec.Vec, hash.Map and Set, and strings and bytes as in Go 1.24.

import (
	"pluto/bytes"
	"pluto/hash"
	"pluto/iter"
	"pluto/sort"
	"pluto/strings"
	"pluto/vec"
	"unsafe"
)

func count(n int) iter.Seq[int] {
	return func(yield func(int) bool) {
		for i := 0; i < n; i++ {
			if !yield(i) {
				return
			}
		}
	}
}

func even(x int) bool { return x%2 == 0 }

func square(x int) int { return x * x }

func add(sum, x int) int { return sum + x }

func swap(k, v int) (int, int) { return v, k }

func seqs() {
	print("filter/map:")
	for x := range iter.Map(square, iter.Filter(even, count(10))) {
		print(" ", x)
	}
	println()

	print("limit:")
	for x := range iter.Limit(count(100), 4) {
		print(" ", x)
	}
	for x := range iter.Limit(count(100), 0) {
		print(" never", x)
	}
	println()

	print("concat:")
	for x := range iter.Concat(count(2), count(0), count(3)) {
		print(" ", x)
	}
	println()

	print("enumerate/map2/limit2:")
	for k, v := range iter.Limit2(iter.Map2(swap, iter.Enumerate(iter.Map(square, count(10)))), 3) {
		print(" ", k, ":", v)
	}
	println()

	pairs := iter.Concat2(iter.Enumerate(count(2)), iter.Enumerate(count(2)))
	print("concat2/filter2/keys/values:")
	for k := range iter.Keys(iter.Filter2(func(k, v int) bool { return k == v }, pairs)) {
		print(" k", k)
	}
	for v := range iter.Values(pairs) {
		print(" v", v)
	}
	println()

	println("reduce", iter.Reduce(add, 100, count(5)), "count", iter.Count(count(7)))
	var out [4]int
	n := iter.Fill(out[:], iter.Map(square, count(10)))
	println("fill", n, out[0], out[1], out[2], out[3])
	n = iter.Fill(out[:], count(2))
	println("fill short", n)
	n = iter.Fill(out[:0], count(2))
	println("fill none", n)

	// Stopping early stops every adaptor in the chain.
	total := 0
	for x := range iter.Map(square, iter.Filter(even, count(1000))) {
		if x > 50 {
			break
		}
		total += x
	}
	println("early", total)
}

func vecs() {
	v := vec.New[int]()
	defer v.Free()
	for i := range count(6) {
		v.Push(i * 10)
	}
	print("all:")
	for i, x := range v.All() {
		print(" ", i, "=", x)
	}
	println()
	print("values:")
	for x := range v.Values() {
		print(" ", x)
	}
	println()
	print("backward:")
	for i, x := range v.Backward() {
		print(" ", i, "=", x)
		if i == 2 {
			break
		}
	}
	println()

	// As over a slice, the loop reads w's length once, when it starts:
	// elements pushed during the loop are not visited, even when w grows
	// into new storage, and elements set ahead of the loop are.
	w := vec.New[int]()
	defer w.Free()
	w.Push(1)
	w.Push(3)
	print("growing:")
	for i, x := range w {
		print(" ", x)
		for k := 0; k < 4; k++ {
			w.Push(x * 2)
		}
		if i == 0 {
			w.Set(1, 5)
		}
	}
	println(" len", w.Len())
	// An index loop visits what it pushes: a worklist.
	print("worklist:")
	for i := 0; i < w.Len() && i < 4; i++ {
		x := w.At(i)
		print(" ", x)
		if x < 100 {
			w.Insert(i+1, x*10)
		}
	}
	println(" len", w.Len())
	// Removing from the back while walking stops the loop below the next
	// index.
	print("shrinking:")
	for i, x := range w.Backward() {
		print(" ", i, "=", x)
		if i == w.Len()-2 {
			w.Truncate(3)
		}
	}
	print(" forward:")
	for i, x := range w.All() {
		print(" ", i, "=", x)
		w.Truncate(1)
	}
	println(" len", w.Len())

	c := vec.Collect(iter.Filter(even, count(9)))
	defer c.Free()
	c.AppendSeq(count(3))
	print("collect:")
	for x := range c.Values() {
		print(" ", x)
	}
	println()
}

func maps() {
	m := hash.NewMap[string, int](4)
	defer m.Free()
	for i, w := range []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine"} {
		m.Set(w, i)
	}

	// All, Keys, Values and Iter agree on the order.
	var keys [10]string
	var vals [10]int
	n := 0
	for k, v := range m.All() {
		keys[n], vals[n] = k, v
		n++
	}
	same := n == m.Len()
	i := 0
	for k := range m.Keys() {
		same = same && k == keys[i]
		i++
	}
	i = 0
	for v := range m.Values() {
		same = same && v == vals[i]
		i++
	}
	it := m.Iter()
	i = 0
	for k, v, ok := it.Next(); ok; k, v, ok = it.Next() {
		same = same && k == keys[i] && v == vals[i]
		i++
	}
	println("map entries", n, "same order", same)
	sorted := keys[:n]
	sort.Strings(sorted)
	print("keys:")
	for _, k := range sorted {
		print(" ", k)
	}
	println()

	// Setting the current key's value during the loop is allowed.
	for k, v := range m.All() {
		m.Set(k, v*100)
	}
	sum := 0
	for v := range m.Values() {
		sum += v
	}
	println("sum after set", sum)

	// Stop at the first odd value found.
	found := false
	for _, v := range m.All() {
		if v%200 == 100 {
			found = true
			break
		}
	}
	println("found odd", found)

	m2 := hash.NewMap[int, int](4)
	defer m2.Free()
	m2.Insert(iter.Enumerate(iter.Map(square, count(5))))
	keysum, valsum := 0, 0
	for k, v := range m2.All() {
		keysum += k
		valsum += v
	}
	println("insert", m2.Len(), keysum, valsum)

	s := hash.NewSet[int](4)
	defer s.Free()
	s.Insert(iter.Filter(even, count(10)))
	s.Insert(count(3))
	total := 0
	for x := range s.All() {
		total += x
	}
	println("set", s.Len(), total)

	var empty hash.Map[string, int]
	for k := range empty.All() {
		println("never", k)
	}
}

func text() {
	print("lines:")
	for line := range strings.Lines("one\ntwo\n\nthree") {
		print(" [", strings.TrimSuffix(line, "\n"), "]", len(line))
	}
	println()
	for line := range strings.Lines("") {
		println("never", line)
	}

	print("split:")
	for part := range strings.SplitSeq("a,b,,c", ",") {
		print(" [", part, "]")
	}
	println()
	print("split empty:")
	for part := range strings.SplitSeq("", ",") {
		print(" [", part, "]")
	}
	println()
	print("split runes:")
	for part := range strings.SplitSeq("héllo", "") {
		print(" [", part, "]")
	}
	println()
	print("split after:")
	for part := range strings.SplitAfterSeq("a,b,c", ",") {
		print(" [", part, "]")
	}
	println()
	print("fields:")
	for f := range strings.FieldsSeq("  the quick\tbrown\n fox  ") {
		print(" [", f, "]")
	}
	println()
	print("fields func:")
	for f := range strings.FieldsFuncSeq("a1b22c333", func(r rune) bool { return r >= '0' && r <= '9' }) {
		print(" [", f, "]")
	}
	println()

	var buf [64]byte
	data := buf[:copy(buf[:], "x=1;y=22;z=333\nnext line\n")]
	print("bytes lines:")
	for line := range bytes.Lines(data) {
		print(" ", len(line), "/", cap(line))
	}
	println()
	buf[40] = ';'
	semi := buf[40:41]
	print("bytes split:")
	for part := range bytes.SplitSeq(data[:14], semi) {
		print(" [", str(part), "]")
	}
	println()
	print("bytes split after:")
	for part := range bytes.SplitAfterSeq(data[:14], semi) {
		print(" [", str(part), "]")
	}
	println()
	print("bytes fields:")
	for f := range bytes.FieldsSeq(data) {
		print(" [", str(f), "]")
	}
	println()
	print("bytes fields func:")
	for f := range bytes.FieldsFuncSeq(data[:14], func(r rune) bool { return r == '=' || r == ';' }) {
		print(" [", str(f), "]")
	}
	println()
}

// str views b as a string.
func str(b []byte) string { return unsafe.String(unsafe.SliceData(b), len(b)) }

func main() {
	seqs()
	vecs()
	maps()
	text()
}
