// select: readiness, defaults, nil and closed channels, assignments in
// cases, loops and labels.

package main

func pinger(a, b chan int) {
	a <- 1
	b <- 2
	close(a)
}

func counter(n int, out chan int, quit chan bool) {
	for i := 0; ; i++ {
		select {
		case out <- i:
		case <-quit:
			println("counter quit after", i)
			quit <- true
			return
		}
	}
}

type msg struct {
	id   int
	text string
}

func main() {
	a := make(chan int)
	b := make(chan int)
	go pinger(a, b)
	gotA, gotB := false, false
	for !gotA || !gotB {
		select {
		case v := <-a:
			println("a", v)
			gotA = true
		case v, ok := <-b:
			println("b", v, ok)
			gotB = true
		}
	}
	// a is closed: a receive case proceeds at once with the zero value.
	select {
	case v, ok := <-a:
		println("closed a", v, ok)
	case <-b:
		println("unexpected b")
	}

	// Nil channels are never ready.
	var nilc chan int
	select {
	case v := <-nilc:
		println("unexpected", v)
	case nilc <- 1:
		println("unexpected send")
	default:
		println("nil channels block")
	}

	// A buffered channel: sends proceed until it is full.
	c := make(chan msg, 2)
	n := 0
	for {
		select {
		case c <- msg{n, "m"}:
			n++
			continue
		default:
		}
		break
	}
	println("sent", n, "len", len(c))

	var m msg
	var ok bool
	select {
	case m, ok = <-c:
	}
	println("received", m.id, m.text, ok)

	out := make(chan int)
	quit := make(chan bool)
	go counter(0, out, quit)
	total := 0
	for i := 0; i < 10; i++ {
		total += <-out
	}
	quit <- true
	<-quit
	println("total", total)

	// Break out of a labelled loop from inside a select.
	tick := make(chan int, 5)
	for i := 0; i < 5; i++ {
		tick <- i
	}
	close(tick)
	seen := 0
loop:
	for {
		select {
		case v, ok := <-tick:
			if !ok {
				break loop
			}
			seen += v
		}
	}
	println("seen", seen)

	// select {} with every channel nil would block forever; a select with
	// a single default runs it.
	select {
	default:
		println("only default")
	}
}
