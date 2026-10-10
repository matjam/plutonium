package main

// owned, shared, synced and weak (language spec sections 5.6 and 6.5): new
// returns an owned[T], which moves and frees its object when dropped, on
// every path out of its scope; shared and synced count their references and
// drop the object with the last one; weak refers without keeping alive.
// Every Drop prints, so the output shows when each object goes. The
// program is also run under the debug allocator, which fails on a leak.

import (
	"pluto/alloc"
	"pluto/rt"
	"pluto/sync"
)

type Res owner struct {
	name string
	kids [2]owned[Res]
}

func (r *Res) Drop() { println("drop", r.name) }

func (r *Res) Name() string { return r.name }

func mk(name string) owned[Res] {
	r := new(Res)
	r.name = name
	return r // moves to the caller: nothing is dropped here
}

// tree builds a small tree: the children move into their parent.
func tree() owned[Res] {
	t := mk("root")
	t.kids[0] = mk("left")
	t.kids[1] = mk("right")
	return t
}

// early drops what its scopes hold on every way out.
func early(n int) int {
	a := mk("early-a")
	if n == 0 {
		return 0 // drops a
	}
	for i := 0; i < 3; i++ {
		b := mk("loop-b")
		if i == n {
			break // drops b
		}
		if i == 0 {
			continue // drops b
		}
		println("loop body", i, b.Name())
	} // each iteration drops b
	c := mk("early-c")
	println(a.Name(), c.Name())
	return n // drops c, then a
}

// consume takes ownership: its parameter is dropped when it returns.
func consume(r owned[Res]) {
	println("consuming", r.Name())
}

// Graph nodes with back references: children are shared, parents weak, so
// dropping the root frees the whole structure without a cycle of counts.
type Node owner struct {
	name     string
	children [2]shared[Node]
	parent   weak[shared[Node]]
}

func (n *Node) Drop() { println("drop node", n.name) }

func node(name string, parent shared[Node]) shared[Node] {
	n := alloc.Shared[Node]()
	n.name = name
	if parent != nil {
		n.parent = weak[shared[Node]](parent)
	}
	return n
}

func parentName(n shared[Node]) string {
	if p, ok := n.parent.Upgrade(); ok {
		return p.name
	}
	return "(gone)"
}

// Counter is shared between tasks through synced references.
type Counter owner struct {
	mu   sync.Mutex // guards n: the tasks reach it through their references
	n    int
	done chan bool
}

func (c *Counter) Drop() {
	println("drop counter") // then its channel is released
}

func bump(c synced[Counter], k int) {
	c.mu.Lock()
	c.n += k
	c.mu.Unlock()
	done := c.done
	c = nil // releases this task's reference before main goes on
	done <- true
}

// leaving waits for leave's task.
var leaving sync.WaitGroup

// leave ends its task with rt.Goexit: the deferred calls of every frame
// run and what each frame holds is dropped, innermost frame first, so
// stop's r is dropped before leave's deferred Done lets main go on.
func leave() {
	defer leaving.Done()
	stop()
}

func stop() {
	r := mk("goexit")
	println("leaving", r.Name())
	rt.Goexit()
}

func main() {
	// A tree of owned boxes, dropped at the end of its block.
	{
		t := tree()
		println("tree", t.Name(), t.kids[0].Name(), t.kids[1].Name())
		left := take(&t.kids[0])
		println("took", left.Name(), t.kids[0] == nil)
	}

	println("early", early(0))
	println("early", early(1))

	// Moving into a call, and explicit Drop.
	r := mk("moved")
	consume(r)
	d := mk("dropped")
	d.Drop()
	println("after drop")

	// Clone copies the object into a new box; both are freed.
	type point struct {
		x, y int
		tag  string
	}
	pt := new(point{1, 2, "pt"})
	pt2 := pt.Clone()
	pt2.x = 10
	println("clone", pt.x, pt2.x, pt2.tag, pt == pt2)

	// A view of an owned box's object, through a conversion.
	o := mk("viewed")
	v := (*Res)(o)
	println("view", v.Name(), &*o == v)

	// Shared references with weak parents.
	{
		root := node("root", nil)
		a := node("a", root)
		b := node("b", root)
		root.children[0] = a
		root.children[1] = b
		c := a // a second reference
		println("parents", parentName(a), parentName(b), c == a)
		w := weak[shared[Node]](a)
		root = nil // releases root: a and b are still held here
		println("parent of a now", parentName(a))
		_, alive := w.Upgrade()
		println("a alive", alive)
	}

	// synced crosses tasks.
	c := alloc.Synced[Counter]()
	c.done = make(chan bool)
	for i := 1; i <= 3; i++ {
		go bump(c, i)
		<-c.done
	}
	c.mu.Lock()
	println("counter", c.n)
	c.mu.Unlock()
	c = nil // the last reference: drops the Counter

	leaving.Add(1)
	go leave()
	leaving.Wait()
	println("end")
}
