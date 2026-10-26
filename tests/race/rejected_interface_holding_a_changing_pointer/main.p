package main
type T struct{ n int }

func (t *T) String() string { t.n++; return "t" }

type stringer interface{ String() string }

func show(s stringer, done chan bool) {
	println(s.String())
	done <- true
}

var tv T

func main() {
	done := make(chan bool)
	var s stringer = &tv
	task show(s, done)
	<-done
}