package main

import (
	"pluto/cmp"
	"pluto/fmt"
	"pluto/slices"
)

// The slices package, checked against Go's: the functions that work in
// place within a slice's capacity give the same results, and the sorts
// (pdqsort and the stable merge sort) order elements, ties included, the
// way Go's do.

var seed uint64 = 12345

func rnd() uint64 {
	seed = seed*6364136223846793005 + 1442695040888963407
	return seed >> 33
}

type pair struct {
	key, idx int
}

func cmpPair(a, b pair) int { return cmp.Compare(a.key, b.key) }

func hash(x []int) uint64 {
	h := uint64(14695981039346656037)
	for _, v := range x {
		h ^= uint64(v)
		h *= 1099511628211
	}
	return h
}

func hashPairs(x []pair) uint64 {
	h := uint64(14695981039346656037)
	for _, v := range x {
		h ^= uint64(v.key)<<20 | uint64(v.idx)
		h *= 1099511628211
	}
	return h
}

func fill(x []int, pattern int) {
	n := len(x)
	for i := range x {
		switch pattern {
		case 0:
			x[i] = int(rnd() % 1000000)
		case 1:
			x[i] = i
		case 2:
			x[i] = n - i
		case 3:
			x[i] = 7
		case 4:
			x[i] = i % 17
		case 5:
			if i < n/2 {
				x[i] = i
			} else {
				x[i] = n - i
			}
		case 6:
			x[i] = int(rnd() % 4)
		}
	}
}

func main() {
	var buf [3000]int
	for _, n := range []int{0, 1, 2, 3, 11, 12, 13, 50, 129, 1000, 3000} {
		for pattern := 0; pattern < 7; pattern++ {
			x := buf[:n]
			fill(x, pattern)
			slices.Sort(x)
			fmt.Println("Sort", n, pattern, slices.IsSorted(x), hash(x))

			fill(x, pattern)
			slices.SortFunc(x, func(a, b int) int { return b - a })
			fmt.Println("SortFunc", n, pattern, slices.IsSortedFunc(x, func(a, b int) int { return b - a }), hash(x))

			var pairs [3000]pair
			p := pairs[:n]
			fill(buf[:n], pattern)
			for i := range p {
				p[i] = pair{buf[i] % 50, i}
			}
			slices.SortStableFunc(p, cmpPair)
			fmt.Println("SortStableFunc", n, pattern, hashPairs(p))

			for i := range p {
				p[i] = pair{buf[i] % 50, i}
			}
			slices.SortFunc(p, cmpPair)
			// pdqsort is deterministic: the order of ties is the same as Go's.
			fmt.Println("SortFunc pairs", n, pattern, hashPairs(p))
		}
	}

	fl := []float64{3, 1.5, -2, 0, 7.25, 1.5, 100}
	slices.Sort(fl)
	fmt.Println(fl, slices.Min(fl), slices.Max(fl))
	st := []string{"pear", "apple", "fig", "banana", "apple", "cherry"}
	slices.Sort(st)
	fmt.Println(st)
	i, ok := slices.BinarySearch(st, "banana")
	fmt.Println(i, ok)
	i, ok = slices.BinarySearch(st, "blueberry")
	fmt.Println(i, ok)
	i, ok = slices.BinarySearchFunc(st, "fig", func(e, t string) int { return cmp.Compare(e, t) })
	fmt.Println(i, ok)
	fmt.Println(slices.Index(st, "fig"), slices.Index(st, "kiwi"), slices.Contains(st, "pear"),
		slices.IndexFunc(st, func(s string) bool { return len(s) > 5 }))
	fmt.Println(slices.Equal(st, st), slices.Equal(st, st[1:]), slices.Compare(st, st[:3]), slices.Compare(st[:3], st), slices.Compare(st, st))

	// Insert, Delete, Replace, Compact and Reverse within capacity.
	s := make([]int, 0, 32)
	s = append(s, 1, 2, 3, 4, 5)
	s = slices.Insert(s, 2, 10, 11)
	fmt.Println(s, len(s), cap(s))
	s = slices.Insert(s, len(s), 99)
	s = slices.Insert(s, 0, s[1:3]...)
	fmt.Println(s)
	s = slices.Delete(s, 1, 4)
	fmt.Println(s, len(s))
	s = slices.Replace(s, 1, 3, 7, 7, 7, 7)
	fmt.Println(s)
	s = slices.Replace(s, 0, 5, 42)
	fmt.Println(s)
	s = slices.DeleteFunc(s, func(v int) bool { return v%2 == 1 })
	fmt.Println(s)
	s = append(s, 1, 1, 2, 2, 2, 3, 1, 1)
	s = slices.Compact(s)
	fmt.Println(s)
	slices.Reverse(s)
	fmt.Println(s)
	s = slices.CompactFunc(s, func(a, b int) bool { return a/2 == b/2 })
	fmt.Println(s, cap(slices.Clip(s)))
	fmt.Println(slices.Max([]int{3, 9, 2}), slices.Min([]string{"b", "a", "c"}),
		slices.MaxFunc([]pair{{1, 0}, {3, 1}, {3, 2}}, cmpPair), slices.MinFunc([]pair{{2, 0}, {1, 1}, {1, 2}}, cmpPair))
	fmt.Println(cmp.Compare(1, 2), cmp.Compare("b", "a"), cmp.Less(1.5, 2.5), cmp.Or(0, 0, 5, 6), cmp.Or("", "x"))

	// The iterators.
	words := []string{"a", "bb", "ccc", "dd", "e"}
	for i, w := range slices.All(words) {
		fmt.Println("All", i, w)
	}
	for i, w := range slices.Backward(words) {
		if i == 1 {
			break
		}
		fmt.Println("Backward", i, w)
	}
	total := 0
	for w := range slices.Values(words) {
		total += len(w)
	}
	fmt.Println("Values", total)
	for c := range slices.Chunk(words, 2) {
		fmt.Println("Chunk", c, len(c), cap(c))
	}
	grown := make([]int, 0, 16)
	grown = slices.AppendSeq(grown, slices.Values([]int{4, 5, 6}))
	grown = slices.AppendSeq(grown, slices.Values([]int{7}))
	fmt.Println("AppendSeq", grown, cap(grown))
}
