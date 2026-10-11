package moves_test

import (
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

// The owners of scoped tasks (scope.go): a task takes an owner whose
// block ends before its Scope waits, and a Scope that starts tasks does
// not move.
func TestScopeOwners(t *testing.T) {
	testguard.Parallel(t)
	const head = `package main

import "pluto/sync"

type Buf owner struct{ n int }

func (b *Buf) Drop() {}

func take(b Buf) {}
`
	rejected := []struct{ name, src, want string }{
		{"taken owner used after Task", `
func main() {
	var s sync.Scope
	for i := 0; i < 2; i++ {
		b := Buf{n: i}
		s.Task(func() { println(b.n) })
		println(b.n)
	}
}`, "b was taken by the task started by s.Task at line 15, so it can no longer be used here"},
		{"scope passed", `
func wait(s sync.Scope) {}

func main() {
	var s sync.Scope
	s.Task(func() {})
	wait(s)
}`, "cannot move s: it starts tasks at main.p:15, which find it at its address"},
		{"scope assigned", `
func main() {
	var s sync.Scope
	s.Task(func() {})
	t := s
	t.Wait()
}`, "cannot move s: it starts tasks"},
		{"shared owner moved inside the task", `
func main() {
	b := Buf{}
	var s sync.Scope
	s.Task(func() { take(b) })
}`, "cannot move b inside a function literal"},
	}
	for _, c := range rejected {
		t.Run(c.name, func(t *testing.T) {
			testguard.Parallel(t)
			err := check(t, head+c.src+"\n")
			if err == nil {
				t.Fatal("no error reported")
			}
			if msg := fullText(err); !strings.Contains(msg, c.want) {
				t.Errorf("error does not contain %q:\n%s", c.want, msg)
			}
		})
	}
	accepted := []struct{ name, src string }{
		{"taken owner moved on by the task", `
func main() {
	var s sync.Scope
	for i := 0; i < 2; i++ {
		b := Buf{n: i}
		s.Task(func() { take(b) })
	}
}`},
		{"shared owner used after the join", `
func main() {
	b := Buf{}
	var s sync.Scope
	s.Task(func() { println(b.n) })
	s.Wait()
	take(b)
}`},
		{"explicit drop of the scope", `
func main() {
	var s sync.Scope
	s.Task(func() {})
	s.Drop()
}`},
	}
	for _, c := range accepted {
		t.Run(c.name, func(t *testing.T) {
			testguard.Parallel(t)
			if err := check(t, head+c.src+"\n"); err != nil {
				t.Errorf("unexpected error:\n%s", fullText(err))
			}
		})
	}
}
