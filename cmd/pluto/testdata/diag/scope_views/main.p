package main

// Views made before a scoped task starts (language spec sections 18.3,
// 19.3 and 19.4; memory-model.md sections 3.8 and 4.1). A view of an
// owner's memory is that memory: a task that grows, frees or replaces the
// owner frees what the view looks at, and a free is a write. Using the view
// while the task may run, without the lock the task holds, is a data race,
// wherever the view is kept.

import (
	"pluto/sync"
	"pluto/vec"
)

type holder struct{ p *int }

type T owner struct{ n int }

type db owner struct {
	rows vec.Vec[int]
}

func first(v *vec.Vec[int]) *int { return v.Ptr(0) }

func grow(v *vec.Vec[int]) {
	for i := 0; i < 100; i++ {
		v.Push(i)
	}
}

// A slice of the vector's elements.
func slice() {
	var mu sync.Mutex
	v := vec.New[int]()
	v.Push(1)
	xs := v.Slice()
	var s sync.Scope
	s.Task(func() {
		mu.Lock()
		v.Push(2)
		mu.Unlock()
	})
	println(xs[0])
	s.Wait()
}

// A view made by one helper; the vector grown by another.
func helpers() {
	var mu sync.Mutex
	v := vec.New[int]()
	v.Push(1)
	p := first(&v)
	var s sync.Scope
	s.Task(func() {
		mu.Lock()
		grow(&v)
		mu.Unlock()
	})
	println(*p)
	s.Wait()
}

// A view kept in a struct field.
func field() {
	var mu sync.Mutex
	v := vec.New[int]()
	v.Push(1)
	h := holder{p: v.Ptr(0)}
	var s sync.Scope
	s.Task(func() {
		mu.Lock()
		v.Push(2)
		mu.Unlock()
	})
	println(*h.p)
	s.Wait()
}

// A view captured by a function literal written before the Scope.
func closure() {
	var mu sync.Mutex
	v := vec.New[int]()
	v.Push(1)
	p := v.Ptr(0)
	get := func() int { return *p }
	var s sync.Scope
	s.Task(func() {
		mu.Lock()
		v.Push(2)
		mu.Unlock()
	})
	println(get())
	s.Wait()
}

// A field of an owned object that the task replaces, which frees it.
func replaced() {
	var mu sync.Mutex
	o := new(T)
	q := &o.n
	var s sync.Scope
	s.Task(func() {
		mu.Lock()
		o = new(T)
		mu.Unlock()
	})
	println(*q)
	s.Wait()
}

// A vector in a field of a local owner.
func nested() {
	var mu sync.Mutex
	var d db
	d.rows.Push(1)
	p := d.rows.Ptr(0)
	var s sync.Scope
	s.Task(func() {
		mu.Lock()
		d.rows.Push(2)
		mu.Unlock()
	})
	println(*p)
	s.Wait()
}

func main() {
	slice()
	helpers()
	field()
	closure()
	replaced()
	nested()
}
