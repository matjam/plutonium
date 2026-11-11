package main

type Node struct{ v int }

type Holder owner struct {
	b owned[Node]
}

type Counted owner struct {
	s shared[Node]
}

//pu:extern take_owned
func takeOwned(b owned[Node])

//pu:extern give_shared
func giveShared() shared[Node]

//pu:extern take_synced
func takeSynced(b synced[Node]) int32

//pu:extern take_weak
func takeWeak(w weak[shared[Node]])

//pu:extern take_holder
func takeHolder(h Holder)

//pu:extern take_counted
func takeCounted(c Counted)

//pu:extern take_ring
func takeRing(r ring[int32, 4])

//pu:export plu_box
func box(b owned[Node]) int32 { return 0 }

//pu:export plu_counted
func counted() Counted { return Counted{} }

func main() {}
