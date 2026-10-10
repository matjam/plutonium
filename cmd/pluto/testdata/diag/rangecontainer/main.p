package main

import (
	"pluto/hash"
	"pluto/iter"
)

// table can be indexed but not ranged over.
type table struct{}

func (t *table) Index(k string) int { return 0 }

// list has an All method of the wrong form.
type list struct{}

func (l *list) All() []int { return nil }

// seq has an All method with a pointer receiver.
type seq struct{}

func (s *seq) All() iter.Seq[int] {
	return func(yield func(int) bool) {}
}

func load() hash.Map[string, int] { return hash.Map[string, int]{} }
func get() seq                    { return seq{} }

func main() {
	var t table
	for k := range t {
		_ = k
	}
	var l list
	for x := range l {
		_ = x
	}
	for x := range get() {
		_ = x
	}
	for k, v := range load() {
		_, _ = k, v
	}
}
