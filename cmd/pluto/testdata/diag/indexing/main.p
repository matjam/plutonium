package main

import (
	"pluto/hash"
	"pluto/vec"
)

type point struct{ x, y int }

type table struct{}

func (t *table) Index(k string) int { return 0 }

func load() hash.Map[string, int] { return hash.Map[string, int]{} }

func main() {
	v := vec.New[point]()
	defer v.Free()
	v.Push(point{1, 2})
	p := &v[0]
	v[0].x = 3
	_ = p

	var t table
	t["a"] = 1
	n, ok := t["b"]
	_, _ = n, ok

	m := hash.NewMap[string, int](0)
	defer m.Free()
	for k := range m {
		_ = k
	}
	_ = load()["a"]
}
