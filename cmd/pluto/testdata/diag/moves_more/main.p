package main

// More ways to misuse owners (language spec sections 6.4, 6.9).

type File owner struct{ fd int }

func (f *File) Drop() {}

type Buf owner struct{ n int }

func (b *Buf) Clone() Buf { return Buf{n: b.n} }

type Bufs owner struct{ data [4]Buf }

func (v *Bufs) Index(i int) Buf { return take(&v.data[i]) }

func closeIt(f *File) {
	f.Drop()
}

func all(bs ...Buf) {}

func verbose() bool { return true }

func main() {
	var bufs [2]Buf
	for _, b := range bufs {
		_ = b.n
	}

	var dst, src [2]Buf
	copy(dst[:], src[:])

	var x any
	b := x.(Buf)
	_ = b

	var v Bufs
	first := v[0]
	_ = first

	one := Buf{}
	all(one)

	two := Buf{}
	list := []Buf{two}
	_ = list

	if verbose() {
		f := File{}
		defer closeIt(&f)
	}
}
