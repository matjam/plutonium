// Command handfix holds one of each construct go2pu cannot translate, for
// TestHandFixNotes. It is never translated into the build.
package main

import (
	"encoding/binary"
	"fmt"
	"iter"
	"os"
	"sort"
	"strconv"
)

type big struct{ a, b, c int }

func (big) Error() string { return "big" }

// Run is exported, so closure conversion (closures.go) leaves it alone.
type job struct{ Run func() }

var jobs []job

func later(f func()) { jobs = append(jobs, job{Run: f}) }

// Closure conversion refuses a generic function and a named func type.
type gjob[T any] struct{ run func(T) }

func gater[T any](q *[]gjob[T], v T) {
	*q = append(*q, gjob[T]{run: func(x T) { _ = v; _ = x }})
}

type visitor func(int)

type vjob struct{ v visitor }

func addV(q *[]vjob, n int) {
	*q = append(*q, vjob{v: func(i int) { _ = n + i }})
}

func counter() func() int {
	n := 0
	return func() int { n++; return n }
}

func seq(n int) iter.Seq[int] {
	return func(yield func(int) bool) {
		for i := range n {
			if !yield(i) {
				return
			}
		}
	}
}

func fail() error { return big{1, 2, 3} }

// A type the package asserts keeps the note: the interface must hold a value.
type asserted struct{ a, b, c int }

func (asserted) Error() string { return "asserted" }

func fail2() error { return asserted{1, 2, 3} }

func isAsserted(err error) bool {
	_, ok := err.(asserted)
	return ok
}

func safe() (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("recovered: %v", r)
		}
	}()
	return nil
}

const debug = false

type wide struct{ a, b, c int }

type holder struct{ keep any }

var sink holder

// store keeps w in an interface: Go boxes it on the heap.
func store(w wide) { sink.keep = w }

// dead has code that never runs, which the translation leaves out.
func dead(n int) int {
	seen := 0
	if debug {
		seen = n
		println("never", seen)
	}
	return n
}

func main() {
	store(wide{1, 2, 3})
	_ = dead(1)
	x := 1
	later(func() { x++ })
	gater(new([]gjob[int]), 1)
	addV(new([]vjob), 2)
	c := counter()
	ch := make(chan int, 1)
	go func(v int) { ch <- v }(c())
	for i := range seq(2) {
		defer fmt.Println(i)
	}
	m := map[string]int{"a": 1}
	for k := range m {
		delete(m, k)
	}
	s := []int{3, 1, 2}
	sort.Slice(s, func(i, j int) bool { return i < j && s[i] < s[j] })
	var buf [8]byte
	binary.BigEndian.PutUint32(buf[:], 1)
	r := []rune("é")
	magic := "\x7fELF\xff" // binary data in a string
	host, _ := os.Hostname()
	quoted := strconv.AppendQuote(nil, host)
	fmt.Println(x, <-ch, fail(), safe(), len(r), host != "", complex(1, 2), len(magic), len(quoted))
}
