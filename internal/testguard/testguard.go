// Package testguard keeps a parallel test suite from taking the machine down.
//
// The compiler's end-to-end tests build hundreds of programs at once, up to
// -parallel of them. If a compiler change makes one compile use several times
// the memory it used to, the parallelism multiplies that, and a 30 GB machine
// starts to swap. A test binary calls Run from TestMain, which
//
//   - lowers -parallel so that the slots, at an assumed memory budget each,
//     fit in the memory available (unless -parallel was given explicitly),
//     and
//   - starts a watchdog that samples the process's resident memory and, past
//     a cap, stops the run with the names of the tests in flight.
//
// The memory available is the lesser of MemAvailable and what the process's
// cgroup v2 limits (memory.max and memory.high, of its cgroup and its
// ancestors) leave it, so a run confined by systemd-run -p MemoryMax=...
// sizes itself to the confinement rather than to the machine.
//
// Tests call Parallel instead of t.Parallel so that the watchdog knows which
// tests are running. A test step that needs far more than a slot (a build of
// a translated compiler takes GB) calls Reserve with its estimate first: the
// reservations share a budget the size of the watchdog's cap, so two such
// steps do not start together unless both fit. Two environment variables
// adjust the limits:
//
//	PLUTO_TEST_SLOT_MB   memory budget of one parallel slot in MB (default
//	                     256); 0 turns the -parallel reduction off
//	PLUTO_TEST_RSS_MB    resident memory cap of the test binary in MB
//	                     (default: 80% of what was available at start, but
//	                     at most 12 GB); 0 turns the watchdog off, and
//	                     with it the limit on reservations
//
// All of this relies on /proc and does nothing where it is missing.
package testguard

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	// DefaultSlotMB is the memory one parallel test slot is assumed to
	// need. A compile peaks at about 100 MB under the soft memory limit
	// TestMain sets, so this leaves room for the bigger ones.
	DefaultSlotMB = 256
	// DefaultMaxCapMB is the largest default cap on the test binary's
	// resident memory.
	DefaultMaxCapMB = 12 << 10
	// maxListed is the most running tests a report names.
	maxListed = 40
	// sampleInterval is how often the watchdog reads the resident size.
	sampleInterval = time.Second
)

// Run prepares the process for m.Run and returns its exit status. It parses
// the command line, lowers the parallelism if memory is short, starts the
// watchdog, and then calls tune (which may be nil) with the number of
// parallel slots the run will use, so the caller can size its own limits.
func Run(m *testing.M, tune func(parallel int)) int {
	flag.Parse()
	mem := availableMemory()
	avail, haveAvail := mem.avail, mem.ok
	parallel := 0
	if f := flag.Lookup("test.parallel"); f != nil {
		if g, ok := f.Value.(flag.Getter); ok {
			parallel, _ = g.Get().(int)
		}
		if haveAvail && !flagSet("test.parallel") {
			slot, on := envMB("PLUTO_TEST_SLOT_MB", DefaultSlotMB)
			if n := capParallel(parallel, avail, slot, on); n != parallel {
				fmt.Fprintf(os.Stderr, "testguard: -parallel lowered from %d to %d: %s at %d MB per slot (set -parallel to choose, PLUTO_TEST_SLOT_MB to change the budget)\n",
					parallel, n, mem, slot>>20)
				parallel = n
				_ = f.Value.Set(strconv.Itoa(n))
			}
		}
	}
	if tune != nil {
		tune(parallel)
	}
	if capBytes, on := rssCap(avail, haveAvail); on {
		if _, ok := readRSS(); ok {
			budget = newPool(capBytes)
			go watch(capBytes, sampleInterval, exit)
		}
	}
	return m.Run()
}

// capParallel returns the parallelism to use: want, lowered so that slots of
// budget bytes each fit in avail bytes, but never below 1. A budget switched
// off (on is false) leaves want alone.
func capParallel(want int, avail, budget uint64, on bool) int {
	if !on || budget == 0 || want <= 1 {
		return want
	}
	return max(1, min(want, int(avail/budget)))
}

