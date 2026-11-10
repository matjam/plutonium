package main

type Buf owner struct{ b []byte; n int }

func (b *Buf) Len() int { return b.n }
func (b Buf) Consume() int { return b.n }
func (b *Buf) Clone() Buf { return Buf{n: b.n} }

type File owner struct{ fd int }

func (f *File) Drop() {}

type Conn owner struct {
	name string
	buf  Buf
}

var global Buf

func take2(b Buf) int { return b.n }
func look(b *Buf) int { return b.n }
func cond() bool { return true }

type Res owner interface{ Len() int }

type Slot owner union {
	Empty
	Open Buf
}

type Dev owner struct {
	r    Res
	s    Slot
	rs   [2]Res
}

// A moving assertion, or a type switch with a binding, on an owner held in
// a field, an element or *p would move out of a place that still holds it
// (language spec sections 11.13, 12.9 and 18.5, Places).
func (d *Dev) step(p *Res) {
	b := d.r.(owned[Buf])
	_ = b

	c, ok := d.rs[1].(owned[Buf])
	_, _ = c, ok

	e := (*p).(owned[Buf])
	_ = e

	o := d.s.(Slot.Open)
	_ = o

	switch v := d.r.(type) {
	case owned[Buf]:
		_ = v
	}

	switch v := d.s.(type) {
	case Slot.Open:
		_ = v
	default:
		_ = v
	}
}

func main() {
	d := Dev{}
	var r Res
	d.step(&r)
}
