package main

// take(&place) moves a value out of a place and leaves the zero value
// behind (language spec section 6.4).

type Node struct {
	val  int
	name string
	kids [3]int
}

type Tree struct {
	left Node
	n    int
}

func main() {
	t := Tree{left: Node{val: 7, name: "seven", kids: [3]int{1, 2, 3}}, n: 1}
	l := take(&t.left)
	println(l.val, l.name, l.kids[2], t.left.val, t.left.name == "", t.left.kids[2], t.n)

	x := 5
	y := take(&x)
	println(x, y)

	p := &t.n
	println(take(p), t.n)

	s := []string{"a", "b"}
	first := take(&s[0])
	println(first, s[0] == "", s[1])
}
