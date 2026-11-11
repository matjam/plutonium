package main

type Node struct{ v int }

type Counted owner struct {
	s shared[Node]
	w weak[shared[Node]]
}

//pu:export plu_owned
func takeOwned(b *owned[Node]) int32 { return 0 }

//pu:export plu_counted
func takeCounted(c *Counted) int32 { return 0 }

//pu:export plu_ring
func takeRing(r *ring[int32, 4]) int32 { return 0 }

func main() {
	b := new(Node)
	println(takeOwned(&b))
}
