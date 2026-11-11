package main

type Writer interface{ Write(p []byte) int }

type Res owner interface{ Read() int }

type File owner struct{ n int }

func (f *File) Read() int          { return f.n }
func (f *File) Write(p []byte) int { return len(p) }

func mk() owned[File] { return new(File) }

func use(r Res) {}

// An assertion to an ordinary interface lends: its result is a view of r,
// which the move of r ends (language spec sections 11.13 and 18.5, D44).
func lentEnded(r Res) {
	w := r.(Writer)
	use(r)
	w.Write(nil)
}

// A moving conversion moves at the enclosing call.
func convAtCall() {
	f := mk()
	use(Res(f))
	println(f.n)
}

// A moving conversion moves at the enclosing assignment.
func convAtAssign() {
	f := mk()
	var r Res = f
	println(f.n)
	use(r)
}

func main() {
	lentEnded(mk())
	convAtCall()
	convAtAssign()
}
