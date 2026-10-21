package testguard

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const mb = 1 << 20

// TestCgroupUsage checks that the file cache counts as free except files
// in a tmpfs, which stay without swap.
func TestCgroupUsage(t *testing.T) {
	root := fakeCgroups(t, map[string]string{
		"a/memory.current": "1048576000\n",
		"a/memory.stat":    "anon 400\nfile 629145600\nshmem 104857600\nfile_mapped 1\n",
		"b/memory.current": "max\n",
	})
	if used, ok := cgroupUsage(filepath.Join(root, "a")); !ok || used != 500*mb {
		t.Errorf("cgroupUsage = %d MB, %v; want 500 MB (1000 less 600 of file cache, 100 of it tmpfs)", used/mb, ok)
	}
	if _, ok := cgroupUsage(filepath.Join(root, "b")); ok {
		t.Error("cgroupUsage read a cgroup with no usage figure")
	}
}

// TestDedicatedCgroup checks that a cgroup holding only the run (itself, the
// processes it started, those that started it) is the run's, and one holding
// anything else is not.
func TestDedicatedCgroup(t *testing.T) {
	procs := []proc{
		{pid: 1, ppid: 0}, {pid: 50, ppid: 1}, // a shell outside the scope
		{pid: 100, ppid: 50}, {pid: 101, ppid: 100}, // systemd-run's env, the wrapper
		{pid: 200, ppid: 101},                        // the test binary
		{pid: 300, ppid: 200}, {pid: 301, ppid: 300}, // a program a test runs, and its child
		{pid: 400, ppid: 1}, // someone else
	}
	for _, c := range []struct {
		procs string
		want  bool
	}{
		{"100\n101\n200\n300\n301\n", true},
		{"200\n", true},
		{"101\n200\n400\n", false},
		{"200\nx\n", false},
	} {
		dir := fakeCgroups(t, map[string]string{"cgroup.procs": c.procs})
		if got := dedicatedCgroup(dir, procs, 200); got != c.want {
			t.Errorf("dedicatedCgroup(%q) = %v", c.procs, got)
		}
	}
	if dedicatedCgroup(t.TempDir(), procs, 200) {
		t.Error("a cgroup without cgroup.procs is dedicated")
	}
}