// rssCap returns the cap on the process's resident memory in bytes, and
// whether the watchdog is on.
func rssCap(avail uint64, haveAvail bool) (uint64, bool) {
	if v, set := os.LookupEnv("PLUTO_TEST_RSS_MB"); set {
		if n, err := strconv.ParseUint(strings.TrimSpace(v), 10, 63); err == nil {
			return n << 20, n != 0
		}
	}
	if !haveAvail {
		return 0, false
	}
	return min(avail/10*8, DefaultMaxCapMB<<20), true
}

// envMB reads a size in megabytes from the environment and returns it in
// bytes; on is false if it is 0, which turns the feature off.
func envMB(name string, def uint64) (bytes uint64, on bool) {
	n := def
	if v, ok := os.LookupEnv(name); ok {
		if u, err := strconv.ParseUint(strings.TrimSpace(v), 10, 63); err == nil {
			n = u
		}
	}
	return n << 20, n != 0
}

// flagSet reports whether the flag was given on the command line.
func flagSet(name string) bool {
	set := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

func mbString(b uint64) string {
	return fmt.Sprintf("%d MB", b>>20)
}

// memory is the memory a test run may use.
type memory struct {
	avail   uint64      // bytes
	ok      bool        // avail is known
	cgroup  cgroupLimit // the cgroup's limits, if inGroup
	inGroup bool        // the cgroup's headroom is less than MemAvailable
}

// String says how much is available and where the figure comes from.
func (m memory) String() string {
	if !m.inGroup {
		return mbString(m.avail) + " available"
	}
	swap := "swap unlimited"
	switch {
	case m.cgroup.swapLimited && m.cgroup.swap == 0:
		swap = "no swap"
	case m.cgroup.swapLimited:
		swap = "swap at most " + mbString(m.cgroup.swap)
	}
	return fmt.Sprintf("%s available in the cgroup (limit %s, %s)", mbString(m.avail), mbString(m.cgroup.limit), swap)
}

// availableMemory returns the memory the process may use: the lesser of
// MemAvailable from /proc/meminfo and the headroom its cgroup v2 limits
// (memory.max and memory.high of its cgroup and of every ancestor) leave it.
func availableMemory() memory {
	var m memory
	if f, err := os.Open("/proc/meminfo"); err == nil {
		m.avail, m.ok = parseMemAvailable(f)
		_ = f.Close()
	}
	return withCgroup(m, cgroupMemory)
}

// withCgroup lowers m to the headroom of the cgroup that read returns.
func withCgroup(m memory, read func() (cgroupLimit, bool)) memory {
	cl, ok := read()
	if !ok {
		return m
	}
	if !m.ok || cl.avail < m.avail {
		m.avail, m.ok, m.inGroup, m.cgroup = cl.avail, true, true, cl
	}
	return m
}

// parseMemAvailable reads the MemAvailable line of a /proc/meminfo.
func parseMemAvailable(r io.Reader) (uint64, bool) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		rest, ok := strings.CutPrefix(sc.Text(), "MemAvailable:")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			return 0, false
		}
		kb, err := strconv.ParseUint(fields[0], 10, 63)
		if err != nil {
			return 0, false
		}
		return kb << 10, true
	}
	return 0, false
}

// readRSS returns the resident memory of this process in bytes.
func readRSS() (uint64, bool) {
	b, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0, false
	}
	return parseStatm(string(b), uint64(os.Getpagesize()))
}

// parseStatm returns the resident set size, the second field of
// /proc/self/statm, which counts pages.
func parseStatm(s string, pageSize uint64) (uint64, bool) {
	fields := strings.Fields(s)
	if len(fields) < 2 {
		return 0, false
	}
	pages, err := strconv.ParseUint(fields[1], 10, 63)
	if err != nil {
		return 0, false
	}
	return pages * pageSize, true
}

