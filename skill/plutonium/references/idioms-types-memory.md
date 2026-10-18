# Idioms: ownership and types

Complete short programs, compiled and run by the skill's test with their output checked. Rules behind them:
`ownership-and-memory.md`. Tools, text and data idioms: `idioms.md`; memory layout, arenas and counted boxes:
`idioms-memory.md`; concurrency: `idioms-concurrency.md`.

| # | Task | Features |
|---|---|---|
| 1 | linked list | `owned[Node]`, `take`, `owner` struct |
| 2 | binary search tree | recursion over `*Node` views, `*owned[Node]` |
| 3 | enum with exhaustive switch | `enum`, `range Enum`, comma-ok conversion |
| 4 | union (Option) | `union`, generic union, type switch, assertion |
| 5 | resource guard | `owner` with `Drop`, `defer`, drop order |
| 6 | generic containers | `owner` generic type, constraints, generic method |
| 7 | constructors | return a value or an `owned[T]`, never `&T{}` |
| 8 | grouping values by key (a multimap) | `map[K]shared[Group]` with an owner inside the box |
| 9 | expression tree behind an owning interface | `type Node owner interface`, `owner` nodes built with `new`, value-receiver leaf |

## 1. A linked list of owned nodes

`owned[Node]` boxes link the nodes; `take(&place)` moves a box out of a field; dropping the list drops every node.

```pluto-run
package main

import "pluto/fmt"

type Node owner struct {
	val  int
	next owned[Node]
}

type List owner struct {
	head owned[Node]
	n    int
}

func (l *List) PushFront(v int) {
	n := new(Node{val: v})
	n.next = take(&l.head)
	l.head = n
	l.n++
}

func (l *List) PopFront() (int, bool) {
	if l.head == nil {
		return 0, false
	}
	n := take(&l.head)
	l.head = take(&n.next)
	l.n--
	return n.val, true
}

func (l *List) Reverse() {
	var prev owned[Node]
	cur := take(&l.head)
	for cur != nil {
		next := take(&cur.next)
		cur.next = prev
		prev = cur
		cur = next
	}
	l.head = prev
}

func (l *List) Each(f func(int)) {
	for p := (*Node)(l.head); p != nil; p = (*Node)(p.next) {
		f(p.val)
	}
}

func main() {
	var l List
	for i := 1; i <= 4; i++ {
		l.PushFront(i)
	}
	l.Each(func(v int) { fmt.Print(v, " ") })
	fmt.Println()
	l.Reverse()
	l.Each(func(v int) { fmt.Print(v, " ") })
	fmt.Println()
	v, _ := l.PopFront()
	fmt.Println("popped", v, "left", l.n)
}
```

```output
4 3 2 1 
1 2 3 4 
popped 1 left 3
```

## 2. A binary search tree

Recursion over `*Node` views; insertion assigns an `owned[Node]` into a field, which drops the empty box that was there.

```pluto-run
package main

import "pluto/fmt"

type Node owner struct {
	key         int
	left, right owned[Node]
}

func insert(root *owned[Node], key int) {
	if *root == nil {
		*root = new(Node{key: key})
		return
	}
	if key < (*root).key {
		insert(&(*root).left, key)
	} else {
		insert(&(*root).right, key)
	}
}

func walk(n *Node, visit func(int)) {
	if n == nil {
		return
	}
	walk((*Node)(n.left), visit)
	visit(n.key)
	walk((*Node)(n.right), visit)
}

func height(n *Node) int {
	if n == nil {
		return 0
	}
	return 1 + max(height((*Node)(n.left)), height((*Node)(n.right)))
}

func main() {
	var root owned[Node]
	for _, k := range [...]int{5, 2, 8, 1, 9, 3} {
		insert(&root, k)
	}
	walk((*Node)(root), func(k int) { fmt.Print(k, " ") })
	fmt.Println("height", height((*Node)(root)))
}
```

```output
1 2 3 5 8 9 height 3
```

## 3. An enum with an exhaustive switch

A `switch` on an enum without `default` must list every member, so adding a member breaks the switch at compile time. `String()` is generated.

```pluto-run
package main

import "pluto/fmt"

type Op enum {
	Add
	Sub
	Mul
	Div
}

func apply(op Op, a, b int) (int, error) {
	switch op {
	case Add:
		return a + b, nil
	case Sub:
		return a - b, nil
	case Mul:
		return a * b, nil
	case Div:
		if b == 0 {
			return 0, fmt.Errorf("%v by zero", op)
		}
		return a / b, nil
	}
}

func main() {
	for op := range Op {
		r, err := apply(op, 12, 4)
		fmt.Println(op, r, err)
	}
	_, err := apply(Op.Div, 1, 0)
	fmt.Println(err)
	for _, n := range [...]int{2, 9} {
		if op, ok := Op(n); ok {
			fmt.Println(n, "is", op)
		} else {
			fmt.Println(n, "is not an Op")
		}
	}
}
```

