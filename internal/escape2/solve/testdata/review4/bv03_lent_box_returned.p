package main

// Owning interfaces take ownership of what is converted into them;
// ordinary interfaces only lend (language spec section 4.4).

type Resource owner interface {
	Name() string
}

type File owner struct{ name string }

func (f *File) Name() string { return f.name }

type Namer interface{ Name() string }

// Converting moves the box into the owning interface.
func moved() {
	f := new(File)
	var r Resource = f
	println(r.Name(), f.name)
}

// An ordinary interface lends the box: it may not leave the function.
func lent() Namer {
	f := new(File)
	var n Namer = f
	return n
}

// Asserting an ordinary interface to an owner type would make a second
// owner of the value it lends.
func asserted(n Namer) {
	f := n.(owned[File])
	_ = f
}

// An assertion out of an owning interface moves where it succeeds.
func taken(r Resource) {
	if f, ok := r.(owned[File]); ok {
		println(f.name)
	}
	println(r.Name())
}

func main() {
	moved()
	_ = lent()
}
