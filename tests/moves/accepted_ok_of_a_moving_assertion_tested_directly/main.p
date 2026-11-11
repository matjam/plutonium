package main

// A direct test of the ok of a comma-ok assertion that moves an owner out
// of an interface variable tells where the variable still holds its value
// (language spec sections 11.13 and 18.5, D47): where ok is false, x was
// not moved.

type Buf owner struct{ n int }

func (b *Buf) Drop() { println("drop", b.n) }

type Thing owner interface{ Len() int }

func (b *Buf) Len() int { return b.n }

type Other owner struct{}

func (o *Other) Len() int { return -1 }

func use(x Thing) { println("use", x.Len()) }

// if !ok returns: after it, x was moved into v.
func notOk(x Thing) {
	v, ok := x.(owned[Buf])
	if !ok {
		use(x)
		return
	}
	println("got", v.Len())
}

// if ok, else: the else branch still has x.
func okElse(x Thing) {
	v, ok := x.(owned[Other])
	if ok {
		println("other", v.Len())
	} else {
		use(x)
	}
}

// A chain of && with ok in it: where it is true, x was moved.
func okAnd(x Thing, want bool) {
	v, ok := x.(owned[Buf])
	if !ok || !want {
		println("no")
		return
	}
	println("got", v.Len())
}

func main() {
	notOk(new(Buf{n: 1}))
	notOk(new(Other{}))
	okElse(new(Buf{n: 2}))
	okAnd(new(Buf{n: 3}), true)
	println("end")
}
