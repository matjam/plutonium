package testguard

import (
	"fmt"
	"runtime/debug"
	"sort"
	"sync"
	"testing"
	"time"
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
	// inUse returns the memory the run uses now (the resident memory of
	// the test binary and its descendants), or nil to count only the
	// reservations.
	inUse func() uint64
	// waiting counts the tests waiting in acquire; freed is when wake
	// last returned freed memory to the system.
	waiting int
	freed   time.Time
	// exclusive is set while a reservation of more than a third of the
	// budget is held: nothing else starts beside it.
	exclusive bool
	// bigWaiting counts the large reservations waiting; while one waits,
	// no small one starts, so that the pool empties for it.
	bigWaiting int
	// patience is how long a large reservation waits for the memory in use
	// to fall (bigPatience).
	patience time.Duration
}

func newPool(size uint64, inUse func() uint64) *pool {
	p := &pool{size: size, held: map[string]uint64{}, inUse: inUse, patience: bigPatience}
	p.cond = sync.NewCond(&p.mu)
	return p
}

// budget is the run's pool, sized by Run.
var budget = newPool(0, nil)

// Reserve blocks until mb megabytes of the run's memory budget are free and
// takes them for the test t, and returns a function that gives them back.
// Call it before a step known to need much more memory than a parallel slot
// (PLUTO_TEST_SLOT_MB), with a measured estimate of that step's peak, and
// call the function when the step is done; the test's cleanup gives back a
// reservation still held. The budget is the watchdog's cap (see Run), and
// what is free is the budget less the larger of the reservations held and
// the memory the run is using (the test binary and the processes it
// started), so tests that reserve nothing are counted too.
//
// A reservation of more than a third of the budget waits until no other is
// held and then runs alone. A test must not reserve while it (or a parent
// test waiting for it) holds a reservation: the second could wait for ever.
// Without /proc, or with the watchdog off, Reserve returns at once.
func Reserve(t testing.TB, mb uint64) (release func()) {
	t.Helper()
	name := t.Name()
	start := time.Now()
	if waited := budget.acquire(name, mb<<20); waited {
		t.Logf("testguard: waited %v for %d MB of the memory budget", time.Since(start).Round(time.Millisecond), mb)
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
	if p.size == 0 {
		p.mu.Unlock()
		return false
	}
	since := time.Now()
	big := n > p.size/3
	for !p.fits(n, since) {
		waited = true
		p.waiting++
		if big {
			p.bigWaiting++
		}
		p.cond.Wait()
		p.waiting--
		if big {
			p.bigWaiting--
		}
	}
	p.used += n
	p.held[name] += n
	if big {
		p.exclusive = true
	}
	p.mu.Unlock()
	if big {
		// Running alone: give the heap the collector has freed back to
		// the system first.
		debug.FreeOSMemory()
	}
	return waited
}

// fits reports whether n more bytes fit for a reservation that has waited
// since then. A large reservation (more than a third of the budget) starts
// only when nothing else is held and the memory in use leaves room for it
// (one larger than the budget, when 10% is in use or less), and then holds
// the pool alone; if the memory in use stays too high (tests
// that reserve nothing, memory the runtime keeps), it starts anyway after
// bigPatience, so that it never waits for ever. Others fit within the budget
// less what is reserved or in use (the larger), or when nothing is held.
func (p *pool) fits(n uint64, since time.Time) bool {
	switch {
	case p.exclusive:
		return false
	case n <= p.size/3 && p.bigWaiting > 0:
		// A large reservation is waiting for the pool to empty: let it.
		return false
	case n > p.size/3:
		return p.used == 0 && (p.taken()+min(n, p.size-p.size/10) <= p.size || time.Since(since) >= p.patience)
	case p.used == 0:
		return true
	}
	return p.taken()+n <= p.size
}

// taken is the larger of what is reserved and what is in use.
func (p *pool) taken() uint64 {
	if p.inUse == nil {
		return p.used
	}
	return max(p.used, p.inUse())
}

// bigPatience is how long a large reservation waits, with nothing else
// held, for the memory in use to fall before it starts anyway.
const bigPatience = time.Minute

func (p *pool) release(name string, n uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.size == 0 {
		return
	}
	p.used -= n
	if n > p.size/3 {
		p.exclusive = false
	}
	if p.held[name] -= n; p.held[name] == 0 {
		delete(p.held, name)
	}
	p.cond.Broadcast()
}

// wake makes the waiters look again, for memory freed by tests that hold
// no reservation; Run calls it every second. While a test waits, it also
// returns the heap the collector has freed to the system every
// freeInterval, since the runtime keeps it for minutes and it counts as in
// use.
func (p *pool) wake() {
	p.mu.Lock()
	free := p.waiting > 0 && time.Since(p.freed) >= freeInterval
	if free {
		p.freed = time.Now()
	}
	p.mu.Unlock()
	if free {
		debug.FreeOSMemory()
	}
	p.mu.Lock()
	p.cond.Broadcast()
	p.mu.Unlock()
}

// freeInterval is how often wake returns freed memory to the system.
const freeInterval = 10 * time.Second

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
