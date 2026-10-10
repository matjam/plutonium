// go statements of every form, and channels of several element types.

package main

type pair struct {
	a, b int
	s    string
}

func producer(n int, out chan<- int) {
	for i := 0; i < n; i++ {
		out <- i
	}
	close(out)
}

func sum(in <-chan int, res chan<- int) {
	t := 0
	for v := range in {
		t += v
	}
	res <- t
}

func vari(done chan string, xs ...int) {
	t := 0
	for _, x := range xs {
		t += x
	}
	println("vari", len(xs), t)
	done <- "vari done"
}

type T struct{ n int }

func (t T) show(done chan bool) {
	println("value method", t.n)
	done <- true
}

// bump only reads through its receiver: a view another task may use must
// point to memory nothing changes while both can use it.
func (t *T) bump(done chan bool) {
	println("pointer method", t.n+1)
	done <- true
}

type shower interface{ show(done chan bool) }

func two() (int, string) { return 7, "seven" }

var fiveSix = []int{5, 6}

var bothOut = make(chan string, 1)

func both(n int, s string) {
	println("both", n, s)
	bothOut <- s
}

type big struct {
	a [8]int
	s string
}

func bigArg(b big, out chan int) {
	t := 0
	for _, v := range b.a {
		t += v
	}
	println("big", t, b.s)
	out <- t
}

func gen[E any](v E, out chan E) { out <- v }

// Package variables, which the puroutines may outlive main's frame for,
// set before any puroutine starts: another puroutine may use a view of
// memory nothing changes.
var tBump T
var t9Show = T{9}

func main() {
	c := make(chan int)
	r := make(chan int, 1)
	go producer(100, c)
	go sum(c, r)
	println("sum", <-r)

	b := make(chan pair, 3)
	b <- pair{1, 2, "x"}
	b <- pair{3, 4, "y"}
	println(len(b), cap(b))
	p := <-b
	println(p.a, p.b, p.s)
	close(b)
	q, ok := <-b
	println(q.a, q.s, ok)
	q, ok = <-b
	println(q.a, q.s, ok)
	for v := range b {
		println("unexpected", v.a)
	}

	d := make(chan string)
	go vari(d, 1, 2, 3, 4)
	println(<-d)
	go vari(d)
	println(<-d)
	// Not []int{5, 6}: that array would be in main's frame, which the
	// puroutine may outlive. Nor a slice main may still change: a view
	// crosses to another puroutine only if what it points to cannot change
	// while both use it, as a package variable nothing writes.
	go vari(d, fiveSix...)
	println(<-d)

	bd := make(chan bool)
	go T{7}.show(bd)
	<-bd
	// Package variables: the puroutines may outlive main's frame.
	t := &tBump
	go t.bump(bd)
	<-bd
	// Not T{9}: the interface would box it in main's frame.
	t9 := &t9Show
	var sh shower = t9
	go sh.show(bd)
	<-bd

	f := func(x int, out chan int) { out <- x * 2 }
	go f(21, r)
	println(<-r)

	go func(x int, r chan int) { r <- x + 1 }(41, r)
	println(<-r)

	go both(two())
	println(<-bothOut)

	var bg big
	for i := range bg.a {
		bg.a[i] = i * i
	}
	bg.s = "squares"
	go bigArg(bg, r)
	bg.a[0] = 1000 // the puroutine has its own copy
	println(<-r)

	gs := make(chan string, 1)
	go gen("generic", gs)
	println(<-gs)
	gp := make(chan pair, 1)
	go gen(pair{1, 1, "p"}, gp)
	println((<-gp).s)

	// Channels are comparable, and nil until made.
	var nilc chan int
	println(nilc == nil, c == c, c != r)
	var anyc any = c
	if cc, ok := anyc.(chan int); ok {
		println("chan in interface", cc == c)
	}
	var ro <-chan int = r
	r <- 3
	println("receive-only", <-ro, len(ro), cap(ro))
}
