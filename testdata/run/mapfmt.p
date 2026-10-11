package main

import "pluto/fmt"

// Printing maps with fmt: keys in sorted order, as Go's fmt prints them,
// whatever order the table holds them in.

type point struct{ X, Y int }

// A map keeps only what may live in heap memory: a slice of a package
// variable's array, not one of a literal in main's frame.
var xy = []string{"x", "y"}

type rec struct {
	Name  string
	Tags  map[string]bool
	Count int
}

func main() {
	empty := map[string]int{}
	var nilm map[string]int
	fmt.Println(empty, nilm, len(empty))
	fmt.Printf("%v %d\n", empty, nilm)

	m := map[string]int{"pear": 3, "apple": 1, "fig": 7, "banana": 2}
	fmt.Println(m)
	fmt.Printf("%v|%d|%x\n", m, m, m)
	fmt.Printf("%#v\n", m)
	fmt.Printf("%#v\n", nilm)

	ints := map[int]string{}
	for i := 20; i > -5; i -= 3 {
		ints[i] = fmt.Sprint(i * i)
	}
	fmt.Println(ints)
	fmt.Println(map[float64]bool{2.5: true, -1: false, 0: true})
	fmt.Println(map[bool]int{true: 1, false: 0})
	fmt.Println(map[point]string{{2, 1}: "b", {1, 9}: "a", {1, 2}: "c"})
	fmt.Println(map[[2]string]int{{"b", "a"}: 1, {"a", "z"}: 2})
	fmt.Println(map[any]int{3: 1, 1: 2, nil: 0})

	nested := map[string]map[int][]string{
		"b": {2: xy, 1: nil},
		"a": {},
	}
	fmt.Println(nested)
	r := rec{Name: "r", Tags: map[string]bool{"z": true, "a": false}, Count: 2}
	fmt.Printf("%v\n%+v\n", r, r)
	fmt.Println(map[string]*point{"p": nil})
	s := fmt.Sprintf("%v", map[uint8]uint16{9: 1, 3: 2})
	fmt.Println(s, len(s))
	var a any = m
	fmt.Println(a)
}
