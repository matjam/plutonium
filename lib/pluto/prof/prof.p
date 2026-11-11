// Package prof profiles a running program: where it spends CPU time, and
// where it allocates memory. It writes profiles in the format of pprof
// (github.com/google/pprof), uncompressed, so that "go tool pprof" and
// the other pprof tools read them.
//
// The CPU profiler samples the stacks of the program's threads 100 times a
// second of CPU time, from a signal handler (pluto/rt, StartCPUProfile).
// The allocation profiler stands in front of the default allocator and
// records the stack of an allocation once every rate bytes allocated; each
// sample stands for rate bytes, and for as many objects of the sampled
// size as fit in them. Memory allocated through an allocator that a task
// installs for itself, such as an arena, is seen only as the arena's own
// requests for memory.
//
// Both are for hosted Linux programs.
package prof

import (
	"pluto/bytes"
	"pluto/errors"
	"pluto/os"
	"pluto/rt"
	"pluto/strings"
	"pluto/sync/atomic"
	"pluto/time"
	"pluto/vec"
	"unsafe"
)

const (
	cpuHz = 100

	// tableBytes is the size of a profile's table of stacks: its pages
	// are taken only as they fill.
	tableBytes = 256 << 20

	maxFrames = 128
)

var (
	cpuStart time.Time
	cpuOn    bool
)

// StartCPUProfile starts the CPU profiler.
func StartCPUProfile() error {
	if cpuOn {
		return errors.New("prof: the CPU profiler is already running")
	}
	if !rt.StartCPUProfile(cpuHz, tableBytes) {
		return errors.New("prof: cannot start the CPU profiler")
	}
	cpuStart = time.Now()
	cpuOn = true
	return nil
}

// StopCPUProfile stops the CPU profiler and writes its profile to the file
// name.
func StopCPUProfile(name string) error {
	if !cpuOn {
		return errors.New("prof: the CPU profiler is not running")
	}
	t := rt.StopCPUProfile()
	cpuOn = false
	p := profile{
		types:      [2][2]string{{"samples", "count"}, {"cpu", "nanoseconds"}},
		periodType: [2]string{"cpu", "nanoseconds"},
		period:     1000000000 / cpuHz,
		start:      cpuStart,
	}
	return p.write(name, t)
}

// The allocation profiler.

// allocSampler is the allocator the allocation profiler installs as the
// default, in front of the one it replaces. It has no state of its own:
// every task allocates through it, so its state is in package variables
// that a lock or atomic operations guard.
type allocSampler struct{}

// The allocation profiler's state. samplerMu guards samplerParent and
// samplerTab, since a thread may still be allocating through the sampler
// when a later StartAllocProfile sets them again; each run has a table of
// its own. samplerRate and samplerBytes are atomic. Only StartAllocProfile
// and StopAllocProfile use samplerOn and samplerStart.
var (
	samplerMu     rt.Lock
	samplerParent rt.Allocator
	samplerTab    *rt.ProfTable
	samplerRate   uint64
	samplerBytes  uint64 // allocated so far
	samplerOn     bool
	samplerStart  time.Time
)

func (s allocSampler) Alloc(size, align uintptr) unsafe.Pointer {
	n := atomic.AddUint64(&samplerBytes, uint64(size))
	rate := atomic.LoadUint64(&samplerRate)
	if hits := n/rate - (n-uint64(size))/rate; hits != 0 {
		s.sample(size, hits, rate)
	}
	samplerMu.Lock()
	p := samplerParent.Alloc(size, align)
	samplerMu.Unlock()
	return p
}

func (allocSampler) Free(p unsafe.Pointer, size, align uintptr) {
	samplerMu.Lock()
	samplerParent.Free(p, size, align)
	samplerMu.Unlock()
}

// sample records the stack of an allocation of size bytes that crossed hits
// multiples of the rate.
//
//pu:noinline
func (allocSampler) sample(size uintptr, hits, rate uint64) {
	var stk [maxFrames]uintptr
	n := rt.CallerPCs(1, stk[:])
	k := ownFrames(stk[:n])
	b := int64(hits * rate)
	objs := b / int64(max(size, 1))
	if objs < 1 {
		objs = 1
	}
	samplerMu.Lock()
	samplerTab.Add(stk[k:n], objs, b)
	samplerMu.Unlock()
}

