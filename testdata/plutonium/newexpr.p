package main

// new(expr) allocates a variable holding expr's value (Go 1.26), and new
// returns its owner, an owned[T] (language spec section 4.2): the variable
// is freed when the owner is dropped.

type O struct {
	a, b int
	s    string
}

type Big struct {
	a, b, c, d, e, f, g int
}

type Node owner struct {
	val  int
	next owned[Node]
}

const typed int8 = -7

var calls int

var nums = [3]int{7, 8, 9}

func next() int {
	calls++
	return calls * 10
}

func pair() Big { return Big{1, 2, 3, 4, 5, 6, 7} }

// box allocates a copy of a value of any type.
func box[T any](v T) owned[T] { return new(v) }

// zero allocates a zero T.
func zero[T any]() owned[T] { return new(T) }

// push returns a node that outlives push's frame. The old head moves into
// the new node.
func push(head owned[Node], v int) owned[Node] {
	return new(Node{val: v, next: head})
}

func main() {
	// Composite literals, small and large.
	q := new(O{a: 3, b: 4, s: "y"})
	println(q.a, q.b, q.s)
	b := new(Big{a: 1, g: 9})
	println(b.a, b.b, b.g)
	arr := new([4]int{1, 2, 3, 4})
	println(arr[0], arr[3], len(arr))

	// Constants: untyped of each default type, and typed.
	n := new(42)
	println(*n)
	*n++
	println(*n)
	s := new("hello")
	println(*s, len(*s))
	r := new('x')
	println(*r)
	t := new(true)
	println(*t)
	c := new(typed)
	println(*c)
	u := new(uint16(65535))
	*u++
	println(*u)

	// Variables: the new variable holds a copy.
	v := O{a: 1, b: 2, s: "v"}
	p := new(v)
	v.a = 100
	p.b = 200
	println(v.a, v.b, p.a, p.b, p.s)
	x := 5
	px := new(x)
	x = 6
	println(x, *px)
	big := Big{g: 77}
	pb := new(big)
	big.g = 0
	println(pb.g, big.g)

	// Each call gives a distinct variable.
	p1, p2 := new(x), new(x)
	*p1 = 1
	println(*p1, *p2, p1 != p2)

	// The operand is evaluated once.
	f := new(next())
	println(*f, calls)
	g := new(pair())
	println(g.a, g.g)

	// Slices and pointers as the value.
	sl := new(nums[:]) // a view of static memory; a slice literal would be in the frame
	println(len(*sl), (*sl)[2])
	pp := new(*q)
	println((*pp).s, *pp == *q)

	// Generic code: a value of type parameter type, and a type parameter.
	bo := box(O{a: 9, s: "boxed"})
	bi := box(11)
	bz := zero[Big]()
	println(bo.a, bo.s, *bi, bz.g)

	// new(T{...}) escaping its frame, as &T{...} would.
	var head owned[Node]
	for i := 1; i <= 3; i++ {
		head = push(head, i)
	}
	for e := (*Node)(head); e != nil; e = (*Node)(e.next) {
		print(e.val, " ")
	}
	println()
}
