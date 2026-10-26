package main

// A view of a box's object may not outlive the box it was made through
// (language spec section 6.5, rule 5).

type Node struct{ val int }

func (n *Node) Get() int { return n.val }

type Getter interface{ Get() int }

var kept *Node

// The owned box n is dropped when field returns.
func field() *int {
	n := new(Node)
	return &n.val
}

// A *Node view of the box, kept in a package variable.
func keep() {
	n := new(Node)
	kept = (*Node)(n)
}

// An ordinary interface only lends the box it holds.
func lend() Getter {
	n := new(Node)
	return n
}

// The box is a temporary, dropped at the end of the statement.
func temp() *Node {
	return (*Node)(new(Node))
}

func main() {
	_ = field()
	keep()
	_ = lend()
	_ = temp()
}
