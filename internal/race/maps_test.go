package race_test

import (
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

// Maps shared between tasks (language spec section 19.3, gap G16): the
// contents of maps of a type that crosses to another task are guarded by
// an inferred lock; maps that stay with one task need none.

var rejectedMaps = []struct {
	name string
	src  string
	want []string
}{
	{"map passed to two tasks, written without a lock", `
func count(words [3]string, m map[string]int, done chan bool) {
	for _, w := range words {
		m[w]++
	}
	done <- true
}

func main() {
	m := map[string]int{}
	done := make(chan bool)
	task count([3]string{"a", "b", "a"}, m, done)
	task count([3]string{"b", "c", "c"}, m, done)
	<-done
	<-done
	println(len(m))
}`, []string{"main.p:4:3: an element of a map[string]int is written here with no lock that guards it, while another task may use it",
		"m[k] = v", "E3502"}},

	{"read of a shared map without the inferred lock", `
import "pluto/sync"

var (
	mu    sync.Mutex
	cache = map[string]int{}
)

func fill(done chan bool) {
	mu.Lock()
	cache["x"] = 1
	mu.Unlock()
	done <- true
}

func main() {
	done := make(chan bool)
	task fill(done)
	task fill(done)
	<-done
	<-done
	println(cache["x"])
}`, []string{"an element of a map[string]int is read here without holding mu, which guards it", "E3503"}},

	{"delete and range race", `
func drain(m map[int]bool, done chan bool) {
	for k := range m {
		delete(m, k)
	}
	done <- true
}

func main() {
	m := map[int]bool{1: true, 2: true}
	done := make(chan bool)
	task drain(m, done)
	for i := 0; i < 10; i++ {
		m[i] = true
	}
	<-done
}`, []string{"an element of a map[int]bool is written here with no lock that guards it", "E3502"}},

	{"map in a struct sent on a channel", `
type job struct {
	results map[string]int
}

func work(jobs chan job, done chan bool) {
	j := <-jobs
	j.results["n"] = 1
	done <- true
}

func main() {
	jobs := make(chan job, 2)
	done := make(chan bool)
	r := map[string]int{}
	task work(jobs, done)
	task work(jobs, done)
	jobs <- job{results: r}
	jobs <- job{results: r}
	<-done
	<-done
}`, []string{"an element of a map[string]int is written here with no lock that guards it"}},

	{"a shared map holds a view", `
var data [4]int

func keep(m map[int]*int, done chan bool) {
	done <- true
}

func main() {
	done := make(chan bool)
	m := map[int]*int{}
	task keep(m, done)
	data[0] = 1
	<-done
}`, []string{"E3501"}},
}

var acceptedMaps = []struct {
	name string
	src  string
}{
	{"map guarded by a package lock", `
import "pluto/sync"

var (
	mu    sync.Mutex
	cache = map[string]int{}
)

func fill(k string, done chan bool) {
	mu.Lock()
	cache[k]++
	mu.Unlock()
	done <- true
}

func main() {
	done := make(chan bool)
	task fill("a", done)
	task fill("b", done)
	<-done
	<-done
	mu.Lock()
	println(len(cache), cache["a"])
	for k, v := range cache {
		println(k, v)
	}
	mu.Unlock()
}`},

	{"map guarded by a lock of the struct that holds it", `
import "pluto/sync"

type Counter struct {
	mu sync.Mutex
	m  map[string]int
}

func (c *Counter) Add(k string) {
	c.mu.Lock()
	c.m[k]++
	c.mu.Unlock()
}

func (c *Counter) Get(k string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.m[k]
}

var counter = Counter{m: map[string]int{}}

func work(done chan bool) {
	counter.Add("x")
	done <- true
}

func main() {
	done := make(chan bool)
	task work(done)
	task work(done)
	<-done
	<-done
	println(counter.Get("x"))
}`},

	{"maps each task makes for itself", `
func count(words [3]string, done chan int) {
	m := map[string]int{}
	for _, w := range words {
		m[w]++
	}
	add(m, "extra")
	done <- len(m)
}

func add(m map[string]int, k string) { m[k]++ }

func main() {
	done := make(chan int)
	for i := 0; i < 4; i++ {
		task count([3]string{"a", "b", "a"}, done)
	}
	local := map[string]int{}
	add(local, "x")
	for i := 0; i < 4; i++ {
		println(<-done)
	}
	println(len(local))
}`},

	{"a map filled before it is shared, then only read", `
func lookup(m map[string]int, done chan int) {
	done <- m["a"] + len(m)
}

func main() {
	m := map[string]int{"a": 1}
	m["b"] = 2
	done := make(chan int)
	task lookup(m, done)
	task lookup(m, done)
	println(<-done, <-done)
}`},
}

func TestRejectedMaps(t *testing.T) {
	testguard.Parallel(t)
	for _, c := range rejectedMaps {
		t.Run(c.name, func(t *testing.T) {
			testguard.Parallel(t)
			err := check(t, "package main"+c.src)
			if err == nil {
				t.Fatalf("no error")
			}
			got := fullText(err)
			for _, w := range c.want {
				if !strings.Contains(got, w) && !strings.Contains(squash(got), squash(w)) {
					t.Errorf("missing %q in:\n%s", w, got)
				}
			}
		})
	}
}

func TestAcceptedMaps(t *testing.T) {
	testguard.Parallel(t)
	for _, c := range acceptedMaps {
		t.Run(c.name, func(t *testing.T) {
			testguard.Parallel(t)
			if err := check(t, "package main"+c.src); err != nil {
				t.Errorf("unexpected error:\n%s", fullText(err))
			}
		})
	}
}
