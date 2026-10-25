package rt

import "unsafe"

// ----------------------------------------------------------------------------
// Profiling. A profiler records stacks of program counters: a CPU profiler
// from a signal handler (prof_linux.p), an allocation profiler from an
// allocator. Both must record without allocating and without locks, so
// stacks go into a ProfTable, a fixed hash table that counts each distinct
// stack, in memory the profiler supplies. pluto/prof writes the tables in
// the pprof format, naming the addresses with PCFrames.

// funcIndexed says findFunc may search the trace table by address: either
// the table is sorted by address (funcIndex nil), or funcIndex holds its
// entries in address order. IndexFuncs sets it.
var (
	funcIndexed bool
	funcIndex   unsafe.Pointer
)

// indexedFunc returns the i'th function in address order.
func indexedFunc(tab unsafe.Pointer, i int) unsafe.Pointer {
	if funcIndex != nil {
		return *(*unsafe.Pointer)(unsafe.Add(funcIndex, i*8))
	}
	return unsafe.Add(tab, traceHeaderSize+i*traceFuncSize)
}

// heapSortFuncs sorts n table entry pointers at idx by start address.
func heapSortFuncs(idx unsafe.Pointer, n int) {
	at := func(i int) *unsafe.Pointer { return (*unsafe.Pointer)(unsafe.Add(idx, i*8)) }
	key := func(i int) uintptr { return *(*uintptr)(*at(i)) }
	sift := func(root, end int) {
		for {
			child := 2*root + 1
			if child >= end {
				return
			}
			if child+1 < end && key(child) < key(child+1) {
				child++
			}
			if key(root) >= key(child) {
				return
			}
			*at(root), *at(child) = *at(child), *at(root)
			root = child
		}
	}
	for i := n/2 - 1; i >= 0; i-- {
		sift(i, n)
	}
	for end := n - 1; end > 0; end-- {
		*at(0), *at(end) = *at(end), *at(0)
		sift(0, end)
	}
}

// walkPCs stores in out the code address of each frame of the walk, from
// the current frame out: the instruction for an exact frame, the call for
// the others. It returns how many it stored.
func walkPCs(fw *frameWalk, out []uintptr) int {
	n := 0
	for fw.fn != nil && n < len(out) {
		out[n] = fw.look()
		n++
		if fw.bottom() {
			break
		}
		fw.next()
	}
	return n
}

// CallerPCs stores in out the code addresses of the frames on the running
// stack, starting skip frames above its caller (0 is the caller itself),
// and returns how many it stored. Each is the address of the call, so
// PCFrames names the line that made it. It does not allocate, so an
// allocator may call it.
//
//pu:noinline
func CallerPCs(skip int, out []uintptr) int {
	var fr [3]uintptr
	callerFrame(&fr)
	var fw frameWalk
	fw.start(traceTable(), fr[0], fr[1], fr[2], false)
	for ; skip > 0 && fw.fn != nil; skip-- {
		if fw.bottom() {
			return 0
		}
		fw.next()
	}
	return walkPCs(&fw, out)
}

// A PCFrame names a place in the source: a function, and the file and line
// of the code.
type PCFrame struct {
	Func string
	File string
	Line int
}

// PCFrames stores in out the frames of the code at pc, a code address that
// CallerPCs or a profiler recorded: one for each inlined call the code is
// in, innermost first, then the function it was compiled into. It returns
// how many it stored, 0 if pc is not in a function the trace table
// describes. The strings are the table's and live as long as the program.
func PCFrames(pc uintptr, out []PCFrame) int {
	tab := traceTable()
	fn := findFunc(tab, pc)
	if fn == nil || len(out) == 0 {
		return 0
	}
	name := traceString(tab, *(*uint32)(unsafe.Add(fn, 12)))
	file, line, inl := findLine(tab, fn, pc-*(*uintptr)(fn))
	if name == "pluto.main" && !inlinedMain(tab, fn, inl) {
		// The program's start, which initialises packages; as in a stack
		// trace (printInlinedMain), only main.main inlined into it is the
		// program's own code.
		out[0] = PCFrame{Func: name}
		return 1
	}
	n := 0
	for inl != 0 && n < len(out)-1 {
		site := inlineSite(tab, fn, inl)
		out[n] = PCFrame{Func: traceString(tab, *(*uint32)(unsafe.Add(site, 4))), File: file, Line: line}
		n++
		file = traceString(tab, *(*uint32)(unsafe.Add(site, 8)))
		line = int(*(*uint32)(unsafe.Add(site, 12)))
		inl = int(*(*uint32)(site))
	}
	out[n] = PCFrame{Func: name, File: file, Line: line}
	return n + 1
}

