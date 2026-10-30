package scratch

import (
	"pluto/alloc"
	"pluto/strings"
	"pluto/testing"
	"unsafe"

	"compiler/compat"
	"compiler/pu"
)

type node struct {
	v    int
	next *node
}

// fill allocates n nodes from the current allocator, linked.
func fill(n int) *node {
	var head *node
	for i := 0; i < n; i++ {
		head = pu.New(node{v: i, next: head})
	}
	return head
}

func sum(n *node) int {
	s := 0
	for ; n != nil; n = n.next {
		s += n.v
	}
	return s
}

// rss returns the process's resident memory in bytes (VmRSS), or 0 if
// /proc does not say.
func rss() int {
	data, err := compat.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, l := range compat.Split(compat.String(data), "\n") {
		if v, ok := strings.CutPrefix(l, "VmRSS:"); ok {
			n := 0
			for _, c := range v {
				if c >= '0' && c <= '9' {
					n = n*10 + int(c-'0')
				}
			}
			return n << 10
		}
	}
	return 0
}

// flat runs cycle many times and fails if memory grows by more than a few
// megabytes between the end of a warm-up and the end. With generation
// checks a cycle registers each allocation it makes; memory freed without
// ending those registrations grew by MB here.
func flat(t *testing.T, name string, cycle func(i int)) {
	for i := 0; i < 10; i++ {
		cycle(i)
	}
	before := rss()
	for i := 0; i < cycles; i++ {
		cycle(i)
	}
	after := rss()
	if before == 0 || after == 0 {
		t.Skip("no /proc/self/status")
	}
	grew := after - before
	t.Logf("%s: resident memory %d MB, grew %d KB over %d cycles", name, before>>20, grew>>10, cycles)
	if grew > 8<<20 {
		t.Errorf("%s: resident memory grew %d MB over %d cycles (from %d MB)", name, grew>>20, cycles, before>>20)
	}
}

const (
	cycles   = 100
	perCycle = 2000 // nodes allocated in a cycle
)

func TestRegionCyclesFlat(t *testing.T) {
	flat(t, "Region", func(i int) {
		r := NewRegion()
		u := r.Use()
		n := fill(perCycle)
		// regionOf returns the Region's plain address; r has its tag.
		if uintptr(unsafe.Pointer(regionOf(unsafe.Pointer(n)))) != uintptr(unsafe.Pointer(r)) {
			t.Fatal("a region's allocation is not found in it")
		}
		u.Restore()
		if sum(n) != perCycle*(perCycle-1)/2 {
			t.Fatal("a region's allocations were overwritten")
		}
		r.Release()
	})
	if n, used := Regions(); n != 0 || used != 0 {
		t.Errorf("after Release: %d regions of %d bytes live, want none", n, used)
	}
}

// TestRegionMixedFlat makes in each region what a loaded program makes:
// slices grown by append past a chunk, a map and nodes, of sizes that
// change from cycle to cycle. With generation checks the tags an address
// has had only go up, and an allocation takes one above the highest of its
// range. A Region whose allocations were registered one by one and never
// ended ran out of tags 55 cycles in, and its Alloc returned nil.
func TestRegionMixedFlat(t *testing.T) {
	flat(t, "Region mixed", func(i int) {
		r := NewRegion()
		u := r.Use()
		ok := mixed(i)
		u.Restore()
		if !ok {
			t.Fatal("a region's allocations were overwritten")
		}
		r.Release()
	})
}

// mixed makes slices, a map and nodes from the current allocator, and
// reports whether they hold what was put in them. The map is dropped
// before mixed returns, while its memory is live.
func mixed(i int) bool {
	var xs []int
	for j := 0; j < 20000+(i%7)*5000; j++ {
		xs = compat.Append(xs, j)
	}
	m := map[int]*node{}
	for j := 0; j < 500+(i%5)*200; j++ {
		m[j] = pu.New(node{v: j})
	}
	n := fill(perCycle / 2)
	return xs[len(xs)-1] == len(xs)-1 && m[7].v == 7 && sum(n) == (perCycle/2)*(perCycle/2-1)/2
}

func TestRegionUsed(t *testing.T) {
	r := NewRegion()
	if r.Used() != 0 {
		t.Errorf("an empty region uses %d bytes", r.Used())
	}
	u := r.Use()
	fill(10)
	u.Restore()
	if r.Used() == 0 {
		t.Error("a region holding nodes uses no bytes")
	}
	if n, used := Regions(); n != 1 || used != r.Used() {
		t.Errorf("Regions() = %d, %d; want 1, %d", n, used, r.Used())
	}
	r.Release()
}

type keepStep struct {
	work *node
	kept *node
}

func (s *keepStep) ScratchWork() { s.work = fill(perCycle) }
func (s *keepStep) ScratchKeep() { s.kept = pu.New(node{v: sum(s.work)}) }

var homeKeys [8]int

func TestHomeCyclesFlat(t *testing.T) {
	flat(t, "DoHome", func(i int) {
		var s keepStep
		DoHome(unsafe.Pointer(&homeKeys[i%len(homeKeys)]), &s)
		if s.kept.v != perCycle*(perCycle-1)/2 {
			t.Fatal("a kept result was overwritten")
		}
	})
	flat(t, "BeginHome", func(i int) {
		key := unsafe.Pointer(&homeKeys[i%len(homeKeys)])
		BeginHome(key)
		n := fill(perCycle)
		EndHome()
		if sum(n) != perCycle*(perCycle-1)/2 {
			t.Fatal("a home's allocations were overwritten")
		}
		Drop(key)
	})
	for i := range homeKeys {
		Drop(unsafe.Pointer(&homeKeys[i]))
	}
}

func TestDoCyclesFlat(t *testing.T) {
	flat(t, "Do", func(i int) {
		var s keepStep
		Do(&s)
		if s.kept.v != perCycle*(perCycle-1)/2 {
			t.Fatal("a kept result was overwritten")
		}
	})
}

func TestWorkerCyclesFlat(t *testing.T) {
	shared := NewLocked()
	w := NewWorker(&shared)
	flat(t, "Worker", func(i int) {
		var s keepStep
		w.Do(&s)
		if s.kept.v != perCycle*(perCycle-1)/2 {
			t.Fatal("a kept result was overwritten")
		}
		m := w.Mark()
		u := alloc.Use(&w)
		n := fill(perCycle)
		u.Restore()
		if sum(n) != perCycle*(perCycle-1)/2 {
			t.Fatal("a worker's allocations were overwritten")
		}
		w.Release(m)
	})
}
