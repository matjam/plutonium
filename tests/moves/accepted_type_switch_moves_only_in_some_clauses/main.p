package main

type Writer interface{ Write(p []byte) int }

type Res owner interface{ Read() int }

type File owner struct{ n int }

func (f *File) Read() int          { return f.n }
func (f *File) Write(p []byte) int { return len(p) }
func (f *File) Drop()              { println("drop", f.n) }

type Pipe owner struct{ n int }

func (p *Pipe) Read() int { return p.n }
func (p *Pipe) Drop()     { println("drop pipe", p.n) }

func mkFile(n int) owned[File] {
	f := new(File)
	f.n = n
	return f
}

func mkPipe(n int) owned[Pipe] {
	p := new(Pipe)
	p.n = n
	return p
}

func use(r Res) { println("use", r.Read()) }

// The lending clause keeps r; the moving clause gives r a new value.
func lendingClause(r Res) {
	switch v := r.(type) {
	case Writer:
		println("write", v.Write(nil))
		println("still", r.Read())
	case owned[Pipe]:
		println("pipe", v.n)
		r = mkPipe(9)
	}
	use(r)
}

// Every clause moves, a default included: r is used only after a new value.
func everyClause(r Res) {
	switch v := r.(type) {
	case owned[File]:
		println("file", v.n)
	default:
		println("other", v.Read())
	}
	r = mkFile(7)
	use(r)
}

// No clause matches: r keeps its value and is dropped at the end of the
// function.
func noMatch(r Res) {
	switch v := r.(type) {
	case owned[File]:
		println("file", v.n)
		return
	}
	use(r)
}

func main() {
	lendingClause(mkFile(1))
	lendingClause(mkPipe(2))
	everyClause(mkFile(3))
	everyClause(mkPipe(4))
	noMatch(mkPipe(5))
	noMatch(mkFile(6))
	println("end")
}
