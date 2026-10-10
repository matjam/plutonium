package main

import (
	"pluto/fmt"
	"pluto/sort"
)

var seed uint64 = 12345

func rnd() uint64 {
	seed = seed*6364136223846793005 + 1442695040888963407
	return seed >> 33
}

type pair struct {
	key, idx int
}

func lessPair(a, b pair) bool { return a.key < b.key }
func greater(a, b int) bool   { return a > b }
func cmpPair(p pair, k int) int {
	return sort.Compare(p.key, k)
}

func sum(x []int) uint64 {
	var h uint64
	for i, v := range x {
		h = h*31 + uint64(v)*uint64(i+1)
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
	var buf [5000]int
	sizes := []int{0, 1, 2, 3, 7, 12, 13, 50, 51, 100, 1000, 5000}
	for _, n := range sizes {
		for p := 0; p < 7; p++ {
			x := buf[:n]
			fill(x, p)
			sort.Sort(x)
			a := sum(x)
			s1 := sort.IsSorted(x)
			fill(x, p)
			sort.SortFunc(x, greater)
			b := sum(x)
			s2 := sort.IsSortedFunc(x, greater)
			fill(x, p)
			sort.SortStable(x)
			c := sum(x)
			fmt.Printf("n=%d p=%d %d %t %d %t %d %t\n", n, p, a, s1, b, s2, c, sort.IntsAreSorted(x))
		}
	}
	var pairs [3000]pair
	for i := range pairs {
		pairs[i] = pair{int(rnd() % 50), i}
	}
	ps := pairs[:]
	sort.SortStableFunc(ps, lessPair)
	ok := true
	for i := 1; i < len(ps); i++ {
		if ps[i-1].key > ps[i].key || ps[i-1].key == ps[i].key && ps[i-1].idx > ps[i].idx {
			ok = false
		}
	}
	fmt.Printf("stable ok=%t first=%d,%d last=%d,%d\n", ok, ps[0].key, ps[0].idx, ps[2999].key, ps[2999].idx)
	for i := range pairs {
		pairs[i] = pair{int(rnd() % 50), i}
	}
	sort.SortFunc(ps, lessPair)
	var h uint64
	for _, p := range ps {
		h = h*31 + uint64(p.idx)
	}
	fmt.Printf("unstable %d %t\n", h, sort.IsSortedFunc(ps, lessPair))
	i, found := sort.BinarySearchFunc(ps, 25, cmpPair)
	fmt.Printf("bsf %d %t\n", i, found)

	nan := 0.0
	nan = nan / nan
	fl := []float64{3.5, -1, nan, 2, 0, nan, -7.25, 100}
	sort.Float64s(fl)
	for _, f := range fl {
		fmt.Printf("%v ", f)
	}
	fmt.Printf("%t\n", sort.Float64sAreSorted(fl))
	ss := []string{"pear", "apple", "fig", "", "banana", "apple2", "Zebra"}
	sort.Strings(ss)
	for _, s := range ss {
		fmt.Printf("%s,", s)
	}
	fmt.Printf("%t %d %d\n", sort.StringsAreSorted(ss), sort.SearchStrings(ss, "fig"), sort.SearchStrings(ss, "zzz"))
	in := []int{1, 3, 3, 3, 7, 9}
	for _, t := range []int{0, 1, 3, 4, 9, 10} {
		i, f := sort.BinarySearch(in, t)
		fmt.Printf("%d:%d,%t,%d ", t, i, f, sort.SearchInts(in, t))
	}
	fmt.Printf("\n%d %d\n", sort.SearchFloat64s(fl, 2), sort.Search(100, func(i int) bool { return i*i >= 50 }))
	idx, fnd := sort.Find(len(in), func(i int) int { return 7 - in[i] })
	fmt.Printf("%d %t\n", idx, fnd)
	u8 := []uint8{200, 3, 255, 0, 17}
	sort.Sort(u8)
	sort.Reverse(u8)
	for _, b := range u8 {
		fmt.Printf("%d ", b)
	}
	fmt.Printf("%d %d %d\n", sort.Compare(1, 2), sort.Compare(nan, 1.0), sort.Compare("b", "a"))
}
