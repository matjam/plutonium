# Idioms: memory, layout and counted boxes

Complete short programs, compiled and run by the skill's test with their output checked. Rules behind them:
`ownership-and-memory.md` and `kernel-mode.md`. Other idioms: `idioms.md`, `idioms-types-memory.md`,
`idioms-concurrency.md`.

| # | Task | Features |
|---|---|---|
| 1 | scratch arena | `arena.NewFixed`, `Mark`/`Release`, `vec.New[T](&a)` |
| 2 | packed network header | `packed be [N]byte`, conversions |
| 3 | counted values in a `hash.Map` | `synced[T]` elements, `Index`, `Delete` |
| 4 | tree with weak parents | `shared[T]`, `weak`, `Upgrade` |
| 5 | LRU cache | `map[K]shared[Node]`, `weak` back links, O(1) unlink |

## 1. Scratch memory in an arena

Allocate with the arena as the allocator; everything dies together when the arena is reset or dropped. A mark/release pair reclaims one phase and keeps earlier results. Converting arena-backed text to an interface (`fmt.Println(b.String())`) in the function that owns the arena is E3313; write its bytes instead (chapter notes in `ownership-and-memory.md`).

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

## 2. A packed network header

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

## 3. Counted values in a hash.Map

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

## 4. Trees with weak parent links

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

## 5. An LRU cache: a doubly linked list with weak back links

The Go instinct is `prev, next *Node`. Here `next` is a `shared[Node]` (it keeps the next node alive), `prev` is a `weak` reference (it does not, so there is no cycle), and a map from key to node gives O(1) lookup. The cache struct is declared `owner` so it moves instead of being copied by accident.

```pluto-run
package main

import (
	"pluto/alloc"
	"pluto/fmt"
)

type Node owner struct {
	key  string
	val  int
	prev weak[shared[Node]]
	next shared[Node]
}

type LRU owner struct {
	cap  int
	m    map[string]shared[Node]
	head shared[Node] // most recent
	tail shared[Node] // least recent
}

func NewLRU(n int) LRU { return LRU{cap: n, m: map[string]shared[Node]{}} }

func (c *LRU) unlink(n shared[Node]) {
	p, hasPrev := n.prev.Upgrade()
	if hasPrev {
		p.next = n.next
	} else {
		c.head = n.next
	}
	if n.next != nil {
		n.next.prev = n.prev
	} else if hasPrev {
		c.tail = p
	} else {
		c.tail = nil
	}
	n.next = nil
	n.prev = weak[shared[Node]](nil)
}

func (c *LRU) pushFront(n shared[Node]) {
	n.next = c.head
	if c.head != nil {
		c.head.prev = weak[shared[Node]](n)
	} else {
		c.tail = n
	}
	c.head = n
}

func (c *LRU) Get(key string) (int, bool) {
	n := c.m[key]
	if n == nil {
		return 0, false
	}
	c.unlink(n)
	c.pushFront(n)
	return n.val, true
}

func (c *LRU) Put(key string, value int) {
	if n := c.m[key]; n != nil {
		n.val = value
		c.unlink(n)
		c.pushFront(n)
		return
	}
	if len(c.m) >= c.cap && c.tail != nil {
		old := c.tail
		c.unlink(old)
		delete(c.m, old.key)
	}
	n := alloc.Shared[Node]()
	n.key = key
	n.val = value
	c.m[key] = n
	c.pushFront(n)
}

func main() {
	c := NewLRU(2)
	c.Put("a", 1)
	c.Put("b", 2)
	v, ok := c.Get("a")
	fmt.Println(v, ok)
	c.Put("c", 3) // evicts b
	_, ok = c.Get("b")
	fmt.Println("b present:", ok)
	c.Put("a", 10)
	c.Put("d", 4) // evicts c
	_, ok = c.Get("c")
	fmt.Println("c present:", ok)
	v, ok = c.Get("a")
	fmt.Println(v, ok)
}
```

```output
1 true
b present: false
c present: false
10 true
```
