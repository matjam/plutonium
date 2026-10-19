// Command closures holds the forms in which go2pu converts closures that
// outlive their frame into structs (closures.go): a queue of jobs whose
// build function is a field, deferred actions queued by a method, a func
// variable assigned in several branches and stored, a named function and a
// method value flowing into the same field, a returned closure that is
// stored, closures that create closures, and a converted value handed to a
// function of another package. TestDifferential checks that Go and
// Plutonium print the same text.
package main

import (
	"fmt"
	"strings"
)

type state struct {
	out  []string
	next int
}

func (s *state) emit(format string, args ...any) {
	s.out = append(s.out, fmt.Sprintf(format, args...))
}

// A job is queued with a build function that runs later, after the
// function that queued it has returned.
type job struct {
	name  string
	build func(s *state)
}

type compiler struct {
	queue []*job
	st    state
}

func (c *compiler) enqueue(name string, k int) {
	label := name + "!"
	c.queue = append(c.queue, &job{name: name, build: func(s *state) {
		s.emit("%s: k=%d label=%s", name, k, label)
	}})
}

func hello(s *state) { s.emit("hello from a named function") }

func (s *state) bump(n int) func(*state) {
	return func(t *state) { t.next += n; t.emit("bump %d -> %d", n, t.next) }
}

func (c *compiler) enqueueOthers() {
	c.queue = append(c.queue, &job{name: "hello", build: hello})
	c.queue = append(c.queue, &job{name: "nil"})
	c.queue = append(c.queue, &job{name: "bump", build: c.st.bump(5)})
}

func (c *compiler) drain() {
	for len(c.queue) > 0 {
		j := c.queue[0]
		c.queue = c.queue[1:]
		if j.build == nil {
			c.st.emit("%s: no build", j.name)
			continue
		}
		j.build(&c.st)
	}
}

// Deferred actions: later keeps its parameter.
type action struct {
	f    func()
	desc string
}

type checker struct {
	delayed []action
	log     []string
}

func (ch *checker) later(f func()) *action {
	i := len(ch.delayed)
	ch.delayed = append(ch.delayed, action{f: f})
	return &ch.delayed[i]
}

func (a *action) describef(format string, args ...any) {
	a.desc = fmt.Sprintf(format, args...)
}

func (ch *checker) decl(name string) {
	count := 0
	var result string
	ch.later(func() {
		count++
		ch.log = append(ch.log, fmt.Sprintf("%s: count=%d result=%s", name, count, result))
	}).describef("decl %s", name)
	result = strings.ToUpper(name)
	ch.later(func() {
		ch.log = append(ch.log, "again "+name+" "+result)
		count += 10
	})
}

func (ch *checker) process() {
	for i := 0; i < len(ch.delayed); i++ {
		a := ch.delayed[i]
		a.f()
	}
	ch.delayed = nil
}

// A func variable assigned in several branches, then stored.
type deferInfo struct {
	run func(s *state, n int) int
	tag string
}

func makeInfos(vals []int) []*deferInfo {
	var infos []*deferInfo
	for i, v := range vals {
		var run func(s *state, n int) int
		switch v % 3 {
		case 0:
			run = func(s *state, n int) int { s.emit("zero case %d %d", i, v); return n + v }
		case 1:
			w := v * 2
			run = func(s *state, n int) int { s.emit("one case %d %d", i, w); return n * w }
		default:
			run = func(s *state, n int) int { return n - i }
		}
		infos = append(infos, &deferInfo{run: run, tag: fmt.Sprint("t", i)})
	}
	return infos
}

// A closure that creates a closure.
type maker struct {
	mk func(base int) func() int
}

func newMaker(scale int) *maker {
	return &maker{mk: func(base int) func() int {
		total := base
		return func() int { total += scale; return total }
	}}
}

// A converted value is read and given to another package.
type mapper struct {
	f func(rune) rune
}

func newMapper(shift rune) *mapper {
	return &mapper{f: func(r rune) rune { return r + shift }}
}

// Package-level variable holding a closure.
var hook func(string) string

func setHook(prefix string) {
	hook = func(s string) string { return prefix + s }
}

func main() {
	c := &compiler{}
	c.enqueue("a", 1)
	c.enqueue("b", 2)
	c.enqueueOthers()
	c.drain()
	for _, l := range c.st.out {
		fmt.Println(l)
	}

	ch := &checker{}
	ch.decl("x")
	ch.decl("y")
	ch.process()
	for _, l := range ch.log {
		fmt.Println(l)
	}
	for _, a := range [...]string{"x", "y"} {
		fmt.Println(a)
	}

	var st state
	n := 7
	for _, info := range makeInfos([]int{3, 4, 5, 6}) {
		n = info.run(&st, n)
		fmt.Println(info.tag, n)
	}
	for _, l := range st.out {
		fmt.Println(l)
	}

	m := newMaker(3)
	f1, f2 := m.mk(10), m.mk(100)
	fmt.Println(f1(), f2(), f1(), f2())

	mp := newMapper(1)
	fmt.Println(strings.Map(mp.f, "HAL"))

	setHook(">> ")
	fmt.Println(hook("done"))
}