// inlinedMain reports whether inline site inl of fn is main.main or is
// inlined into it.
func inlinedMain(tab, fn unsafe.Pointer, inl int) bool {
	for inl != 0 {
		site := inlineSite(tab, fn, inl)
		if strequal(traceString(tab, *(*uint32)(unsafe.Add(site, 4))), "main.main") {
			return true
		}
		inl = int(*(*uint32)(site))
	}
	return false
}

// FuncStart returns the address of the first instruction of the function
// containing pc, or 0.
func FuncStart(pc uintptr) uintptr {
	fn := findFunc(traceTable(), pc)
	if fn == nil {
		return 0
	}
	return *(*uintptr)(fn)
}

// A ProfTable counts stacks: for each distinct stack added, two sums (a
// CPU profile's sample count and time, an allocation profile's objects and
// bytes). Adding is lock-free and allocates nothing, so a signal handler
// and every thread of the program may add at once; reading it (Entry) is
// for when no one adds any more. A stack is told apart by a 64-bit hash.
// When the table or its stack store fills, further new stacks are counted
// in Dropped.
type ProfTable struct {
	slots   unsafe.Pointer // nslots profSlots
	nslots  uint64         // a power of two
	pcs     unsafe.Pointer // the stack store
	npcs    uint64
	used    uint64 // words of the stack store taken (atomic)
	Dropped uint64 // stacks not recorded for want of room (atomic)
}

type profSlot struct {
	key    uint64 // the stack's hash, or 0 for an empty slot
	at     uint32 // where the stack is in the store
	n      uint32 // its length
	v0, v1 uint64
	ready  uint32 // the stack is stored
	_      uint32
}

// profProbes bounds the search for a stack's slot.
const profProbes = 64

// InitProfTable makes t a table in the size bytes of memory at mem, which
// must be zero and stay untouched by anything else while t is in use.
func InitProfTable(t *ProfTable, mem unsafe.Pointer, size uintptr) {
	slotSize := unsafe.Sizeof(profSlot{})
	nslots := uint64(1)
	for uintptr(nslots*2)*slotSize <= size/4 {
		nslots *= 2
	}
	t.slots = mem
	t.nslots = nslots
	t.pcs = unsafe.Add(mem, uintptr(nslots)*slotSize)
	t.npcs = uint64((size - uintptr(nslots)*slotSize) / 8)
	t.used = 0
	t.Dropped = 0
}

func (t *ProfTable) slot(i uint64) *profSlot {
	return (*profSlot)(unsafe.Add(t.slots, uintptr(i&(t.nslots-1))*unsafe.Sizeof(profSlot{})))
}

// Add adds v0 and v1 to the sums of the stack stk, recording the stack if
// it is new. It reports false if there was no room for a new stack.
func (t *ProfTable) Add(stk []uintptr, v0, v1 int64) bool {
	h := uint64(len(stk))*0x9E3779B97F4A7C15 + 1
	for _, pc := range stk {
		h = (h ^ uint64(pc)) * 0x100000001B3
		h ^= h >> 29
	}
	if h == 0 {
		h = 1
	}
	for i := uint64(0); i < profProbes; i++ {
		s := t.slot(h + i)
		k := load64(&s.key)
		if k == 0 {
			// Take room for the stack first, so that a slot is never
			// claimed without one; a lost race wastes it.
			at := xadd64(&t.used, int64(len(stk))) - uint64(len(stk))
			if at+uint64(len(stk)) > t.npcs {
				xadd64(&t.Dropped, 1)
				return false
			}
			if cas64(&s.key, 0, h) {
				for j, pc := range stk {
					*(*uintptr)(unsafe.Add(t.pcs, uintptr(at+uint64(j))*8)) = pc
				}
				s.at, s.n = uint32(at), uint32(len(stk))
				xadd64(&s.v0, v0)
				xadd64(&s.v1, v1)
				store32(&s.ready, 1)
				return true
			}
			k = load64(&s.key)
		}
		if k == h {
			xadd64(&s.v0, v0)
			xadd64(&s.v1, v1)
			return true
		}
	}
	xadd64(&t.Dropped, 1)
	return false
}

// Len returns the number of slots, for Entry.
func (t *ProfTable) Len() int { return int(t.nslots) }

// Entry returns the stack in slot i and its sums, or false if the slot is
// empty. The stack is a view of the table's memory.
func (t *ProfTable) Entry(i int) ([]uintptr, int64, int64, bool) {
	s := t.slot(uint64(i))
	if load32(&s.ready) == 0 {
		return nil, 0, 0, false
	}
	stk := unsafe.Slice((*uintptr)(unsafe.Add(t.pcs, uintptr(s.at)*8)), int(s.n))
	return stk, int64(s.v0), int64(s.v1), true
}