```output
Add 16 <nil>
Sub 8 <nil>
Mul 48 <nil>
Div 3 <nil>
Div by zero
2 is Mul
9 is not an Op
```

## 4. A union (sum type) for results and expressions

Variants carry their payload inline. A type switch names the variants; a missing one is a compile error. The first variant is the zero value.

```pluto-run
package main

import "pluto/fmt"

type Option[T any] union {
	None
	Some T
}

func find(xs []int, want int) Option[int] {
	for i, x := range xs {
		if x == want {
			return Option[int].Some(i)
		}
	}
	return Option[int].None
}

func show(o Option[int]) string {
	switch v := o.(type) {
	case None:
		return "none"
	case Some:
		return fmt.Sprint("at ", v)
	}
}

func main() {
	fmt.Println(show(find([]int{4, 5, 6}, 6)), show(find(nil, 1)))
	var o Option[int] // zero value: the first variant
	fmt.Println(show(o))
	if i, ok := find([]int{9}, 9).(Option[int].Some); ok {
		fmt.Println("index", i)
	}
}
```

```output
at 2 none
none
index 0
```

## 5. A resource guard with Drop and defer

A type declared `owner` with a `Drop` method releases itself when its variable goes out of scope, on every path. `defer` and `Drop` run in a defined order: deferred calls first, then locals in reverse order.

```pluto-run
package main

import (
	"pluto/fmt"
	"pluto/sync"
)

type Guard owner struct {
	mu   *sync.Mutex
	name string
}

func Lock(mu *sync.Mutex, name string) Guard {
	mu.Lock()
	fmt.Println("lock", name)
	return Guard{mu: mu, name: name}
}

func (g *Guard) Drop() {
	if g.mu != nil {
		fmt.Println("unlock", g.name)
		g.mu.Unlock()
	}
}

var mu sync.Mutex

func work(fail bool) error {
	g := Lock(&mu, "work") // unlocked by g's Drop, when work returns
	defer fmt.Println("deferred print", g.name)
	if fail {
		return fmt.Errorf("failed")
	}
	fmt.Println("working")
	return nil
}

func main() {
	fmt.Println(work(false))
	fmt.Println(work(true))
}
```

```output
lock work
working
deferred print work
unlock work
<nil>
lock work
deferred print work
unlock work
failed
```

## 6. Generic containers and functions

A generic type that stores an owner is itself declared `owner`; generic functions use constraints as in Go; a generic method may have its own type parameters.

```pluto-run
package main

import (
	"pluto/cmp"
	"pluto/fmt"
	"pluto/vec"
)

type Queue[T any] owner struct {
	items vec.Vec[T]
	head  int
}

func (q *Queue[T]) Push(x T) { q.items.Push(x) }

func (q *Queue[T]) Pop() (T, bool) {
	var zero T
	if q.head >= q.items.Len() {
		return zero, false
	}
	x := q.items[q.head]
	q.head++
	return x, true
}

func Filter[T any](xs []T, keep func(T) bool) vec.Vec[T] {
	out := vec.New[T]()
	for _, x := range xs {
		if keep(x) {
			out.Push(x)
		}
	}
	return out
}

func Reduce[T, A any](xs []T, init A, f func(A, T) A) A {
	acc := init
	for _, x := range xs {
		acc = f(acc, x)
	}
	return acc
}

func MaxOf[T cmp.Ordered](xs []T) (T, bool) {
	var best T
	if len(xs) == 0 {
		return best, false
	}
	best = xs[0]
	for _, x := range xs[1:] {
		best = max(best, x)
	}
	return best, true
}

type Pair[K comparable, V any] struct {
	Key K
	Val V
}

func (p Pair[K, V]) Map[W any](f func(V) W) Pair[K, W] { return Pair[K, W]{p.Key, f(p.Val)} }

func main() {
	var q Queue[string]
	q.Push("a")
	q.Push("b")
	x, _ := q.Pop()
	fmt.Println(x)
	nums := []int{5, 12, 7, 20, 3}
	big := Filter(nums, func(n int) bool { return n > 6 })
	sum := Reduce(nums, 0, func(a, n int) int { return a + n })
	m, _ := MaxOf([]string{"pear", "apple", "zebra"})
	fmt.Println(big, sum, m)
	p := Pair[string, int]{"k", 4}.Map(func(n int) string { return fmt.Sprint(n * n) })
	fmt.Println(p)
}
```

```output
a
[12 7 20] 47 zebra
{k 16}
```

