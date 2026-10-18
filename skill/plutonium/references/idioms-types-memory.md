# Idioms: ownership, types and memory

Complete short programs, compiled and run by the skill's test with their output checked. Rules behind them:
`ownership-and-memory.md`. Tools, text and data idioms: `idioms.md`; concurrency: `idioms-concurrency.md`.

| # | Task | Features |
|---|---|---|
| 1 | linked list | `owned[Node]`, `take`, `owner` struct |
| 2 | binary search tree | recursion over `*Node` views, `*owned[Node]` |
| 3 | enum with exhaustive switch | `enum`, `range Enum`, comma-ok conversion |
| 4 | union (Option) | `union`, generic union, type switch, assertion |
| 5 | resource guard | `owner` with `Drop`, `defer`, drop order |
| 6 | generic containers | `owner` generic type, constraints, generic method |
| 7 | scratch arena | `arena.NewFixed`, `Mark`/`Release`, `vec.New[T](&a)` |
| 8 | packed network header | `packed be [N]byte`, conversions |
| 9 | counted values in a `hash.Map` | `synced[T]` elements, `Index`, `Delete` |
| 10 | tree with weak parents | `shared[T]`, `weak`, `Upgrade` |

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

## 7. Scratch memory in an arena

Allocate with the arena as the allocator; everything dies together when the arena is reset or dropped. A mark/release pair reclaims one phase and keeps earlier results. Do not print arena-backed strings through `fmt.Println` in the function that owns the arena (chapter notes in `ownership-and-memory.md`).

```pluto-run
package main

import (
	"pluto/arena"
	"pluto/fmt"
	"pluto/strings"
	"pluto/vec"
)

type Stat struct {
	word  string
	count int
}

func tally(text string, a *arena.Fixed) int {
	words := vec.New[Stat](a) // lives in the arena
	for w := range strings.FieldsSeq(text) {
		found := false
		for i := range words {
			if p := words.Ptr(i); p.word == w {
				p.count++
				found = true
				break
			}
		}
		if !found {
			words.Push(Stat{w, 1})
		}
	}
	best := 0
	for i := range words {
		best = max(best, words.Ptr(i).count)
	}
	return best
}

func main() {
	a := arena.NewFixed(64 << 10)
	m := a.Mark()
	fmt.Println(tally("a b a c a b", &a))
	a.Release(m) // reclaim this phase; earlier allocations would stay
	fmt.Println(a.Used(), tally("x y x", &a))
}
```

```output
3
0 2
```

## 8. A packed network header

A `packed be [N]byte` type lays out fields at exact bit positions and converts to and from a byte array. Reading and writing a field is a shift and mask. Use it for protocol headers and hardware registers.

```pluto-run
package main

import "pluto/fmt"

type Header packed be [7]byte {
	kind    bits[uint8, 4]
	version bits[uint8, 4]
	length  uint16
	id      uint32
}

func main() {
	raw := [7]byte{0x21, 0x00, 0x28, 0, 0, 0, 7}
	h := Header(raw) // every bit is kept
	fmt.Println(h.version, h.kind, h.length, h.id)
	h.length = 300
	h.id++
	out := [7]byte(h)
	fmt.Println(out)
	built := Header{version: 4, kind: 1, length: 20, id: 9}
	fmt.Println([7]byte(built), built == Header(out))
}
```

```output
2 1 40 7
[33 1 44 0 0 0 8]
[65 0 20 0 0 0 9] false
```

## 9. Counted values in a hash.Map

Neither `map` nor `vec.Vec` nor `hash.Map` accepts an owner as element today (E3207); store a counted box. Reading `reg[name]` copies the reference, so `acct` below is the same account as the one in the map. Bind the copy to a variable before calling methods on it.

```pluto-run
package main

import (
	"pluto/alloc"
	"pluto/fmt"
	"pluto/hash"
	"pluto/sync"
)

type Account struct {
	mu      sync.Mutex
	balance int
}

func (a *Account) Deposit(n int) {
	a.mu.Lock()
	a.balance += n
	a.mu.Unlock()
}

func (a *Account) Balance() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.balance
}

func main() {
	reg := hash.NewMap[string, synced[Account]](8)
	for _, name := range [...]string{"ann", "bo"} {
		reg[name] = alloc.Synced[Account]() // the map holds a counted reference
	}
	acct := reg["ann"]
	acct.Deposit(50)
	other := reg["bo"]
	other.Deposit(7)
	total := 0
	for _, a := range reg {
		total += a.Balance()
	}
	fmt.Println(acct.Balance(), total, reg.Len(), reg.Has("zed"))
	reg.Delete("bo") // drops the map's reference; other still holds the account
	fmt.Println(other.Balance(), reg.Len())
}
```

```output
50 57 2 false
7 1
```

## 10. Trees with weak parent links

`shared[T]` counts references; a `weak` back pointer does not keep its parent alive, so the tree is freed when the last strong reference goes. `Upgrade` returns the strong reference while it exists.

```pluto-run
package main

import (
	"pluto/alloc"
	"pluto/fmt"
	"pluto/vec"
)

type Dir owner struct {
	name     string
	parent   weak[shared[Dir]]
	children vec.Vec[shared[Dir]]
}

func newDir(name string, parent shared[Dir]) shared[Dir] {
	d := alloc.Shared[Dir]()
	d.name = name
	if parent != nil {
		d.parent = weak[shared[Dir]](parent)
		parent.children.Push(d)
	}
	return d
}

func path(d shared[Dir]) string {
	p, ok := d.parent.Upgrade()
	if !ok {
		return "/" + d.name
	}
	return path(p) + "/" + d.name
}

func main() {
	var none shared[Dir]
	root := newDir("root", none)
	usr := newDir("usr", root)
	bin := newDir("bin", usr)
	fmt.Println(path(bin), root.children.Len())
}
```

```output
/root/usr/bin 1
```

