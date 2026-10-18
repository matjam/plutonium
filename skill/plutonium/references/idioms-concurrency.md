# Idioms: concurrency

Complete short programs, compiled and run by the skill's test with their output checked. Rules behind them:
`concurrency.md`. Other idioms: `idioms.md`, `idioms-types-memory.md`, `idioms-memory.md`.

| # | Task | Features |
|---|---|---|
| 1 | worker pool | `sync.Scope`, jobs and results channels |
| 2 | pipeline | `task`, directional channels, `done` channel, `select` |
| 3 | producer and consumer of owned jobs | `owned[T]` over a channel |
| 4 | timeouts, tickers, cancellation | `time.After`, `NewTicker`, `select` |
| 5 | lock-guarded cache | `sync.RWMutex`, inferred guard |
| 6 | parallel work on disjoint rows | `[H][W]T` partition in a `Scope` |
| 7 | counters, once, first error | `atomic.Int64`, `sync.Once`, `sync.Mutex` |
| 8 | fan-out and fan-in with a closer task and a timeout | `sync.Scope`, local `WaitGroup`, `select` with `time.After` |

## 1. A worker pool with sync.Scope

Workers are scoped tasks that may use the caller's variables. `s.Wait()` joins them, so closing the results channel afterwards is safe.

```pluto-run
package main

import (
	"pluto/fmt"
	"pluto/slices"
	"pluto/sync"
	"pluto/vec"
)

func main() {
	jobs := make(chan int)
	results := make(chan int, 16)
	var s sync.Scope
	for w := 0; w < 3; w++ {
		s.Task(func() {
			for j := range jobs {
				results <- j * j
			}
		})
	}
	for i := 1; i <= 9; i++ {
		jobs <- i
	}
	close(jobs)
	s.Wait()
	close(results)
	got := vec.New[int]()
	for r := range results {
		got.Push(r)
	}
	slices.Sort(got.Slice())
	fmt.Println(got)
}
```

```output
[1 4 9 16 25 36 49 64 81]
```

## 2. A pipeline of tasks connected by channels

Each stage is a function started with `task`; it owns the output channel it closes. A `done` channel lets the consumer stop early without leaking blocked senders.

```pluto-run
package main

import "pluto/fmt"

func generate(done <-chan struct{}, out chan<- int) {
	defer close(out)
	for i := 1; ; i++ {
		select {
		case out <- i:
		case <-done:
			return
		}
	}
}

func square(done <-chan struct{}, in <-chan int, out chan<- int) {
	defer close(out)
	for v := range in {
		select {
		case out <- v * v:
		case <-done:
			return
		}
	}
}

func main() {
	done := make(chan struct{})
	defer close(done) // stops every stage when main returns
	nums := make(chan int)
	squares := make(chan int)
	task generate(done, nums)
	task square(done, nums, squares)
	sum := 0
	for i := 0; i < 5; i++ {
		sum += <-squares
	}
	fmt.Println(sum)
}
```

```output
55
```

## 3. Producer and consumer of owned jobs

Sending an owner moves it: the producer gives up the box, the consumer receives and drops it. A task that must finish before `main` ends reports on a `done` channel.

```pluto-run
package main

import (
	"pluto/fmt"
	"pluto/strings"
)

type Job struct {
	id   int
	text string
}

func produce(out chan<- owned[Job], n int) {
	for i := 1; i <= n; i++ {
		out <- new(Job{id: i, text: strings.Repeat("x", i)})
	}
	close(out)
}

func consume(in <-chan owned[Job], done chan<- int) {
	total := 0
	for j := range in {
		total += len(j.text)
	}
	done <- total
}

func main() {
	jobs := make(chan owned[Job], 2)
	done := make(chan int)
	task produce(jobs, 4)
	task consume(jobs, done)
	fmt.Println(<-done)
}
```

```output
10
```

## 4. Timeouts, tickers and cancellation

A timer is a channel, so `select` waits for it like any other case. `NewTicker` returns an owner that stops when dropped; `Stop` it explicitly when you are done early.

```pluto-run
package main

import (
	"pluto/fmt"
	"pluto/time"
)

func main() {
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	done := make(chan struct{})
	task func(done chan<- struct{}) {
		time.Sleep(32 * time.Millisecond)
		close(done)
	}(done)
	n := 0
loop:
	for {
		select {
		case <-tick.C:
			n++
		case <-done:
			break loop
		}
	}
	fmt.Println(n >= 1)

	res := make(chan string)
	select {
	case s := <-res:
		fmt.Println(s)
	case <-time.After(10 * time.Millisecond):
		fmt.Println("timed out")
	}
	start := time.Now()
	time.Sleep(15 * time.Millisecond)
	fmt.Println(time.Since(start) >= 15*time.Millisecond)
}
```

