package testguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const mb = 1 << 20

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
				"user.slice/run.scope/memory.stat":     "anon 100\nfile 7\ninactive_file 536870912\nactive_file 3\n",
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
	// the watchdog's cap is 80% of it.
	if n := capParallel(64, m.avail, 256*mb, true); n != 42 {
		t.Errorf("capParallel in the cgroup = %d", n)
	}
	t.Setenv("PLUTO_TEST_RSS_MB", "")
	if c, on := rssCap(m.avail, m.ok); !on || c != 8800*mb {
		t.Errorf("rssCap in the cgroup = %d MB, %v", c/mb, on)
	}
}

// TestPool checks that reservations wait for room, that one larger than the
// budget runs alone, and that releasing wakes the waiters.
func TestPool(t *testing.T) {
	p := newPool(8800 * mb)
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
	free := newPool(0)
	free.acquire("x", 1<<40)
	free.acquire("y", 1<<40)
}

// TestReserve checks the test-facing wrapper: the reservation is held until
// release, release is idempotent, and the cleanup gives back what is held.
func TestReserve(t *testing.T) {
	saved := budget
	budget = newPool(1000 * mb)
	t.Cleanup(func() { budget = saved })
	t.Run("held", func(t *testing.T) {
		release := Reserve(t, 600)
		if budget.used != 600*mb {
			t.Errorf("used = %d MB", budget.used/mb)
		}
		if !strings.Contains(report(3<<30, 2<<30), t.Name()+" (600 MB)") {
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
