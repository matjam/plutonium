package main

// x.Drop() is valid on every owner variable, whether or not its type
// declares Drop (spec sections 7.18, 18.6), but only on the variable itself.

type File owner struct{ fd int }

func (f *File) Drop() {}

// Log is defined from File and has none of its methods.
type Log owner File

// Plain declares no Drop.
type Plain owner struct{ n int }

type Holder owner struct {
	log Log
	n   int
}

func main() {
	var p Plain
	q := &p
	q.Drop()
	m := p.Drop
	_ = m

	var h Holder
	h.log.Drop()

	l := Log(File{fd: 1})
	pl := &l
	f := File(*pl)
	_ = f
}