// watch samples the resident memory every interval and calls stop with a
// report if it ever exceeds limit.
func watch(limit uint64, interval time.Duration, stop func(report string)) {
	for range time.Tick(interval) {
		rss, ok := readRSS()
		if !ok || rss <= limit {
			continue
		}
		stop(report(rss, limit))
		return
	}
}

// exit prints the report and ends the process with a failure status.
func exit(report string) {
	fmt.Fprint(os.Stderr, report)
	fmt.Fprintln(os.Stderr, "FAIL: test binary stopped by testguard")
	os.Exit(2)
}

// report describes a memory overrun: the sizes, the tests in flight, and the
// test functions on any goroutine stack (which also covers tests that run
// sequentially and so are not registered).
func report(rss, limit uint64) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\ntestguard: resident memory %s passed the cap of %s; stopping the run.\n", mbString(rss), mbString(limit))
	fmt.Fprintf(&b, "Memory is probably being used by one of these tests (oldest first):\n")
	names := running()
	for i, n := range names {
		if i == maxListed {
			fmt.Fprintf(&b, "\t... and %d more\n", len(names)-i)
			break
		}
		fmt.Fprintf(&b, "\t%s\n", n)
	}
	if len(names) == 0 {
		fmt.Fprintf(&b, "\t(no parallel test registered)\n")
	}
	if held := budget.holders(); len(held) > 0 {
		fmt.Fprintf(&b, "Tests holding memory reservations (testguard.Reserve):\n")
		for _, h := range held {
			fmt.Fprintf(&b, "\t%s\n", h)
		}
	}
	if fns := stackTests(); len(fns) > 0 {
		fmt.Fprintf(&b, "Test functions running on goroutine stacks:\n")
		for _, f := range fns {
			fmt.Fprintf(&b, "\t%s\n", f)
		}
	}
	fmt.Fprintf(&b, "Raise the cap with PLUTO_TEST_RSS_MB (0 turns it off), or lower -parallel.\n")
	return b.String()
}

// testFrame matches a test function in a stack frame line.
var testFrame = regexp.MustCompile(`\.(Test(?:[A-Z0-9_]\w*)?)[.(]`)

// stackTests returns the distinct test functions that appear on the stacks of
// goroutines that are not waiting in t.Parallel for their turn to run.
func stackTests() []string {
	buf := make([]byte, 1<<20)
	buf = buf[:runtime.Stack(buf, true)]
	seen := map[string]bool{}
	var out []string
	for _, g := range strings.Split(string(buf), "\n\n") {
		if strings.Contains(g, "testing.(*T).Parallel(") {
			continue
		}
		for _, line := range strings.Split(g, "\n") {
			if strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "goroutine ") {
				continue
			}
			if m := testFrame.FindStringSubmatch(line); m != nil && m[1] != "TestMain" && !seen[m[1]] {
				seen[m[1]] = true
				out = append(out, m[1])
			}
		}
	}
	sort.Strings(out)
	return out
}

var (
	mu       sync.Mutex
	inFlight = map[string]time.Time{}
)

// Parallel calls t.Parallel and records the test as running until it
// finishes, so the watchdog can name it.
func Parallel(t *testing.T) {
	t.Parallel()
	name := t.Name()
	mu.Lock()
	inFlight[name] = time.Now()
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		delete(inFlight, name)
		mu.Unlock()
	})
}

// running returns the names of the registered tests that have no registered
// subtest of their own (a parent test is running only through its
// subtests), oldest first.
func running() []string {
	mu.Lock()
	defer mu.Unlock()
	var names []string
	for n := range inFlight {
		leaf := true
		for m := range inFlight {
			if strings.HasPrefix(m, n+"/") {
				leaf = false
				break
			}
		}
		if leaf {
			names = append(names, n)
		}
	}
	sort.Slice(names, func(i, j int) bool {
		ti, tj := inFlight[names[i]], inFlight[names[j]]
		if !ti.Equal(tj) {
			return ti.Before(tj)
		}
		return names[i] < names[j]
	})
	return names
}