// fakeCgroups writes a cgroup v2 hierarchy under a new directory: files maps
// a path under the root ("a/b/memory.max") to its contents.
func fakeCgroups(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, data := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestReadCgroup(t *testing.T) {
	const self = "0::/user.slice/run.scope\n"
	for _, c := range []struct {
		name  string
		proc  string
		files map[string]string
		want  cgroupLimit
		ok    bool
	}{
		{
			name: "scope with MemoryMax and no swap",
			proc: self,
			files: map[string]string{
				"memory.max":                           "1024\n", // the root's files carry no limit and are not read
				"user.slice/memory.max":                "max\n",
				"user.slice/run.scope/memory.max":      "11811160064\n",
				"user.slice/run.scope/memory.high":     "max\n",
				"user.slice/run.scope/memory.swap.max": "0\n",
				"user.slice/run.scope/memory.current":  "1073741824\n",
				"user.slice/run.scope/memory.stat":     "anon 100\nfile 536870912\nfile_mapped 7\ninactive_file 3\n",
			},
			want: cgroupLimit{limit: 11264 * mb, avail: 11264*mb - 512*mb, swap: 0, swapLimited: true},
			ok:   true,
		},
		{
			name: "memory.high below memory.max",
			proc: self,
			files: map[string]string{
				"user.slice/run.scope/memory.max":     "8589934592\n",
				"user.slice/run.scope/memory.high":    "4294967296\n",
				"user.slice/run.scope/memory.current": "0\n",
			},
			want: cgroupLimit{limit: 4096 * mb, avail: 4096 * mb},
			ok:   true,
		},
		{
			name: "an ancestor is tighter",
			proc: self,
			files: map[string]string{
				"user.slice/memory.max":                "6442450944\n",
				"user.slice/memory.current":            "5368709120\n",
				"user.slice/run.scope/memory.max":      "11811160064\n",
				"user.slice/run.scope/memory.current":  "0\n",
				"user.slice/run.scope/memory.swap.max": "max\n",
			},
			want: cgroupLimit{limit: 6144 * mb, avail: 1024 * mb},
			ok:   true,
		},
		{
			name: "an ancestor has the lower limit but more headroom",
			proc: self,
			files: map[string]string{
				"user.slice/memory.max":               "10737418240\n",
				"user.slice/memory.current":           "1073741824\n",
				"user.slice/run.scope/memory.max":     "11811160064\n",
				"user.slice/run.scope/memory.current": "10737418240\n",
			},
			want: cgroupLimit{limit: 10240 * mb, avail: 1024 * mb},
			ok:   true,
		},
		{
			name: "usage past the limit leaves nothing",
			proc: self,
			files: map[string]string{
				"user.slice/run.scope/memory.max":     "1073741824\n",
				"user.slice/run.scope/memory.current": "2147483648\n",
			},
			want: cgroupLimit{limit: 1024 * mb, avail: 0},
			ok:   true,
		},
		{
			name: "no limits",
			proc: self,
			files: map[string]string{
				"user.slice/memory.max":           "max\n",
				"user.slice/run.scope/memory.max": "max\n",
			},
		},
		{
			name:  "cgroup v1 only",
			proc:  "12:memory:/user.slice\n1:name=systemd:/user.slice\n",
			files: map[string]string{"user.slice/memory.max": "1024\n"},
		},
		{
			name:  "unreadable limit",
			proc:  self,
			files: map[string]string{"user.slice/run.scope/memory.max": "lots\n"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, ok := readCgroup(c.proc, fakeCgroups(t, c.files))
			if ok != c.ok || (ok && got != c.want) {
				t.Errorf("readCgroup = %+v, %v; want %+v, %v", got, ok, c.want, c.ok)
			}
		})
	}
}

func TestWithCgroup(t *testing.T) {
	sys := memory{avail: 18000 * mb, ok: true}
	tight := cgroupLimit{limit: 11264 * mb, avail: 11000 * mb, swapLimited: true}
	m := withCgroup(sys, func() (cgroupLimit, bool) { return tight, true })
	if !m.ok || !m.inGroup || m.avail != 11000*mb {
		t.Errorf("a tighter cgroup: %+v", m)
	}
	for _, want := range []string{"11000 MB available in the cgroup", "limit 11264 MB", "no swap"} {
		if !strings.Contains(m.String(), want) {
			t.Errorf("%q lacks %q", m.String(), want)
		}
	}
	loose := cgroupLimit{limit: 64000 * mb, avail: 60000 * mb}
	if m := withCgroup(sys, func() (cgroupLimit, bool) { return loose, true }); m.inGroup || m.avail != 18000*mb {
		t.Errorf("a looser cgroup: %+v", m)
	}
	if m := withCgroup(sys, func() (cgroupLimit, bool) { return cgroupLimit{}, false }); m != sys {
		t.Errorf("no cgroup: %+v", m)
	}
	if m := withCgroup(memory{}, func() (cgroupLimit, bool) { return tight, true }); !m.ok || m.avail != 11000*mb {
		t.Errorf("no meminfo: %+v", m)
	}
	// The run's sizing follows: 11000 MB at 256 MB a slot is 42 slots, and
	// the watchdog's cap is 95% of it (a cgroup's headroom).
	if n := capParallel(64, m.avail, 256*mb, true); n != 42 {
		t.Errorf("capParallel in the cgroup = %d", n)
	}
	t.Setenv("PLUTO_TEST_RSS_MB", "")
	if c, on := rssCap(m.avail, m.ok, m.inGroup); !on || c != 10450*mb {
		t.Errorf("rssCap in the cgroup = %d MB, %v", c/mb, on)
	}
}

// TestPool checks that reservations wait for room, that one larger than the
// budget runs alone, and that releasing wakes the waiters.
func TestPool(t *testing.T) {
	p := newPool(8800*mb, nil)
	if p.acquire("a", 5000*mb) {
		t.Error("the first reservation waited")
	}
	got := make(chan string, 2)
	go func() { p.acquire("b", 5000*mb); got <- "b" }()
	select {
	case <-got:
		t.Fatal("two 5000 MB reservations held at once in 8800 MB")
	case <-time.After(50 * time.Millisecond):
	}
	if h := p.holders(); len(h) != 1 || h[0] != "a (5000 MB)" {
		t.Errorf("holders = %v", h)
	}
	p.release("a", 5000*mb)
	if n := <-got; n != "b" {
		t.Fatal(n)
	}
	// A reservation over the budget waits for b and then holds everything.
	go func() { p.acquire("huge", 15000*mb); got <- "huge" }()
	select {
	case <-got:
		t.Fatal("a reservation over the budget started beside another")
	case <-time.After(50 * time.Millisecond):
	}
	p.release("b", 5000*mb)
	<-got
	go func() { p.acquire("c", 100*mb); got <- "c" }()
	select {
	case <-got:
		t.Fatal("a reservation started beside one over the budget")
	case <-time.After(50 * time.Millisecond):
	}
	p.release("huge", 15000*mb)
	<-got
	p.release("c", 100*mb)
	if p.used != 0 || len(p.holders()) != 0 {
		t.Errorf("after release: used %d, holders %v", p.used, p.holders())
	}
	// No budget: nothing waits.
	free := newPool(0, nil)
	free.acquire("x", 1<<40)
	free.acquire("y", 1<<40)
}

// TestPoolDrains checks that a waiting large reservation stops small ones
// from starting, so that the pool empties for it.
func TestPoolDrains(t *testing.T) {
	p := newPool(9000*mb, nil)
	p.acquire("small1", 1000*mb)
	got := make(chan string, 2)
	go func() { p.acquire("big", 5000*mb); got <- "big" }()
	// Wait until the large one is waiting.
	for deadline := time.Now().Add(5 * time.Second); ; {
		p.mu.Lock()
		w := p.bigWaiting
		p.mu.Unlock()
		if w == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the large reservation never waited")
		}
		time.Sleep(time.Millisecond)
	}
	go func() { p.acquire("small2", 1000*mb); got <- "small2" }()
	select {
	case n := <-got:
		t.Fatalf("%s started while a large reservation waited for the pool to empty", n)
	case <-time.After(50 * time.Millisecond):
	}
	p.release("small1", 1000*mb)
	if n := <-got; n != "big" {
		t.Fatalf("%s started first, want the large reservation", n)
	}
	p.release("big", 5000*mb)
	if n := <-got; n != "small2" {
		t.Fatal(n)
	}
}

