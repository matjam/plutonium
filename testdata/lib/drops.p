package main

// Where owners are destroyed (language spec section 6.4): at the end of
// their scope on every path out of it, when a variable or place is given a
// new value, at the end of the statement for a temporary, and never twice
// for a value that moved.

type Res owner struct{ id int }

func (r *Res) Drop() {
	if r.id != 0 {
		println("drop", r.id)
	}
}

func mk(id int) Res { return Res{id: id} }

// consume takes r by value: it is r's owner, and destroys it on return.
func consume(r Res) { println("consume", r.id) }

func (r Res) eat() int { return r.id }

func (r *Res) peek() int { return r.id }

type Pair owner struct {
	a, b Res
}

func (p *Pair) Drop() { println("pair drop", p.a.id, p.b.id) }

func scopes() {
	a := mk(1)
	{
		b := mk(2)
		_ = b.peek()
	}
	println("after block")
	c := mk(3)
	consume(c)
	d := mk(4)
	d = mk(5)
	_ = d.peek()
	println("end scopes", a.peek())
}

func loops() {
	for i := 0; i < 3; i++ {
		r := mk(10 + i)
		if i == 1 {
			continue
		}
		_ = r.peek()
	}
	for i := 0; i < 3; i++ {
		r := mk(20 + i)
		if i == 1 {
			break
		}
		_ = r.peek()
	}
}

func early(x bool) Res {
	a := mk(30)
	if x {
		return mk(31)
	}
	return a
}

func (r Res) look() int { return r.id }

func temps() {
	println("temp", mk(40).look())
	mk(41)
	_ = mk(42)
	println("eaten", mk(43).eat())
}

func explicit() {
	r := mk(50)
	r.Drop()
	println("after explicit")
	p := Pair{a: mk(51), b: mk(52)}
	_ = p.a.peek()
}

func deferred() {
	r := mk(60)
	defer r.Drop()
	println("in deferred", r.peek())
}

func taking() {
	p := Pair{a: mk(70), b: mk(71)}
	a := take(&p.a)
	println("took", a.peek(), p.a.peek())
}

func gotos() {
	n := 0
again:
	r := mk(80 + n)
	n++
	if n < 2 {
		_ = r.peek()
		goto again
	}
	_ = r.peek()
}

// Named owns a counted string: destroying it releases the string after
// its Drop method, and moving it moves the reference.
type Named owner struct {
	name string
	tags [2]string
}

func (n *Named) Drop() {
	if n.name != "" {
		println("named drop", n.name, n.tags[1])
	}
}

// dyn makes a string with a counted buffer.
func dyn(c byte, i int) string {
	return string(rune(c)) + string(rune('0'+i))
}

func keepName(n Named) string { return n.name }

func counted() {
	a := Named{name: dyn('a', 1), tags: [2]string{dyn('t', 1), dyn('t', 2)}}
	b := a // moves: a is cleared, so only b is destroyed
	println("kept", keepName(b))
	c := Named{name: dyn('c', 1)}
	c = Named{name: dyn('c', 2)}
	for i := 0; i < 2; i++ {
		d := Named{name: dyn('d', i)}
		_ = d.name
	}
	println("end counted", c.name)
}

func main() {
	counted()
	println("--")
	scopes()
	println("--")
	loops()
	println("--")
	x := early(true)
	y := early(false)
	println("early", x.peek(), y.peek())
	consume(x)
	println("--")
	temps()
	println("--")
	explicit()
	println("--")
	deferred()
	println("--")
	taking()
	println("--")
	gotos()
	println("-- main ends")
}
