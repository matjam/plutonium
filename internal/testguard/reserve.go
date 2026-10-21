package testguard

import (
	"fmt"
	"sort"
	"sync"
	"testing"
)

// A pool hands out a memory budget to tests that declare how much they
// need. Tests that need far more than a parallel slot (a build of the
// translated compiler takes GB) reserve their estimate before they start,
// so the run does not start two of them when only one fits.
type pool struct {
	mu   sync.Mutex
	cond *sync.Cond
	size uint64            // the budget; 0 means no limit (no /proc)
	used uint64            // what the holders have reserved
	held map[string]uint64 // reservations by test name
}

func newPool(size uint64) *pool {
	p := &pool{size: size, held: map[string]uint64{}}
	p.cond = sync.NewCond(&p.mu)
	return p
}

// budget is the run's pool, sized by Run.
var budget = newPool(0)

// Reserve blocks until mb megabytes of the run's memory budget are free and
// takes them for the test t, and returns a function that gives them back.
// Call it before a step known to need much more memory than a parallel slot
// (PLUTO_TEST_SLOT_MB), with a measured estimate of that step's peak, and
// call the function when the step is done; the test's cleanup gives back a
// reservation still held. The budget is the watchdog's cap (see Run).
//
// A reservation larger than the whole budget waits until no other is held
// and then runs alone. A test must not reserve while it (or a parent test
// waiting for it) holds a reservation: the second could wait for ever. Without /proc, or with the watchdog off, Reserve
// returns at once.
func Reserve(t testing.TB, mb uint64) (release func()) {
	t.Helper()
	name := t.Name()
	if waited := budget.acquire(name, mb<<20); waited {
		t.Logf("testguard: waited for %d MB of the memory budget", mb)
	}
	var once sync.Once
	release = func() { once.Do(func() { budget.release(name, mb<<20) }) }
	t.Cleanup(release)
	return release
}

// acquire takes n bytes for name, waiting until they fit; it reports whether
// it had to wait.
func (p *pool) acquire(name string, n uint64) (waited bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.size == 0 {
		return false
	}
	for !p.fits(n) {
		waited = true
		p.cond.Wait()
	}
	p.used += n
	p.held[name] += n
	return waited
}

// fits reports whether n more bytes fit: within the budget, or, for a
// reservation bigger than the budget, when nothing else is held.
func (p *pool) fits(n uint64) bool {
	if n > p.size {
		return p.used == 0
	}
	return p.used+n <= p.size
}

func (p *pool) release(name string, n uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.size == 0 {
		return
	}
	p.used -= n
	if p.held[name] -= n; p.held[name] == 0 {
		delete(p.held, name)
	}
	p.cond.Broadcast()
}

// holders describes the reservations held, largest first.
func (p *pool) holders() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	names := make([]string, 0, len(p.held))
	for n := range p.held {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		if p.held[names[i]] != p.held[names[j]] {
			return p.held[names[i]] > p.held[names[j]]
		}
		return names[i] < names[j]
	})
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = fmt.Sprintf("%s (%s)", n, mbString(p.held[n]))
	}
	return out
}