// TestPoolInUse checks that memory in use without a reservation counts
// while others are held: a reservation waits while the run uses too much,
// and starts when the use falls and the pool is woken. With nothing held,
// a reservation starts whatever the use.
func TestPoolInUse(t *testing.T) {
	var mu sync.Mutex
	inUse := uint64(7000 * mb)
	setUse := func(n uint64) {
		mu.Lock()
		inUse = n
		mu.Unlock()
	}
	p := newPool(8800*mb, func() uint64 {
		mu.Lock()
		defer mu.Unlock()
		return inUse
	})
	p.acquire("x", 100*mb)
	got := make(chan bool, 1)
	go func() { got <- p.acquire("a", 2000*mb) }()
	select {
	case <-got:
		t.Fatal("2000 MB reserved with 7000 MB of 8800 MB in use")
	case <-time.After(50 * time.Millisecond):
	}
	setUse(1000 * mb)
	p.wake()
	select {
	case waited := <-got:
		if !waited {
			t.Error("acquire did not report its wait")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the reservation did not start when the use fell")
	}
	// The reservations count in full before they are in use.
	if !p.fits(2900*mb, time.Now()) || p.fits(6800*mb, time.Now()) {
		t.Error("2100 MB reserved, 1000 MB in use: 2900 MB must fit and 6800 MB not")
	}
	setUse(6000 * mb)
	if p.fits(2900*mb, time.Now()) {
		t.Error("2900 MB fit with 6000 MB of 8800 MB in use")
	}
	// Nothing held: a small reservation starts whatever the use; a large
	// one waits for the use to fall, or for bigPatience, and then holds the
	// pool alone.
	p.release("a", 2000*mb)
	p.release("x", 100*mb)
	setUse(8000 * mb)
	if !p.fits(2000*mb, time.Now()) {
		t.Error("a small reservation did not fit with nothing held")
	}
	if p.fits(3000*mb, time.Now()) {
		t.Error("a large reservation fit with 8000 MB of 8800 MB in use")
	}
	if !p.fits(9000*mb, time.Now().Add(-bigPatience)) {
		t.Error("a large reservation still waited after bigPatience")
	}
	setUse(1000 * mb)
	if !p.fits(3000*mb, time.Now()) {
		t.Error("a large reservation did not fit with 1000 MB in use")
	}
	p.acquire("big", 3000*mb)
	setUse(0)
	if p.fits(1*mb, time.Now()) {
		t.Error("a reservation fit beside one of more than a third of the budget")
	}
	p.release("big", 3000*mb)
	if !p.fits(1*mb, time.Now()) {
		t.Error("the pool stayed closed after the large reservation ended")
	}
}

// TestReserve checks the test-facing wrapper: the reservation is held until
// release, release is idempotent, and the cleanup gives back what is held.
func TestReserve(t *testing.T) {
	saved := budget
	budget = newPool(1000*mb, nil)
	t.Cleanup(func() { budget = saved })
	t.Run("held", func(t *testing.T) {
		release := Reserve(t, 600)
		if budget.used != 600*mb {
			t.Errorf("used = %d MB", budget.used/mb)
		}
		if !strings.Contains(report(3<<30, 2<<30, nil), t.Name()+" (600 MB)") {
			t.Error("the report does not name the reservation")
		}
		release()
		release()
		if budget.used != 0 {
			t.Errorf("used after release = %d MB", budget.used/mb)
		}
	})
	t.Run("cleanup", func(t *testing.T) {
		Reserve(t, 700)
	})
	if budget.used != 0 {
		t.Errorf("the cleanup left %d MB reserved", budget.used/mb)
	}
}
