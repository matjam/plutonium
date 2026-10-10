package testguard

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestParseMemAvailable(t *testing.T) {
	const meminfo = "MemTotal:       32448000 kB\nMemFree:         1000000 kB\nMemAvailable:   14261236 kB\nBuffers: 1 kB\n"
	got, ok := parseMemAvailable(strings.NewReader(meminfo))
	if !ok || got != 14261236<<10 {
		t.Errorf("parseMemAvailable = %d, %v", got, ok)
	}
	for _, bad := range []string{"", "MemTotal: 1 kB\n", "MemAvailable:\n", "MemAvailable: lots kB\n"} {
		if _, ok := parseMemAvailable(strings.NewReader(bad)); ok {
			t.Errorf("parseMemAvailable(%q) succeeded", bad)
		}
	}
}

func TestParseStatm(t *testing.T) {
	got, ok := parseStatm("5000 250 100 20 0 300 0\n", 4096)
	if !ok || got != 250*4096 {
		t.Errorf("parseStatm = %d, %v", got, ok)
	}
	for _, bad := range []string{"", "5000", "5000 x"} {
		if _, ok := parseStatm(bad, 4096); ok {
			t.Errorf("parseStatm(%q) succeeded", bad)
		}
	}
}

func TestCapParallel(t *testing.T) {
	const mb = 1 << 20
	for _, c := range []struct {
		want          int
		avail, budget uint64
		on            bool
		got           int
	}{
		{24, 14000 * mb, 256 * mb, true, 24},  // plenty of memory
		{24, 3000 * mb, 256 * mb, true, 11},   // short
		{24, 100 * mb, 256 * mb, true, 1},     // never below one
		{24, 3000 * mb, 256 * mb, false, 24},  // switched off
		{24, 3000 * mb, 0, true, 24},          // no budget
		{1, 3000 * mb, 256 * mb, true, 1},     // already serial
		{8, 14000 * mb, 1000 * mb, true, 8},   // 14 slots fit
		{16, 14000 * mb, 1000 * mb, true, 14}, // 14 slots fit
	} {
		if got := capParallel(c.want, c.avail, c.budget, c.on); got != c.got {
			t.Errorf("capParallel(%d, %d MB, %d MB, %v) = %d, want %d", c.want, c.avail/mb, c.budget/mb, c.on, got, c.got)
		}
	}
}

func TestRSSCap(t *testing.T) {
	const gb = 1 << 30
	t.Setenv("PLUTO_TEST_RSS_MB", "")
	if got, on := rssCap(10*gb, true); !on || got != 8*gb {
		t.Errorf("default cap at 10 GB available = %d, %v", got, on)
	}
	if got, on := rssCap(30*gb, true); !on || got != 12*gb {
		t.Errorf("default cap at 30 GB available = %d, %v", got, on)
	}
	if _, on := rssCap(0, false); on {
		t.Error("watchdog on without /proc/meminfo")
	}
	t.Setenv("PLUTO_TEST_RSS_MB", "2048")
	if got, on := rssCap(30*gb, true); !on || got != 2*gb {
		t.Errorf("cap with override = %d, %v", got, on)
	}
	t.Setenv("PLUTO_TEST_RSS_MB", "0")
	if _, on := rssCap(30*gb, true); on {
		t.Error("PLUTO_TEST_RSS_MB=0 left the watchdog on")
	}
}

func TestEnvMB(t *testing.T) {
	t.Setenv("PLUTO_TEST_SLOT_MB", "")
	if b, on := envMB("PLUTO_TEST_SLOT_MB", 256); !on || b != 256<<20 {
		t.Errorf("default = %d, %v", b, on)
	}
	t.Setenv("PLUTO_TEST_SLOT_MB", "512")
	if b, on := envMB("PLUTO_TEST_SLOT_MB", 256); !on || b != 512<<20 {
		t.Errorf("override = %d, %v", b, on)
	}
	t.Setenv("PLUTO_TEST_SLOT_MB", "0")
	if _, on := envMB("PLUTO_TEST_SLOT_MB", 256); on {
		t.Error("0 did not turn the budget off")
	}
}

// TestRunningNames checks that Parallel registers a test while it runs, that
// a parent is left out in favor of its subtests.
func TestRunningNames(t *testing.T) {
	Parallel(t)
	if !contains(running(), t.Name()) {
		t.Fatalf("running() = %v, missing %s", running(), t.Name())
	}
	t.Run("sub", func(t *testing.T) {
		Parallel(t)
		names := running()
		if !contains(names, t.Name()) || contains(names, "TestRunningNames") {
			t.Errorf("running() = %v, want the subtest and not its parent", names)
		}
		if !strings.Contains(report(3<<30, 2<<30), t.Name()) {
			t.Errorf("report does not name %s", t.Name())
		}
	})
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestReport(t *testing.T) {
	r := report(3<<30, 2<<30)
	for _, want := range []string{"3072 MB", "cap of 2048 MB", "PLUTO_TEST_RSS_MB", "TestReport"} {
		if !strings.Contains(r, want) {
			t.Errorf("report lacks %q:\n%s", want, r)
		}
	}
}

// TestWatch checks that the watchdog calls stop once the resident size is
// over the limit, and not before.
func TestWatch(t *testing.T) {
	if _, ok := readRSS(); !ok {
		t.Skip("no /proc/self/statm")
	}
	done := make(chan string, 1)
	go watch(1<<10, 5*time.Millisecond, func(r string) { done <- r })
	select {
	case r := <-done:
		if !strings.Contains(r, "passed the cap") {
			t.Errorf("report = %q", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watchdog did not stop a process over its limit")
	}
	quiet := make(chan string, 1)
	go watch(1<<50, 5*time.Millisecond, func(r string) { quiet <- r })
	select {
	case r := <-quiet:
		t.Errorf("watchdog stopped a process under its limit: %s", r)
	case <-time.After(100 * time.Millisecond):
	}
}

// TestReportTruncates checks that a report names at most maxListed tests.
func TestReportTruncates(t *testing.T) {
	mu.Lock()
	for i := range maxListed + 5 {
		inFlight[fmt.Sprintf("TestFake/%03d", i)] = time.Now()
	}
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		for i := range maxListed + 5 {
			delete(inFlight, fmt.Sprintf("TestFake/%03d", i))
		}
	})
	r := report(3<<30, 2<<30)
	if !strings.Contains(r, "... and 5 more") || strings.Count(r, "TestFake/") != maxListed {
		t.Errorf("report does not stop at %d tests:\n%s", maxListed, r)
	}
}
