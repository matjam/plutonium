package main

import "unsafe"

// Language features the standard library leans on: goto out of a loop,
// generic types with pointer-receiver methods, explicit instantiation,
// interface assertions on type parameters, three-index slices, clear,
// min/max, unsafe views, parallel assignment through slices, and function
// values passed down and stored in structs.

func equalFoldASCII(s, t string) bool {
	i := 0
	for ; i < len(s) && i < len(t); i++ {
		a, b := s[i], t[i]
		if a|b >= 0x80 {
			goto slow
		}
		if a == b {
			continue
		}
		if a > b {
			a, b = b, a
		}
		if 'A' <= a && a <= 'Z' && b == a+'a'-'A' {
			continue
		}
		return false
	}
	return len(s) == len(t)
slow:
	return s[i:] == t[i:]
}

type stack[T any] struct {
	data []T
}

func (s *stack[T]) push(x T) {
	if len(s.data) == cap(s.data) {
		panic("full")
	}
	s.data = append(s.data, x)
}

func (s *stack[T]) pop() (T, bool) {
	var zero T
	n := len(s.data)
	if n == 0 {
		return zero, false
	}
	x := s.data[n-1]
	s.data[n-1] = zero
	s.data = s.data[:n-1]
	return x, true
}

func sizeOf[T any](n int) uintptr {
	var zero T
	return uintptr(n) * unsafe.Sizeof(zero)
}

type hasher interface{ hash() uint64 }

type key struct{ a, b uint32 }

func (k key) hash() uint64 { return uint64(k.a)<<32 | uint64(k.b) }

func hashOf[K comparable](k K) (uint64, bool) {
	if h, ok := any(k).(hasher); ok {
		return h.hash(), true
	}
	switch v := any(k).(type) {
	case int:
		return uint64(v), true
	case string:
		return uint64(len(v)), true
	}
	return 0, false
}

type iter struct {
	s string
	f func(byte) bool
}

func (it *iter) next() (string, bool) {
	i := 0
	for i < len(it.s) && it.f(it.s[i]) {
		i++
	}
	if i == len(it.s) {
		return "", false
	}
	j := i
	for j < len(it.s) && !it.f(it.s[j]) {
		j++
	}
	w := it.s[i:j]
	it.s = it.s[j:]
	return w, true
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' }

func reverse[T any](s []T) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

func insertionSort[T any](data []T, less func(a, b T) bool) {
	for i := 1; i < len(data); i++ {
		for j := i; j > 0 && less(data[j], data[j-1]); j-- {
			data[j], data[j-1] = data[j-1], data[j]
		}
	}
}

type pair struct{ k, v int }

func main() {
	println("fold", equalFoldASCII("Hello", "hELLO"), equalFoldASCII("abc", "abd"), equalFoldASCII("x\xffy", "x\xffy"), equalFoldASCII("x\xffy", "X\xffz"))

	var backing [4]int
	st := stack[int]{data: backing[:0]}
	for i := 1; i <= 4; i++ {
		st.push(i * 10)
	}
	a, ok1 := st.pop()
	b, ok2 := st.pop()
	println("stack", a, ok1, b, ok2, len(st.data), cap(st.data), backing[3])

	println("sizeOf", sizeOf[int32](10), sizeOf[pair](3), sizeOf[struct{}](5))

	h1, ok1 := hashOf(key{1, 2})
	h2, ok2 := hashOf(42)
	h3, ok3 := hashOf("four")
	_, ok4 := hashOf(3.5)
	println("hash", h1, ok1, h2, ok2, h3, ok3, ok4)

	it := iter{s: "  the quick\tbrown  fox ", f: isSpace}
	for w, ok := it.next(); ok; w, ok = it.next() {
		println("word", w, len(w))
	}

	xs := []int{5, 1, 4, 2, 3, 9, 0}
	full := xs[1:3:4]
	println("3index", len(full), cap(full), full[0], full[1])
	reverse(xs)
	println("reversed", xs[0], xs[1], xs[6])
	insertionSort(xs, func(a, b int) bool { return a < b })
	println("sorted", xs[0], xs[1], xs[2], xs[3], xs[4], xs[5], xs[6])
	ps := []pair{{3, 0}, {1, 1}, {3, 2}, {2, 3}, {1, 4}}
	insertionSort(ps, func(a, b pair) bool { return a.k < b.k })
	for _, p := range ps {
		print(p.k, ":", p.v, " ")
	}
	println()
	clear(xs[2:5])
	println("clear", xs[1], xs[2], xs[4], xs[5], min(3, 1, 2), max(25, -1, 7), min("b", "a"))

	buf := [8]byte{'h', 'e', 'l', 'l', 'o'}
	s := unsafe.String(&buf[0], 5)
	view := unsafe.Slice(unsafe.StringData(s), len(s))
	p := unsafe.Pointer(unsafe.SliceData(view[1:]))
	println("unsafe", s, len(view), *(*byte)(p), *(*byte)(unsafe.Add(p, 2)))
	buf[0] = 'j'
	println("aliased", s)

	var words [3]string
	n := 0
	rest := "a,b,c,d"
	for n < len(words)-1 {
		i := 0
		for i < len(rest) && rest[i] != ',' {
			i++
		}
		if i == len(rest) {
			break
		}
		words[n] = rest[:i]
		rest = rest[i+1:]
		n++
	}
	words[n] = rest
	println("splitn", n+1, words[0], words[1], words[2])
}