// ownFrames reports how many leading pcs of stk are in this package (the
// sampler's Alloc and the interface wrapper the call came through), so the
// profile starts at the code that asked for the memory.
func ownFrames(stk []uintptr) int {
	var fr [16]rt.PCFrame
	for i, pc := range stk {
		m := rt.PCFrames(pc, fr[:])
		if m == 0 {
			return i
		}
		f := fr[m-1].Func
		if !strings.HasPrefix(f, "pluto/prof.") && !strings.HasPrefix(f, "*pluto/prof.") {
			return i
		}
	}
	return 0
}

// StartAllocProfile starts the allocation profiler, sampling once every
// rate bytes allocated (512 KiB if rate is 0).
func StartAllocProfile(rate int) error {
	if samplerOn {
		return errors.New("prof: the allocation profiler is already running")
	}
	if rate <= 0 {
		rate = 512 << 10
	}
	mem := rt.Mmap(tableBytes)
	if mem == nil {
		return errors.New("prof: no memory for the allocation profiler")
	}
	rt.IndexFuncs()
	// The table's header is at the start of its memory.
	hdr := (unsafe.Sizeof(rt.ProfTable{}) + 63) &^ 63
	tab := (*rt.ProfTable)(mem)
	rt.InitProfTable(tab, unsafe.Add(mem, hdr), tableBytes-hdr)
	samplerMu.Lock()
	samplerTab = tab
	samplerParent = rt.DefaultAllocator()
	samplerMu.Unlock()
	atomic.StoreUint64(&samplerRate, uint64(rate))
	atomic.StoreUint64(&samplerBytes, 0)
	samplerStart = time.Now()
	samplerOn = true
	rt.SetDefaultAllocator(allocSampler{})
	return nil
}

// StopAllocProfile stops the allocation profiler, puts back the default
// allocator it replaced, and writes its profile to the file name. The
// sampler's table is not freed, as other threads may still be finishing
// an allocation through it.
func StopAllocProfile(name string) error {
	if !samplerOn {
		return errors.New("prof: the allocation profiler is not running")
	}
	rt.SetDefaultAllocator(samplerParent)
	samplerOn = false
	p := profile{
		types:      [2][2]string{{"alloc_objects", "count"}, {"alloc_space", "bytes"}},
		periodType: [2]string{"space", "bytes"},
		period:     int64(atomic.LoadUint64(&samplerRate)),
		start:      samplerStart,
	}
	return p.write(name, samplerTab)
}

// Writing a profile.

// profile describes the profile a table of stacks is written as: the two
// kinds of value its sums are, what the sampling period counts, and when
// sampling began.
type profile struct {
	types      [2][2]string // type and unit of each value
	periodType [2]string
	period     int64
	start      time.Time
}

// Fields of the pprof messages (profile.proto).
const (
	profSampleType    = 1
	profSample        = 2
	profMapping       = 3
	profLocation      = 4
	profFunction      = 5
	profStringTable   = 6
	profTimeNanos     = 9
	profDurationNanos = 10
	profPeriodType    = 11
	profPeriod        = 12
)

// A writer builds the message of a profile.
type writer owner struct {
	out     bytes.Buffer
	msg     bytes.Buffer // the message being built for out
	sub     bytes.Buffer // a message inside msg
	strs    map[string]int64
	strList vec.Vec[string]
	funcs   map[string]uint64
	locs    map[uintptr]uint64
}

