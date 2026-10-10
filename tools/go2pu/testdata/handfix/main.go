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

type job struct{ run func() }

var jobs []job

func later(f func()) { jobs = append(jobs, job{run: f}) }

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

func safe() (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("recovered: %v", r)
		}
	}()
	return nil
}

func main() {
	x := 1
	later(func() { x++ })
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
