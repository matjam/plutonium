package main

type Writer interface{ Write(p []byte) int }

type Res owner interface{ Read() int }

type File owner struct{ n int }

func (f *File) Read() int          { return f.n }
func (f *File) Write(p []byte) int { return len(p) + f.n }
func (f *File) Drop()              { println("drop", f.n) }

func mk(n int) owned[File] {
	f := new(File)
	f.n = n
	return f
}

func use(r Res) { println("use", r.Read()) }

// An assertion to an ordinary interface lends: r keeps its value
// (language spec sections 11.13 and 18.5, D44).
func lends(r Res) {
	w := r.(Writer)
	println("write", w.Write(nil))
	use(r)
}

// A moving conversion moves at the enclosing call; f is used before it.
func convAtCall() {
	f := mk(2)
	println("before", f.n)
	use(Res(f))
}

// A moving conversion moves at the enclosing assignment; f is used before
// it, and r after.
func convAtAssign() {
	f := mk(3)
	println("before", f.n)
	var r Res = f
	use(r)
}

func main() {
	lends(mk(1))
	convAtCall()
	convAtAssign()
	println("end")
}
