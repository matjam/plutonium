// Counted strings (language spec section 6.10) across puroutines, channels,
// select, and range over functions: every string made here must be freed
// exactly once (TestStringsLeakFree runs this with the debug allocator).
// With the imports changed to Go's packages and alloc.FreeChan removed, Go
// prints the same.

package main

import (
	"pluto/alloc"
	"pluto/fmt"
	"pluto/strconv"
	"pluto/strings"
	"pluto/sync"
)

// heap returns a counted copy of s.
func heap(s string) string { return strings.Clone(s) }

// ----------------------------------------------------------------------------
// go statements

var (
	wg      sync.WaitGroup
	results = make(chan string, 8)
)

type tagged struct {
	tag  string
	n    int
	note string
}

func worker(name string, t tagged, rest ...string) {
	out := name + ":" + t.tag + "/" + strconv.Itoa(t.n) + "/" + t.note
	for _, r := range rest {
		out += "," + r
	}
	results <- out
	wg.Done()
}

func goStatements() {
	base := heap("base")
	wg.Add(3)
	go worker(base, tagged{tag: heap("t1"), n: 1, note: "static"})
	go worker(base+"+", tagged{tag: "t2", n: 2, note: heap("n2")}, heap("x"), "y")
	t := tagged{tag: heap("t3"), n: 3}
	go worker("lit", t, base)
	base = "changed" // the puroutines hold their own references
	t.tag = "gone"
	wg.Wait()
	var got [3]string
	for i := range got {
		got[i] = <-results
	}
	sortStrings(got[:])
	for _, s := range got {
		fmt.Println("go:", s)
	}
	_ = base
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// ----------------------------------------------------------------------------
// Shared strings on several threads: counts are atomic once the scheduler
// has started its workers.

// Each puroutine gets the string as an argument (a package variable that
// main writes while they run would be a data race) and copies it over and
// over, so the counts of one buffer change on several threads at once.
func copier(shared string, rounds int, done chan<- int) {
	n := 0
	for i := 0; i < rounds; i++ {
		s := shared
		t := s[1:]
		if len(t) == len(shared)-1 {
			n++
		}
	}
	done <- n
}

func sharedCounts() {
	shared := heap("shared between puroutines")
	done := make(chan int)
	for i := 0; i < 4; i++ {
		go copier(shared, 20000, done)
	}
	total := 0
	for i := 0; i < 4; i++ {
		total += <-done
	}
	fmt.Println("shared:", total, shared)
	alloc.FreeChan(done)
}

// ----------------------------------------------------------------------------
// Channels

type msg struct {
	id   int
	body string
}

func channels() {
	ch := make(chan string, 4)
	ch <- heap("one")
	ch <- "two" + strconv.Itoa(2)
	s := heap("three")
	ch <- s
	s = "reused"
	a := <-ch
	b, ok := <-ch
	fmt.Println("chan:", a, b, ok, <-ch, s)

	mc := make(chan msg, 2)
	mc <- msg{1, heap("first")}
	mc <- msg{2, "second"}
	close(mc)
	for m := range mc {
		fmt.Println("msg:", m.id, m.body)
	}
	m, ok := <-mc
	fmt.Println("closed:", m.id, m.body == "", ok)

	// Values left in a channel are released when it is freed.
	left := make(chan msg, 3)
	left <- msg{1, heap("left behind")}
	left <- msg{2, strconv.Itoa(12345)}
	alloc.FreeChan(left)
	alloc.FreeChan(mc)

	// Unbuffered, from another puroutine, received with range.
	un := make(chan string)
	go func(c chan<- string, n int) {
		for i := 0; i < n; i++ {
			c <- "item " + strconv.Itoa(i)
		}
		close(c)
	}(un, 3)
	count := 0
	for range un {
		count++
	}
	fmt.Println("range discard:", count)
	alloc.FreeChan(un)
	alloc.FreeChan(ch)
}

// ----------------------------------------------------------------------------
// select

func selects() {
	full := make(chan string, 1)
	full <- heap("occupant")
	data := make(chan string, 1)
	data <- heap("payload")
	var got string = heap("old value")
	// Two sends that cannot proceed (full is full), one receive that can.
	for i := 0; i < 2; i++ {
		select {
		case full <- heap("never sent"):
			fmt.Println("select: sent")
		case got = <-data:
			fmt.Println("select: got", got)
		default:
			fmt.Println("select: default", got)
		}
	}
	select {
	case v, ok := <-full:
		fmt.Println("select: received", v, ok)
	case full <- "late":
		fmt.Println("select: late sent")
	}
	select {
	case <-data:
		fmt.Println("select: dropped")
	case full <- strconv.Itoa(7):
		fmt.Println("select: refilled")
	}
	fmt.Println("select: left", len(full), len(data))
	alloc.FreeChan(full)
	alloc.FreeChan(data)
}

// ----------------------------------------------------------------------------
// Range over functions

func words(n int) func(yield func(int, string) bool) {
	return func(yield func(int, string) bool) {
		for i := 0; i < n; i++ {
			if !yield(i, "w"+strconv.Itoa(i)) {
				return
			}
		}
	}
}

func findWord(target string) string {
	for i, w := range words(10) {
		if w == target {
			return w + " at " + strconv.Itoa(i)
		}
	}
	return "missing"
}

func rangeFuncs() {
	last := heap("none")
	joined := ""
	for i, w := range words(5) {
		if i == 1 {
			continue
		}
		last = w
		joined += w
		if i == 3 {
			break
		}
	}
	fmt.Println("rangefunc:", last, joined)
	fmt.Println("rangefunc:", findWord("w4"), findWord("zz"))

	// Nested loops, a labelled break out of both and a goto out.
	pair := ""
outer:
	for _, a := range words(3) {
		for _, b := range words(3) {
			if a == "w1" && b == "w2" {
				pair = a + b
				break outer
			}
		}
	}
	fmt.Println("rangefunc nested:", pair)
	for _, w := range words(4) {
		if w == "w2" {
			last = w + "!"
			goto out
		}
	}
out:
	fmt.Println("rangefunc goto:", last)

	// A closure constructor keeps a temporary string.
	n := 0
	for f := range strings.SplitSeq(heap("a,b")+","+heap("c"), ",") {
		n++
		last = f
	}
	fmt.Println("splitseq:", n, last)

	// A defer in a loop body keeps its arguments until the function
	// returns.
	deferInBody()
}

func deferInBody() {
	for _, w := range words(3) {
		if w == "w1" {
			defer fmt.Println("deferred:", w+"?")
			return
		}
	}
}

func main() {
	goStatements()
	sharedCounts()
	channels()
	selects()
	rangeFuncs()
	alloc.FreeChan(results)
}
