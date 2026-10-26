package main
type Point struct{ x, y int }

type Node struct {
	name string
	next *Node
}

var b = Node{name: "b"}
var a = Node{name: "a", next: &b}

func walk(n *Node, p Point, done chan string) {
	s := ""
	for ; n != nil; n = n.next {
		s += n.name
	}
	done <- s
}

func main() {
	done := make(chan string)
	list := &a
	task walk(list, Point{1, 2}, done)
	println(<-done)
	ch := make(chan *Node, 1)
	ch <- list
	println((<-ch).name)
}