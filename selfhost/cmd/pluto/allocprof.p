package main

// The allocation profile of the self-hosted compiler. The Go build has
// allocprof.go, which does nothing (Go's own profiles cover it); this
// hand-written file, when PLUTO_ALLOCPROF is set, installs an allocator
// in front of the default one that, every sampleBytes bytes allocated,
// walks the stack and charges those bytes to the functions on it. At the
// end it prints, to standard error, the functions charged the most: flat,
// the first function outside the libraries; and cumulative, every
// function on the stack. The compiler's translated code does not free, so
// the bytes allocated are close to the memory it holds. It needs a build
// without generation checks (--gencheck=false): with them, the memory it
// passes on would be registered twice, by the heap and by it.

import (
	"pluto/os"
	"pluto/rt"
	"pluto/strings"
	"unsafe"
)

const (
	sampleBytes = 4 << 20
	reportBytes = 4 << 30 // a report every 4 GB, in case the build is killed
	profSlots   = 1 << 16
	profDepth   = 96
)

type profEntry struct {
	fn         unsafe.Pointer
	flat, cum  uintptr
}

// allocProf is the profiling allocator. Its table is fixed, so sampling
// allocates nothing.
type allocProf struct {
	parent rt.Allocator
	n      uintptr // bytes since the last sample
	total  uintptr
	stack  [profDepth]unsafe.Pointer
	tab    [profSlots]profEntry
}

var prof allocProf

func (p *allocProf) Alloc(size, align uintptr) unsafe.Pointer {
	p.n += size
	if p.n >= sampleBytes {
		p.sample()
	}
	return p.parent.Alloc(size, align)
}

func (p *allocProf) Free(ptr unsafe.Pointer, size, align uintptr) {
	p.parent.Free(ptr, size, align)
}

//pu:noinline
func (p *allocProf) sample() {
	b := p.n
	p.n = 0
	p.total += b
	if p.total/reportBytes != (p.total-b)/reportBytes {
		println("allocprof: at MB", p.total>>20)
		p.report(true, 25)
		p.report(false, 40)
	}
	n := rt.CallerStack(p.stack[:])
	flat := false
	for i := 0; i < n; i++ {
		fn := p.stack[i]
		dup := false
		for j := 0; j < i; j++ {
			if p.stack[j] == fn {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		e := p.entry(fn)
		if e == nil {
			return
		}
		e.cum += b
		if !flat && !libraryFunc(rt.FuncName(fn)) {
			e.flat += b
			flat = true
		}
	}
}

// libraryFunc reports whether name is a function of the libraries the
// compiler's code allocates through, rather than of the compiler.
func libraryFunc(name string) bool {
	return strings.HasPrefix(name, "pluto/") || strings.HasPrefix(name, "selfhost/compat") ||
		strings.HasPrefix(name, "selfhost/pu.") || strings.Contains(name, "allocProf")
}

func (p *allocProf) entry(fn unsafe.Pointer) *profEntry {
	h := (uintptr(fn) >> 4) * 0x9E3779B97F4A7C15
	for k := uintptr(0); k < profSlots; k++ {
		e := &p.tab[(h+k)&(profSlots-1)]
		if e.fn == fn {
			return e
		}
		if e.fn == nil {
			e.fn = fn
			return e
		}
	}
	return nil
}

// startAllocProfile installs the profiling allocator if PLUTO_ALLOCPROF is
// set, and returns the function that prints its report.
func startAllocProfile() func() {
	if os.GetenvLossy("PLUTO_ALLOCPROF") == "" {
		return func() {}
	}
	prof.parent = rt.DefaultAllocator()
	rt.SetDefaultAllocator(&prof)
	return func() {
		rt.SetDefaultAllocator(prof.parent)
		println("allocprof: total MB", prof.total>>20)
		prof.report(true, 60)
		prof.report(false, 60)
	}
}

// report prints the top functions charged the most, by flat or cumulative
// bytes. It picks them by repeated scans, so it allocates nothing.
func (p *allocProf) report(flat bool, top int) {
	if flat {
		println("allocprof: flat MB")
	} else {
		println("allocprof: cumulative MB")
	}
	var last uintptr = ^uintptr(0)
	var lastFn unsafe.Pointer
	for r := 0; r < top; r++ {
		var best *profEntry
		var bv uintptr
		for i := range p.tab {
			e := &p.tab[i]
			if e.fn == nil {
				continue
			}
			v := e.cum
			if flat {
				v = e.flat
			}
			// Entries are taken in order of (value desc, address asc).
			if v > last || (v == last && uintptr(e.fn) <= uintptr(lastFn)) {
				continue
			}
			if best == nil || v > bv || (v == bv && uintptr(e.fn) < uintptr(best.fn)) {
				best, bv = e, v
			}
		}
		if best == nil || bv == 0 {
			return
		}
		println("  ", bv>>20, rt.FuncName(best.fn))
		last, lastFn = bv, best.fn
	}
}
