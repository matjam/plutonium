// pluto/fmt prints library containers as Go prints the built-in types they
// replace: a hash.Map like a map, with sorted keys, and a vec.Vec like a
// slice, alone, in structs, through pointers and nested. Enums print by
// name through their String method. The output matches Go's for the same
// program written with maps and slices, except for the type names %#v
// prints.
package main

import (
	"pluto/fmt"
	"pluto/hash"
	"pluto/vec"
)

type point struct{ X, Y int }

type Level enum {
	Low
	Mid
	High
}

type config struct {
	Name   string
	Limits hash.Map[string, int]
	Tags   vec.Vec[string]
	Level  Level
	level  Level
}

func hashPoint(p point) uint64 { return uint64(p.X*31 + p.Y) }

func main() {
	m := hash.NewMap[string, int](0)
	defer m.Free()
	for i, k := range []string{"pear", "apple", "fig", "kiwi", "banana"} {
		m[k] = i
	}
	fmt.Println(m)
	fmt.Printf("%v|%d|%#v\n", m, m, m)
	fmt.Println(&m)

	var empty hash.Map[int, bool]
	fmt.Println(empty, len(empty))

	ints := hash.NewMap[int, point](0)
	defer ints.Free()
	for _, k := range []int{30, -5, 12, 0, 7} {
		ints[k] = point{k, -k}
	}
	fmt.Printf("%v\n%+v\n", ints, ints)

	var keys hash.Map[point, string]
	keys.InitFunc(0, hashPoint)
	defer keys.Free()
	keys[point{2, 1}] = "c"
	keys[point{1, 9}] = "b"
	keys[point{1, 2}] = "a"
	fmt.Println(keys)

	v := vec.New[int]()
	defer v.Free()
	fmt.Println(v)
	for i := 1; i <= 4; i++ {
		v.Push(i * 10)
	}
	fmt.Println(v, &v)
	fmt.Printf("%v|%x|%#v|%5d\n", v, v, v, v)

	pts := vec.New[point]()
	defer pts.Free()
	pts.Push(point{1, 2})
	pts.Push(point{3, 4})
	fmt.Printf("%v %+v\n", pts, pts)

	c := config{Name: "srv", Limits: hash.NewMap[string, int](0), Tags: vec.New[string](), Level: Level.High, level: Level.Mid}
	defer c.Limits.Free()
	defer c.Tags.Free()
	c.Limits["conns"] = 100
	c.Limits["bytes"] = 4096
	c.Tags.Push("a")
	c.Tags.Push("b")
	fmt.Printf("%v\n%+v\n", c, c)
	fmt.Println(&c)
	fmt.Println(Level.Low, []Level{Level.Mid, Level.High}, [2]Level{})
}