```output
true
timed out
true
```

## 5. A lock-guarded cache shared by tasks

The compiler infers that `mu` guards `m` because every write holds it; a read without the lock would be E3503. `RLock` is enough for readers.

```pluto-run
package main

import (
	"pluto/fmt"
	"pluto/sync"
)

type Cache struct {
	mu sync.RWMutex
	m  map[string]int
}

func (c *Cache) Get(k string) (int, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.m[k]
	return v, ok
}

func (c *Cache) Put(k string, v int) {
	c.mu.Lock()
	c.m[k] = v
	c.mu.Unlock()
}

func main() {
	c := Cache{m: map[string]int{}}
	var s sync.Scope
	for i := 0; i < 4; i++ {
		s.Task(func() { c.Put(fmt.Sprint("k", i), i*10) })
	}
	s.Wait()
	v, ok := c.Get("k3")
	fmt.Println(v, ok)
	_, ok = c.Get("zz")
	fmt.Println(ok)
}
```

```output
30 true
false
```

## 6. Parallel work on disjoint rows

Each task writes only its own row of a `[H][W]T` array, which the compiler proves disjoint (no lock). `grid[y][x]` on a slice of slices would be E3403.

```pluto-run
package main

import (
	"pluto/fmt"
	"pluto/sync"
)

const H, W = 4, 5

func main() {
	var grid [H][W]int
	var s sync.Scope
	for y := range grid {
		s.Task(func() {
			for x := 0; x < W; x++ {
				grid[y][x] = (y + 1) * (x + 1)
			}
		})
	}
	s.Wait()
	sum := 0
	for _, row := range grid {
		for _, v := range row {
			sum += v
		}
	}
	fmt.Println(grid[3], sum)
}
```

```output
[4 8 12 16 20] 150
```

## 7. Counters, once-only setup and first error

`atomic` values need no lock. `sync.Once` runs setup once. A scoped task can record the first error under a lock.

```pluto-run
package main

import (
	"pluto/errors"
	"pluto/fmt"
	"pluto/sync"
	"pluto/sync/atomic"
)

func check(i int) error {
	if i == 3 {
		return errors.New("item 3 failed")
	}
	return nil
}

func main() {
	var ok atomic.Int64
	var once sync.Once
	var mu sync.Mutex
	var first error
	var s sync.Scope
	for i := 0; i < 6; i++ {
		s.Task(func() {
			once.Do(func() { fmt.Println("setup once") })
			if err := check(i); err != nil {
				mu.Lock()
				if first == nil {
					first = err
				}
				mu.Unlock()
				return
			}
			ok.Add(1)
		})
	}
	s.Wait()
	fmt.Println(ok.Load(), first)
}
```

```output
setup once
5 item 3 failed
```

## 8. Fan-out and fan-in with a closer task and a timeout

Workers are scoped tasks. A `sync.WaitGroup` declared in the function may be used by Scope tasks (only passing `&wg` to a `task` statement is an error). One more task closes `results` when every worker is done; the collector selects on the results and a deadline. Leaving the function waits for the scope's tasks.

```pluto-run
package main

import (
	"pluto/fmt"
	"pluto/sort"
	"pluto/sync"
	"pluto/time"
	"pluto/vec"
)

func squares(workers, n int, timeout time.Duration) vec.Vec[int] {
	jobs := make(chan int, n)
	results := make(chan int, n)
	var wg sync.WaitGroup
	var s sync.Scope
	for w := 0; w < workers; w++ {
		wg.Add(1)
		s.Task(func() {
			defer wg.Done()
			for j := range jobs {
				results <- j * j
			}
		})
	}
	s.Task(func() { // the closer
		wg.Wait()
		close(results)
	})
	for i := 1; i <= n; i++ {
		jobs <- i
	}
	close(jobs)
	out := vec.New[int]()
	deadline := time.After(timeout)
	for {
		select {
		case r, ok := <-results:
			if !ok {
				sort.Ints(out.Slice())
				return out
			}
			out.Push(r)
		case <-deadline:
			return out
		}
	}
}

func main() {
	fmt.Println(squares(3, 8, time.Second))
}
```

```output
[1 4 9 16 25 36 49 64]
```
