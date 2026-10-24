package escape2

import (
	"strings"
	"testing"
)

// deepLoadSrc keeps what a captured slice holds, three closure records
// down: newNode appends to nodes, growing it into heap memory (a copy of
// its elements, which keeps them), and f reaches newNode through walk,
// outer and newValue. The summaries of walk and outer follow the records
// past the three loads of section 18.3, and what is reachable past the
// limit is kept: what nodes holds, never nodes itself, whose address
// newNode's record holds. NODE is what newNode appends.
const deepLoadSrc = `package main

import (
	"pluto/alloc"
	"unsafe"
)

type node struct{ n int }

func grow(s []*node) []*node {
	n := 2*len(s) + 1
	p := alloc.Raw(uintptr(n)*unsafe.Sizeof(s[0]), unsafe.Alignof(s[0]))
	ns := unsafe.Slice((**node)(p), n)
	copy(ns, s)
	return ns[:len(s)+1]
}

func f() int {
	var local node
	var nodes []*node
	newNode := func(k int) *node {
		n := NODE
		n.n = k
		nodes = grow(nodes)
		nodes[len(nodes)-1] = n
		return n
	}
	newValue := func(k int) *node { return newNode(k) }
	outer := func(k int) { _ = newValue(k) }
	walk := func(k int) { outer(k) }
	for i := 0; i < 3; i++ {
		walk(i)
	}
	_ = local
	return len(nodes)
}

func main() { _ = f() }
`

// TestDeepLoadPastLimits: a load past the limits keeps what it reaches,
// not the pointer it loads through (solve/rules.go, "Paths past the
// limits"). With a frame pointer among the nodes the program is rejected.
func TestDeepLoadPastLimits(t *testing.T) {
	for _, c := range []struct {
		node     string
		rejected bool
	}{
		{"alloc.New[node]()", false},
		{"&local", true},
	} {
		src := strings.Replace(deepLoadSrc, "NODE", c.node, 1)
		err := Check(loadSrc(t, src))
		if isInternal(err) && err != nil {
			t.Fatalf("%s: %v", c.node, err)
		}
		if got := len(sites(err)) > 0; got != c.rejected {
			t.Errorf("%s: rejected %v, want %v (%v)", c.node, got, c.rejected, err)
		}
	}
}