func (p *profile) write(name string, t *rt.ProfTable) error {
	var w writer
	w.strs = make(map[string]int64)
	w.funcs = make(map[string]uint64)
	w.locs = make(map[uintptr]uint64)
	w.str("")
	for _, ty := range p.types {
		w.valueType(profSampleType, ty[0], ty[1])
	}
	// One mapping that says the profile is symbolized, so that pprof
	// does not look for the binary to name the addresses.
	w.msg.Reset()
	putUint(&w.msg, 1, 1)          // id
	putUint(&w.msg, 3, ^uint64(0)) // memory_limit
	putUint(&w.msg, 7, 1)          // has_functions
	putUint(&w.msg, 8, 1)          // has_filenames
	putUint(&w.msg, 9, 1)          // has_line_numbers
	putUint(&w.msg, 10, 1)         // has_inline_frames
	putBytes(&w.out, profMapping, w.msg.Bytes())

	var ids [maxFrames]uint64
	for i := 0; i < t.Len(); i++ {
		stk, v0, v1, ok := t.Entry(i)
		if !ok {
			continue
		}
		n := min(len(stk), maxFrames)
		for j := 0; j < n; j++ {
			ids[j] = w.location(stk[j])
		}
		w.msg.Reset()
		w.sub.Reset()
		for j := 0; j < n; j++ {
			putVarint(&w.sub, ids[j])
		}
		putBytes(&w.msg, 1, w.sub.Bytes()) // location_id, packed
		w.sub.Reset()
		putVarint(&w.sub, uint64(v0))
		putVarint(&w.sub, uint64(v1))
		putBytes(&w.msg, 2, w.sub.Bytes()) // value, packed
		putBytes(&w.out, profSample, w.msg.Bytes())
	}
	if d := atomic.LoadUint64(&t.Dropped); d != 0 {
		println("prof:", d, "samples dropped: the profile's table is full")
	}
	now := time.Now()
	putUint(&w.out, profTimeNanos, uint64(p.start.UnixNano()))
	putUint(&w.out, profDurationNanos, uint64(now.Sub(p.start).Nanoseconds()))
	w.valueType(profPeriodType, p.periodType[0], p.periodType[1])
	putUint(&w.out, profPeriod, uint64(p.period))
	// The string table goes last: every message before it has added its
	// strings.
	for i := 0; i < w.strList.Len(); i++ {
		putString(&w.out, profStringTable, w.strList.At(i))
	}
	return os.WriteFile(name, w.out.Bytes(), 0o644)
}

// str returns the index of s in the string table, adding it if new.
func (w *writer) str(s string) int64 {
	if i, ok := w.strs[s]; ok {
		return i
	}
	i := int64(w.strList.Len())
	w.strs[s] = i
	w.strList.Push(s)
	return i
}

func (w *writer) valueType(field int, ty, unit string) {
	w.msg.Reset()
	putUint(&w.msg, 1, uint64(w.str(ty)))
	putUint(&w.msg, 2, uint64(w.str(unit)))
	putBytes(&w.out, field, w.msg.Bytes())
}

// location returns the id of the location of pc, writing it, and the
// functions it names, the first time.
func (w *writer) location(pc uintptr) uint64 {
	if id, ok := w.locs[pc]; ok {
		return id
	}
	id := uint64(len(w.locs) + 1)
	w.locs[pc] = id
	var frames [32]rt.PCFrame
	n := rt.PCFrames(pc, frames[:])
	var fids [32]uint64
	for i := 0; i < n; i++ {
		fids[i] = w.function(frames[i].Func, frames[i].File)
	}
	w.msg.Reset()
	putUint(&w.msg, 1, id)
	putUint(&w.msg, 2, 1) // mapping_id
	putUint(&w.msg, 3, uint64(pc))
	for i := 0; i < n; i++ {
		w.sub.Reset()
		putUint(&w.sub, 1, fids[i])
		putUint(&w.sub, 2, uint64(frames[i].Line))
		putBytes(&w.msg, 4, w.sub.Bytes()) // line
	}
	putBytes(&w.out, profLocation, w.msg.Bytes())
	return id
}

// function returns the id of the function name in file, writing it the
// first time.
func (w *writer) function(name, file string) uint64 {
	key := name + "\x00" + file
	if id, ok := w.funcs[key]; ok {
		return id
	}
	id := uint64(len(w.funcs) + 1)
	w.funcs[key] = id
	w.msg.Reset()
	putUint(&w.msg, 1, id)
	putUint(&w.msg, 2, uint64(w.str(name)))
	putUint(&w.msg, 3, uint64(w.str(name))) // system_name
	putUint(&w.msg, 4, uint64(w.str(file)))
	putBytes(&w.out, profFunction, w.msg.Bytes())
	return id
}

// Protocol buffer encoding.

func putVarint(b *bytes.Buffer, v uint64) {
	for v >= 0x80 {
		b.WriteByte(byte(v) | 0x80)
		v >>= 7
	}
	b.WriteByte(byte(v))
}

// putUint writes a varint field, leaving out a zero, the default.
func putUint(b *bytes.Buffer, field int, v uint64) {
	if v == 0 {
		return
	}
	putVarint(b, uint64(field)<<3)
	putVarint(b, v)
}

func putBytes(b *bytes.Buffer, field int, data []byte) {
	putVarint(b, uint64(field)<<3|2)
	putVarint(b, uint64(len(data)))
	b.Write(data)
}

func putString(b *bytes.Buffer, field int, s string) {
	putVarint(b, uint64(field)<<3|2)
	putVarint(b, uint64(len(s)))
	b.WriteString(s)
}
