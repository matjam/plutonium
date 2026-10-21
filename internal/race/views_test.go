package race_test

import (
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

// Views of an owner's memory made before a task starts (free.go): a task
// that grows, frees or replaces the owner frees the memory the view looks
// at, which is a write of that memory (memory-model.md section 3.8), so a
// use of the view while the task may run is a data race unless one lock
// orders the two (language spec sections 19.3 and 19.4).
var viewsRejected = []struct {
	name string
	src  string
	want []string
}{
	{"pointer into a vector that a task grows", `
import (
	"pluto/sync"
	"pluto/vec"
)

func main() {
	var mu sync.Mutex
	v := vec.New[int]()
	v.Push(1)
	p := v.Ptr(0)
	var s sync.Scope
	s.Task(func() {
		mu.Lock()
		for i := 0; i < 1000; i++ {
			v.Push(i)
		}
		mu.Unlock()
	})
	sum := 0
	for i := 0; i < 1000000; i++ {
		sum += *p
	}
	s.Wait()
	println(sum)
}`, []string{"main.p:22:10: v.data[i] is read here while the task started at main.p:13 may write it",
		"The task started at main.p:13 frees v.data[i] at pluto/vec/vec.p:", "in (*Vec).free called at main.p:16", "E3402"}},

	{"element read through the vector", `
import (
	"pluto/sync"
	"pluto/vec"
)

func main() {
	var mu sync.Mutex
	v := vec.New[int]()
	v.Push(1)
	var s sync.Scope
	s.Task(func() {
		mu.Lock()
		v.Push(2)
		mu.Unlock()
	})
	println(v.At(0))
	s.Wait()
}`, []string{"is read here while the task started at main.p:12 may write it", "(*Vec).At called at main.p:17", "E3402"}},

	{"slice of a vector", `
import (
	"pluto/sync"
	"pluto/vec"
)

func main() {
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
}`, []string{"main.p:18:10: v.data[0] is read here while the task started at main.p:13 may write it", "E3402"}},

	{"view from a helper, grown by a helper", `
import (
	"pluto/sync"
	"pluto/vec"
)

func first(v *vec.Vec[int]) *int { return v.Ptr(0) }

func grow(v *vec.Vec[int]) {
	for i := 0; i < 100; i++ {
		v.Push(i)
	}
}

func main() {
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
}`, []string{"main.p:26:10: v.data[i] is read here while the task started at main.p:21 may write it",
		"called at main.p:23", "E3402"}},

	{"view stored in a struct field", `
import (
	"pluto/sync"
	"pluto/vec"
)

type holder struct{ p *int }

func main() {
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
}`, []string{"main.p:20:10: v.data[i] is read here while the task started at main.p:15 may write it", "E3402"}},

	{"view captured by a closure written before the Scope", `
import (
	"pluto/sync"
	"pluto/vec"
)

func main() {
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
}`, []string{"main.p:12:29: v.data[i] is read here while the task started at main.p:14 may write it", "E3402"}},

	{"field of an owned object the task replaces", `
import "pluto/sync"

type T owner struct{ n int }

func main() {
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
}`, []string{"main.p:16:10: the memory allocated at main.p:8 is read here while the task started at main.p:11 may write it",
		"The task started at main.p:11 frees the memory allocated at main.p:8 at main.p:13", "E3402"}},

	{"vector in a field of a local owner", `
import (
	"pluto/sync"
	"pluto/vec"
)

type db owner struct {
	rows vec.Vec[int]
	n    int
}

func main() {
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
}`, []string{"main.p:23:10: d.rows.data[i] is read here while the task started at main.p:18 may write it", "E3402"}},

	{"element of a vector of vectors that a task replaces", `
import (
	"pluto/sync"
	"pluto/vec"
)

func main() {
	var mu sync.Mutex
	v := vec.New[vec.Vec[int]]()
	v.Push(vec.New[int]())
	v.Ptr(0).Push(1)
	p := v.Ptr(0).Ptr(0)
	var s sync.Scope
	s.Task(func() {
		mu.Lock()
		v.Set(0, vec.New[int]())
		mu.Unlock()
	})
	println(*p)
	s.Wait()
}`, []string{"main.p:19:10: v.data[i].data[i] is read here while the task started at main.p:14 may write it", "E3402"}},

	{"memory the task frees", `
import (
	"pluto/alloc"
	"pluto/sync"
)

func main() {
	var mu sync.Mutex
	x := alloc.New[int]()
	*x = 1
	p := x
	var s sync.Scope
	s.Task(func() {
		mu.Lock()
		alloc.Free(x)
		mu.Unlock()
	})
	println(*p)
	s.Wait()
}`, []string{"main.p:18:10: the memory allocated at main.p:9 is read here while the task started at main.p:13 may write it", "E3402"}},

	{"synced variable the task releases", `
import (
	"pluto/alloc"
	"pluto/sync"
)

type S struct{ hits int }

func main() {
	var mu sync.Mutex
	y := alloc.Synced[S]()
	q := &y.hits
	var s sync.Scope
	s.Task(func() {
		mu.Lock()
		y = nil
		mu.Unlock()
	})
	println(*q)
	s.Wait()
}`, []string{"main.p:19:10: the memory allocated at main.p:11 is read here while the task started at main.p:14 may write it", "frees the memory allocated at main.p:11 at main.p:16", "E3402"}},

	{"view of a synced object kept past its lock", `
import (
	"pluto/sync"
	"pluto/vec"
)

type DB owner struct {
	mu   sync.Mutex
	rows vec.Vec[int]
}

func reader(d synced[DB], done chan bool) {
	d.mu.Lock()
	p := d.rows.Ptr(0)
	d.mu.Unlock()
	println(*p)
	done <- true
}

func writer(d synced[DB], done chan bool) {
	d.mu.Lock()
	for i := 0; i < 100; i++ {
		d.rows.Push(i)
	}
	d.mu.Unlock()
	done <- true
}

func main() {
	var d synced[DB]
	d.rows.Push(1)
	done := make(chan bool)
	task reader(d, done)
	task writer(d, done)
	<-done
	<-done
}`, []string{"main.p:16:10: the field rows.data[i] of a synced[DB] is read here without holding mu, which guards it", "E3503"}},
}

// Views of an owner that no task frees while they are used.
var viewsAccepted = []struct {
	name string
	src  string
}{
	{"task only reads the vector", `
import (
	"pluto/sync"
	"pluto/vec"
)

func main() {
	v := vec.New[int]()
	v.Push(1)
	p := v.Ptr(0)
	var s sync.Scope
	s.Task(func() { println(v.Len(), v.At(0)) })
	println(*p)
	s.Wait()
	v.Push(2)
}`},

	{"view used after the join", `
import (
	"pluto/sync"
	"pluto/vec"
)

func main() {
	var mu sync.Mutex
	v := vec.New[int]()
	v.Push(1)
	p := v.Ptr(0)
	var s sync.Scope
	s.Task(func() {
		mu.Lock()
		v.Push(2)
		mu.Unlock()
	})
	s.Wait()
	p = v.Ptr(0)
	println(*p)
}`},

	{"view made and used under the lock", `
import (
	"pluto/sync"
	"pluto/vec"
)

func main() {
	var mu sync.Mutex
	v := vec.New[int]()
	v.Push(1)
	var s sync.Scope
	s.Task(func() {
		mu.Lock()
		v.Push(2)
		mu.Unlock()
	})
	mu.Lock()
	p := v.Ptr(0)
	println(*p)
	mu.Unlock()
	s.Wait()
}`},

	{"task grows another vector", `
import (
	"pluto/sync"
	"pluto/vec"
)

func main() {
	a := vec.New[int]()
	b := vec.New[int]()
	a.Push(1)
	p := a.Ptr(0)
	var s sync.Scope
	s.Task(func() {
		for i := 0; i < 100; i++ {
			b.Push(i)
		}
	})
	println(*p)
	s.Wait()
	println(b.Len())
}`},

	{"task replaces another owned object", `
import "pluto/sync"

type T owner struct{ n int }

func main() {
	var mu sync.Mutex
	o := new(T)
	k := new(T)
	q := &o.n
	var s sync.Scope
	s.Task(func() {
		mu.Lock()
		k = new(T)
		mu.Unlock()
	})
	println(*q)
	s.Wait()
	println(k.n)
}`},

	{"task fills its own vector", `
import (
	"pluto/sync"
	"pluto/vec"
)

func main() {
	var s sync.Scope
	for i := 0; i < 4; i++ {
		s.Task(func() {
			v := vec.New[int]()
			for j := 0; j < i; j++ {
				v.Push(j)
			}
			v = vec.New[int]()
			v.Push(i)
			println(v.Len())
		})
	}
}`},

	{"reference released while a task holds its own", `
import (
	"pluto/alloc"
	"pluto/sync"
)

type S struct {
	mu   sync.Mutex
	hits int
}

func (s *S) Add(done chan bool) {
	s.mu.Lock()
	s.hits++
	s.mu.Unlock()
	done <- true
}

type F struct{ main synced[S] }

func main() {
	done := make(chan bool)
	f := F{main: alloc.Synced[S]()}
	task f.main.Add(done)
	f.main = nil
	<-done
}`},

	{"view of a synced object used under its lock", `
import (
	"pluto/sync"
	"pluto/vec"
)

type DB owner struct {
	mu   sync.Mutex
	rows vec.Vec[int]
}

func reader(d synced[DB], done chan bool) {
	d.mu.Lock()
	p := d.rows.Ptr(0)
	println(*p)
	d.mu.Unlock()
	done <- true
}

func writer(d synced[DB], done chan bool) {
	d.mu.Lock()
	for i := 0; i < 100; i++ {
		d.rows.Push(i)
	}
	d.mu.Unlock()
	done <- true
}

func main() {
	var d synced[DB]
	d.rows.Push(1)
	done := make(chan bool)
	task reader(d, done)
	task writer(d, done)
	<-done
	<-done
}`},
}

func TestViewsRejected(t *testing.T) {
	testguard.Parallel(t)
	for _, c := range viewsRejected {
		t.Run(c.name, func(t *testing.T) {
			testguard.Parallel(t)
			err := check(t, "package main"+c.src)
			if err == nil {
				t.Fatalf("no error")
			}
			got := fullText(err)
			for _, w := range c.want {
				if !contains(got, w) {
					t.Errorf("missing %q in:\n%s", w, got)
				}
			}
		})
	}
}

func TestViewsAccepted(t *testing.T) {
	testguard.Parallel(t)
	for _, c := range viewsAccepted {
		t.Run(c.name, func(t *testing.T) {
			testguard.Parallel(t)
			if err := check(t, "package main"+c.src); err != nil {
				t.Errorf("unexpected error:\n%s", fullText(err))
			}
		})
	}
}
