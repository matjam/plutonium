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
	r Res
	s Slot
}

// take(&x.f) moves the value out first, and a switch without a binding
// reads the place and moves nothing (language spec sections 11.13, 12.9).
func (d *Dev) step() {
	b := take(&d.r).(owned[Buf])
	_ = b

	switch s := take(&d.s).(type) {
	case Slot.Open:
		d.s = Slot.Open(s)
	default:
		d.s = s
	}

	switch d.r.(type) {
	case owned[Buf]:
	}

	switch d.s.(type) {
	case Slot.Open:
	case Slot.Empty:
	}
}

func main() {
	d := Dev{}
	d.step()
}
