package escape2

import (
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

// sampleStacks samples the stacks of the running goroutines every 10 ms
// until the returned function is called, then writes the functions found
// most often on top and anywhere on the stacks to file: cmd/pluto's
// PLUTO_STACKSAMPLE, for machines without the CPU profiler's timer.
func sampleStacks(t *testing.T, file string) func() {
	flat, cum := map[string]int{}, map[string]int{}
	samples := 0
	done, finished := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(finished)
		buf := make([]byte, 4<<20)
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-tick.C:
			}
			n := runtime.Stack(buf, true)
			for _, g := range strings.Split(string(buf[:n]), "\n\n") {
				if idle(g) {
					continue
				}
				seen := map[string]bool{}
				top := true
				for _, ln := range strings.Split(g, "\n")[1:] {
					if strings.HasPrefix(ln, "\t") || ln == "" {
						continue
					}
					if i := strings.LastIndexByte(ln, '('); i > 0 {
						ln = ln[:i]
					}
					if top {
						flat[ln]++
						top = false
					}
					if !seen[ln] {
						seen[ln] = true
						cum[ln]++
					}
				}
				samples++
			}
		}
	}()
	return func() {
		close(done)
		<-finished
		var f strings.Builder
		fmt.Fprintf(&f, "%d samples of running goroutines\n", samples)
		for _, m := range []struct {
			name string
			m    map[string]int
		}{{"flat (top frame)", flat}, {"cumulative (anywhere on the stack)", cum}} {
			type kv struct {
				k string
				v int
			}
			var all []kv
			for k, v := range m.m {
				all = append(all, kv{k, v})
			}
			sort.Slice(all, func(i, j int) bool { return all[i].v > all[j].v || all[i].v == all[j].v && all[i].k < all[j].k })
			fmt.Fprintf(&f, "\n%s\n", m.name)
			for i, e := range all {
				if i == 60 {
					break
				}
				fmt.Fprintf(&f, "%6d %5.1f%%  %s\n", e.v, 100*float64(e.v)/float64(max(samples, 1)), e.k)
			}
		}
		if err := os.WriteFile(file, []byte(f.String()), 0o644); err != nil {
			t.Errorf("stack sample: %v", err)
		}
	}
}

// idle reports whether goroutine dump g is of a goroutine that is waiting,
// or of the sampler itself.
func idle(g string) bool {
	for _, s := range []string{"sampleStacks", "[chan receive", "[chan send", "[select", "[sleep", "[syscall", "[IO wait", "[semacquire", "[sync.WaitGroup", "[sync.Mutex"} {
		if strings.Contains(g, s) {
			return true
		}
	}
	return false
}
