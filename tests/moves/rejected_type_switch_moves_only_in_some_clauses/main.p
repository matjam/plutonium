package main

type Writer interface{ Write(p []byte) int }

type Res owner interface{ Read() int }

type File owner struct{ n int }

func (f *File) Read() int            { return f.n }
func (f *File) Write(p []byte) int   { return len(p) }
func (f *File) Drop()                { println("drop", f.n) }

func mkFile(n int) owned[File] {
	f := new(File)
	f.n = n
	return f
}

func use(r Res) {}

// A clause naming an ordinary interface lends: r keeps its value there,
// and only the owned[File] clause moves it.
func lendingClause(r Res) {
	switch v := r.(type) {
	case Writer:
		v.Write(nil)
	case owned[File]:
		println(v.n)
	}
	use(r)
}

// With no default clause, an unmatched value stays in r.
func noDefault(r Res) {
	switch v := r.(type) {
	case owned[File]:
		println(v.n)
	}
	use(r)
}

// The binding of a lending clause is a view of r, which a new value for r
// ends.
func viewEnded(r Res) {
	switch v := r.(type) {
	case Writer:
		r = mkFile(2)
		v.Write(nil)
	default:
		_ = v
	}
	use(r)
}

func main() {
	lendingClause(mkFile(1))
	noDefault(mkFile(1))
	viewEnded(mkFile(1))
}
