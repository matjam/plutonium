// Maps across tasks (language spec sections 7.10 and 19.3): a map
// guarded by a mutex that several tasks write, maps that each
// task makes for itself and hands to main under the same mutex, and
// maps sent on a channel. Counts are atomic once tasks run; every map
// and string made here must be freed exactly once (TestMapTasks runs this
// with the debug allocator, with one, two and many processors). With the
// imports changed to Go's packages, Go prints the same.

package main

import (
	"pluto/fmt"
	"pluto/strconv"
	"pluto/sync"
)

// The compiler infers one guard for the contents of all maps of a type that
// crosses between tasks (whatever the type's name), so the shared map
// has a type of its own, map[string]int64, apart from the map[string]int
// that each worker fills and hands over.
type counter map[string]int64

type tally struct {
	mu      sync.Mutex
	counts  counter
	results [8]map[string]int
}

func (t *tally) add(k string, n int) {
	t.mu.Lock()
	t.counts[k] += int64(n)
	t.mu.Unlock()
}

// hand stores a worker's own map for main to take.
func (t *tally) hand(id int, m map[string]int) {
	t.mu.Lock()
	t.results[id] = m
	t.mu.Unlock()
}

var (
	shared = tally{counts: counter{}}
	wg     sync.WaitGroup
)

func worker(id int) {
	own := map[string]int{}
	for i := 0; i < 1000; i++ {
		k := "k" + strconv.Itoa(i%17)
		shared.add(k, 1)
		own[k] += id + 1
	}
	shared.hand(id, own)
	// Drop this task's reference before main may exit: main waits
	// only for Done, and the leak check runs when it exits.
	own = nil
	wg.Done()
}

func main() {
	const n = 8
	wg.Add(n)
	for i := 0; i < n; i++ {
		task worker(i)
	}
	wg.Wait()

	// Main takes the workers' maps and passes them on through a channel
	// of its own.
	pipe := make(chan map[string]int, n)
	shared.mu.Lock()
	for i := range shared.results {
		pipe <- shared.results[i]
		shared.results[i] = nil
	}
	total := int64(0)
	for _, v := range shared.counts {
		total += v
	}
	fmt.Println(len(shared.counts), total, shared.counts["k0"], shared.counts["k16"])
	// A package variable lives as long as the program: dropping its
	// reference frees the map, for the leak check.
	shared.counts = nil
	shared.mu.Unlock()

	merged := map[string]int{}
	for i := 0; i < n; i++ {
		m := <-pipe
		for k, v := range m {
			merged[k] += v
		}
	}
	sum := 0
	for _, v := range merged {
		sum += v
	}
	fmt.Println(len(merged), sum, merged["k3"])
}