## 7. Constructors

`&T{...}` and a slice literal are memory in the constructor's frame, so returning them is E3001. Return the value
(structs copy), an `owned[T]` for a heap object that must keep its address, or a `shared`/`synced` box for sharing.
A function that must return a list of constants returns a package-level slice (static memory outlives every call).

```pluto-run
package main

import "pluto/fmt"

type Server struct {
	name string
	port int
}

func NewServer(name string) Server { return Server{name: name, port: 80} }

func NewBoxed(name string) owned[Server] { return new(Server{name: name, port: 443}) }

func (s *Server) Addr() string { return fmt.Sprintf("%s:%d", s.name, s.port) }

var defaults = []string{"a", "b"} // static: may be returned and kept anywhere

func names() []string { return defaults }

func main() {
	a := NewServer("a")
	b := NewBoxed("b")
	fmt.Println(a.Addr(), b.Addr(), names())
}
```

```output
a:80 b:443 [a b]
```

```pluto-error E3001
package main

type Server struct{ name string }

func bad(name string) *Server {
	return &Server{name: name} // a pointer into bad's frame
}

func names() []string {
	return []string{"a", "b"} // the elements live in names's frame
}

func main() {
	_ = bad("x")
	_ = names()
}
```

## 8. Grouping values by key

A map of growing lists is `map[string][]string` plus `append` in Go. Here a slice cannot grow and a map cannot
hold a `vec.Vec`, so the map holds a counted box (`shared[Group]` in one task, `synced[Group]` across tasks) whose
object owns the vector. `Group` is declared `owner` because it contains one.

```pluto-run
package main

import (
	"pluto/alloc"
	"pluto/fmt"
	"pluto/slices"
	"pluto/vec"
)

type Group owner struct{ items vec.Vec[string] }

func main() {
	groups := map[string]shared[Group]{}
	for _, kv := range [...][2]string{{"a", "x"}, {"b", "y"}, {"a", "z"}} {
		g := groups[kv[0]] // a copy of the reference, or nil
		if g == nil {
			g = alloc.Shared[Group]()
			groups[kv[0]] = g
		}
		g.items.Push(kv[1]) // groups[k].items.Push(...) works too once the key exists
	}
	keys := vec.New[string]()
	for k := range groups {
		keys.Push(k)
	}
	slices.Sort(keys.Slice())
	for _, k := range keys.Slice() {
		g := groups[k]
		fmt.Println(k, g.items)
	}
}
```

```output
a [x z]
b [y]
```

## 9. An expression tree behind an owning interface

A plain interface is a view, so it cannot hold tree nodes that outlive the function that built them. Declare the interface `owner`: it owns the value inside, nodes are declared `owner` too and built with `new(...)`, and the leaf is a plain value. (In a `return`, the compiler's hint for a pointer-receiver mismatch (E0021) suggests `new(T{...})`; elsewhere it suggests `&T{...}`, which would be E3001 in a constructor.) Reassigning `left = new(Bin{..., left, ...})` moves `left` into the new node.

```pluto-run
package main

import "pluto/fmt"

type Node owner interface {
	Eval() int
	String() string
}

type Num int

type Bin owner struct {
	op          byte
	left, right Node
}

func (n Num) Eval() int      { return int(n) }
func (n Num) String() string { return fmt.Sprint(int(n)) }

func (b *Bin) Eval() int {
	l, r := b.left.Eval(), b.right.Eval()
	if b.op == '+' {
		return l + r
	}
	return l * r
}

func (b *Bin) String() string {
	return "(" + b.left.String() + " " + string(rune(b.op)) + " " + b.right.String() + ")"
}

type Parser struct {
	src string
	pos int
}

func (p *Parser) peek() byte {
	if p.pos < len(p.src) {
		return p.src[p.pos]
	}
	return 0
}

func (p *Parser) expr() Node {
	left := p.term()
	for p.peek() == '+' {
		p.pos++
		left = new(Bin{'+', left, p.term()})
	}
	return left
}

func (p *Parser) term() Node {
	left := p.factor()
	for p.peek() == '*' {
		p.pos++
		left = new(Bin{'*', left, p.factor()})
	}
	return left
}

func (p *Parser) factor() Node {
	c := p.peek()
	p.pos++
	if c == '(' {
		e := p.expr()
		p.pos++ // the closing parenthesis
		return e
	}
	return Num(c - '0')
}

func main() {
	for _, src := range []string{"1+2*3", "(1+2)*3"} {
		p := Parser{src: src}
		t := p.expr()
		fmt.Println(t, "=", t.Eval())
	}
}
```

```output
(1 + (2 * 3)) = 7
((1 + 2) * 3) = 9
```
