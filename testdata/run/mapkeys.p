package main

// Map key and element types: every comparable kind of key, and elements
// that hold strings, slices, interfaces, functions and pointers.

type ID int32

type name struct {
	first, last string
}

type mixed struct {
	n    int8
	f    float64
	s    string
	i    any
	arr  [2]uint16
	flag bool
}

type blank struct {
	a int
	_ int
	b string
}

type shape interface{ area() int }

type sq struct{ side int }

func (s sq) area() int { return s.side * s.side }

type rect struct{ w, h int }

func (r rect) area() int { return r.w * r.h }

// A map holds what may live in heap memory, so the pointers, slices and
// boxed interface values below point at package variables: a map cannot
// keep a view of a function's frame, and an interface holding a value
// larger than a word is boxed in the frame that converts it, except in a
// package variable's initializer.
var (
	objs    [4]int
	letters       = [3]string{"x", "y", "z"}
	keyName any   = name{"a", "b"}
	keyArr  any   = [2]int{1, 2}
	keyStr  any   = "1"
	sqShape shape = sq{2}
	rectSh  shape = rect{2, 2}
	valOne  any   = "one"
	valName any   = name{"n", "m"}
)

func joined(m map[string][]string, k string) string {
	s := ""
	for _, x := range m[k] {
		s += x
	}
	return s
}

func main() {
	// Integer keys of every size, signed and named.
	i8 := map[int8]int{-128: 1, 127: 2}
	u16 := map[uint16]int{65535: 3}
	i64 := map[int64]int{-1 << 62: 4}
	ids := map[ID]string{-1: "minus", 1 << 20: "big"}
	runes := map[rune]bool{'é': true}
	bytes := map[byte]int{'a': 5}
	println(i8[-128], i8[127], u16[65535], i64[-1<<62], ids[-1], ids[1<<20], runes['é'], bytes['a'])

	// Booleans and pointers.
	bm := map[bool]string{true: "yes", false: "no"}
	pm := map[*int]int{}
	for i := range objs {
		pm[&objs[i]] = i
	}
	println(bm[true], bm[false], len(pm), pm[&objs[2]], pm[nil])

	// Floats: 0 and -0 are one key; NaN is never found and every store
	// adds an entry.
	fm := map[float64]string{}
	negz := 0.0
	negz = -negz
	fm[0] = "zero"
	fm[negz] = "negative zero"
	fm[1.5] = "x"
	zero := 0.0
	nan := zero / zero
	fm[nan] = "a"
	fm[nan] = "b"
	_, ok := fm[nan]
	println(len(fm), fm[0], ok)
	f32 := map[float32]int{0.25: 1}
	println(f32[0.25])

	// Strings, including empty and long ones that share prefixes.
	sm := map[string]int{"": 1}
	long := ""
	for i := 0; i < 40; i++ {
		long += "ab"
		sm[long] = i
	}
	println(sm[""], sm["abab"], sm[long], len(sm))

	// Structs of strings, arrays, mixed structs with interfaces and floats.
	people := map[name]int{{"Ada", "Lovelace"}: 1815, {"Alan", "Turing"}: 1912}
	first := "Ada"
	println(people[name{first, "Lovelace"}], people[name{"Alan", "Turing"}], people[name{"Alan", "Kay"}])
	grid := map[[2]int]string{{0, 0}: "origin", {1, -1}: "se"}
	println(grid[[2]int{1, -1}], grid[[2]int{0, 0}], len(grid))
	bl := map[blank]int{{a: 1, b: "x"}: 1}
	bl[blank{a: 1, b: "x"}]++
	println(len(bl), bl[blank{a: 1, b: "x"}])
	strs := map[[3]string]int{{"a", "b", "c"}: 1}
	println(strs[[3]string{"a", "b", "c"}], strs[[3]string{"a", "b", ""}])
	mk := map[mixed]int{}
	mk[mixed{n: 1, f: 2.5, s: "x", i: 7, arr: [2]uint16{1, 2}, flag: true}] = 1
	mk[mixed{n: 1, f: 2.5, s: "x", i: int64(7), arr: [2]uint16{1, 2}, flag: true}] = 2
	mk[mixed{n: 1, f: negz, s: "x", i: 7, arr: [2]uint16{1, 2}, flag: true}] = 3
	println(len(mk), mk[mixed{n: 1, f: 2.5, s: "x", i: 7, arr: [2]uint16{1, 2}, flag: true}],
		mk[mixed{n: 1, f: 0, s: "x", i: 7, arr: [2]uint16{1, 2}, flag: true}])

	// Interface keys: dynamic types tell keys apart.
	am := map[any]string{}
	am[1] = "int"
	am[int64(1)] = "int64"
	am[keyStr] = "string"
	am[keyName] = "struct"
	am[nil] = "nil"
	am[keyArr] = "array"
	println(len(am), am[1], am[int64(1)], am["1"], am[name{"a", "b"}], am[nil], am[[2]int{1, 2}], am[2] == "")
	shapes := map[shape]string{sqShape: "square", rectSh: "rect"}
	println(shapes[sq{2}], shapes[rect{2, 2}], len(shapes))
	var sh shape = sq{2}
	println(shapes[sh])

	// Elements that hold counted and other values.
	lists := map[string][]string{}
	lists["xyz"] = letters[:]
	lists["xy"] = letters[:2]
	println(joined(lists, "xyz"), joined(lists, "xy"), len(lists["none"]))
	ifaces := map[int]any{1: valOne, 2: 2, 3: valName}
	println(ifaces[1].(string), ifaces[2].(int), ifaces[3].(name).last, ifaces[4] == nil)
	funcs := map[string]func(int) int{"double": func(x int) int { return 2 * x }}
	println(funcs["double"](21), funcs["none"] == nil)
	ptrs := map[int]*int{1: &objs[1]}
	*ptrs[1] = 99
	println(objs[1])
}
