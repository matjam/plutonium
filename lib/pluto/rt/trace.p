package rt

import "unsafe"

// ----------------------------------------------------------------------------
// Stack traces. The compiler emits a table (pluto.tracetab) describing every
// function: its code range and name, its source lines, the calls inlined
// into it, and the shape of its frame. Frames have a fixed layout (see
// docs/spec/debugging.md), so the table is enough to walk the stack without
// frame pointers, and to print where each frame is. Walking allocates
// nothing and needs no operating system, so it works in freestanding builds
// and before an allocator exists.

// callerFrame stores the program counter and stack pointer its caller will
// have after it returns, and the caller's RBP (trace_amd64.s).
func callerFrame(out *[3]uintptr)

// traceTable returns the address of pluto.tracetab (trace_amd64.s).
func traceTable() unsafe.Pointer

// Layout of the table; keep in sync with internal/debuginfo/trace.go.
const (
	traceHeaderSize    = 16
	traceFuncSize      = 32
	traceInlineSize    = 16
	traceFlagFP        = 1
	traceFlagInterrupt = 2
	maxTraceFrames     = 64
)

// Traceback prints the stack trace of the machine state pc, sp and bp (the
// RBP register), as captured by a fault or interrupt handler at the
// faulting instruction, to the console. A kernel can call it from its
// exception handlers; to stop with a panic instead, call PanicFault.
func Traceback(pc, sp, bp uintptr) {
	traceback(Console{}, pc, sp, bp, true, false)
}

// traceback prints to w the stack walked from pc, sp and bp. exact says pc
// is the faulting instruction rather than a return address. skipRuntime
// omits the innermost frames of package rt, which belong to the panic
// itself.
func traceback[W Writer](w W, pc, sp, bp uintptr, exact, skipRuntime bool) {
	tab := traceTable()
	for depth := 0; ; depth++ {
		if depth == maxTraceFrames {
			writeString(w, "    ...additional frames elided...\n")
			return
		}
		look := pc
		if !exact {
			look = pc - 1 // the call, not the instruction after it
		}
		fn := findFunc(tab, look)
		if fn == nil {
			if depth == 0 && exact && sp != 0 {
				// A fault in assembly the table does not describe, such
				// as an atomic operation: such functions are leaves with
				// no frame, so the return address is at the stack
				// pointer.
				pc = *(*uintptr)(unsafe.Pointer(sp))
				sp += 8
				exact = false
				continue
			}
			return
		}
		name := traceString(tab, *(*uint32)(unsafe.Add(fn, 12)))
		if name == "pluto.main" {
			// main.main may be inlined into it.
			printInlinedMain(w, tab, fn, look)
			return
		}
		if name == "pluto/rt.start" || name == "pluto/rt.taskMain" {
			// The bottom of the main thread's or a puroutine's stack.
			return
		}
		if skipRuntime && hasPrefix(name, "pluto/rt.") {
			// part of the panic
		} else {
			skipRuntime = false
			printFrame(w, tab, fn, name, look)
		}
		flags := *(*uint8)(unsafe.Add(fn, 23))
		if flags&traceFlagInterrupt != 0 {
			return // an interrupt frame has no caller to return to
		}
		var cfa, callerBP uintptr
		start := *(*uintptr)(fn)
		mask := *(*uint8)(unsafe.Add(fn, 22))
		prologueEnd := uintptr(*(*uint16)(unsafe.Add(fn, 20)))
		switch {
		case exact && pc-start < prologueEnd:
			cfa, callerBP = prologueCFA(pc-start, mask, flags&traceFlagFP != 0, sp, bp)
		case flags&traceFlagFP != 0:
			cfa = bp + 16
			callerBP = *(*uintptr)(unsafe.Pointer(bp))
		default:
			frameSize := uintptr(*(*uint32)(unsafe.Add(fn, 16)))
			cfa = sp + frameSize + 8*uintptr(popcount(mask)) + 8
			callerBP = bp
			if mask&2 != 0 {
				// RBP is pushed second, after RBX if that is saved.
				at := cfa - 16
				if mask&1 != 0 {
					at -= 8
				}
				callerBP = *(*uintptr)(unsafe.Pointer(at))
			}
		}
		if cfa <= sp || cfa&7 != 0 {
			return
		}
		pc = *(*uintptr)(unsafe.Pointer(cfa - 8))
		sp, bp = cfa, callerBP
		exact = false
	}
}

// prologueCFA returns the canonical frame address and the caller's RBP for
// a fault at offset off in a function's prologue, before the frame is
// complete. The prologue is "push rbp; mov rbp, rsp" for frame-pointer
// frames, then a push for each saved register (RBX and RBP are one byte,
// R12 to R15 two), then the stack adjustment.
func prologueCFA(off uintptr, mask uint8, fp bool, sp, bp uintptr) (uintptr, uintptr) {
	if fp {
		switch {
		case off == 0:
			return sp + 8, bp
		case off < 4:
			return sp + 16, bp
		}
		return bp + 16, *(*uintptr)(unsafe.Pointer(bp))
	}
	pos := uintptr(0)
	depth := uintptr(8)
	for bit := 0; bit < 6; bit++ {
		if mask&(1<<bit) == 0 {
			continue
		}
		if off <= pos {
			break
		}
		if bit < 2 {
			pos++
		} else {
			pos += 2
		}
		depth += 8
	}
	return sp + depth, bp
}

// findFunc returns the table entry of the function containing pc, or nil.
func findFunc(tab unsafe.Pointer, pc uintptr) unsafe.Pointer {
	n := int(*(*uint32)(tab))
	for i := 0; i < n; i++ {
		fn := unsafe.Add(tab, traceHeaderSize+i*traceFuncSize)
		start := *(*uintptr)(fn)
		size := uintptr(*(*uint32)(unsafe.Add(fn, 8)))
		if pc >= start && pc < start+size {
			return fn
		}
	}
	return nil
}

// printFrame prints the frames at pc in function fn: one for each inlined
// call it is in, innermost first, then fn itself, named name.
func printFrame[W Writer](w W, tab, fn unsafe.Pointer, name string, pc uintptr) {
	file, line := printInlined(w, tab, fn, pc, 0)
	printLocation(w, name, file, line)
}

// printInlined prints a frame for each inlined call the code at pc in fn is
// in, innermost first, up to and including inline site last (1-based within
// fn; 0 for all of them). It returns the file and line in the code the last
// one printed was inlined into.
func printInlined[W Writer](w W, tab, fn unsafe.Pointer, pc uintptr, last int) (string, int) {
	file, line, inl := findLine(tab, fn, pc-*(*uintptr)(fn))
	for inl != 0 {
		site := inlineSite(tab, fn, inl)
		printLocation(w, traceString(tab, *(*uint32)(unsafe.Add(site, 4))), file, line)
		file = traceString(tab, *(*uint32)(unsafe.Add(site, 8)))
		line = int(*(*uint32)(unsafe.Add(site, 12)))
		if inl == last {
			break
		}
		inl = int(*(*uint32)(site))
	}
	return file, line
}

// printInlinedMain prints the frames at pc in pluto.main (fn) if pc is in
// the code of main.main inlined there: main.main and the calls inlined into
// it. The rest of pluto.main, which initialises packages, is not part of
// the program's stack as its author sees it and prints nothing.
func printInlinedMain[W Writer](w W, tab, fn unsafe.Pointer, pc uintptr) {
	_, _, inl := findLine(tab, fn, pc-*(*uintptr)(fn))
	for inl != 0 {
		site := inlineSite(tab, fn, inl)
		if strequal(traceString(tab, *(*uint32)(unsafe.Add(site, 4))), "main.main") {
			printInlined(w, tab, fn, pc, inl)
			return
		}
		inl = int(*(*uint32)(site))
	}
}

// inlineSite returns the entry of fn's inline site inl (1-based).
func inlineSite(tab, fn unsafe.Pointer, inl int) unsafe.Pointer {
	firstInl := int(*(*uint32)(unsafe.Add(fn, 28)))
	inlines := unsafe.Add(tab, int(*(*uint32)(unsafe.Add(tab, 12))))
	return unsafe.Add(inlines, (firstInl+inl-1)*traceInlineSize)
}

// traceSymbolize is the default Symbolizer (see PrintPC): it names the code
// at pc from the trace table. Where pc lies in inlined code it reports the
// innermost function, the one whose source the code was compiled from, as
// the first line of a stack trace would.
func traceSymbolize(pc uintptr) (string, string, int, bool) {
	tab := traceTable()
	fn := findFunc(tab, pc)
	if fn == nil {
		return "", "", 0, false
	}
	name := traceString(tab, *(*uint32)(unsafe.Add(fn, 12)))
	file, line, inl := findLine(tab, fn, pc-*(*uintptr)(fn))
	if inl != 0 {
		name = traceString(tab, *(*uint32)(unsafe.Add(inlineSite(tab, fn, inl), 4)))
	}
	if line <= 0 {
		return "", "", 0, false
	}
	return name, file, line, true
}

// printLocation prints one frame. Generated code with no source position,
// such as the wrapper that calls a method through an interface, is left
// out.
func printLocation[W Writer](w W, name, file string, line int) {
	if line <= 0 {
		return
	}
	writeString(w, "    ")
	writeString(w, name)
	writeString(w, " (")
	writeString(w, file)
	writeString(w, ":")
	writeInt(w, int64(line))
	writeString(w, ")\n")
}

// findLine returns the file, line and inlined call (1-based within the
// function, or 0) of the code at offset off in fn.
func findLine(tab, fn unsafe.Pointer, off uintptr) (string, int, int) {
	p := unsafe.Add(tab, int(*(*uint32)(unsafe.Add(tab, 8)))+int(*(*uint32)(unsafe.Add(fn, 24))))
	size := uintptr(*(*uint32)(unsafe.Add(fn, 8)))
	file := ""
	line := 0
	inl := 0
	at := uintptr(0)
	for at < size {
		v := uvarint(&p)
		line += int(unzigzag(v >> 2))
		if v&2 != 0 {
			file = traceString(tab, uint32(uvarint(&p)))
		}
		if v&1 != 0 {
			inl = int(uvarint(&p))
		}
		at += uintptr(uvarint(&p))
		if off < at {
			break
		}
	}
	return file, line, inl
}

func uvarint(p *unsafe.Pointer) uint64 {
	var v uint64
	shift := uint(0)
	for {
		b := *(*uint8)(*p)
		*p = unsafe.Add(*p, 1)
		v |= uint64(b&0x7f) << shift
		if b < 0x80 {
			return v
		}
		shift += 7
	}
}

func unzigzag(v uint64) int64 { return int64(v>>1) ^ -int64(v&1) }

// traceString returns the string at offset off in the table's strings.
func traceString(tab unsafe.Pointer, off uint32) string {
	p := unsafe.Add(tab, int(*(*uint32)(unsafe.Add(tab, 4)))+int(off))
	n := int(*(*uint16)(p))
	if n == 0 {
		return ""
	}
	return unsafe.String((*byte)(unsafe.Add(p, 2)), n)
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && strequal(s[:len(prefix)], prefix)
}

func popcount(x uint8) int {
	n := 0
	for x != 0 {
		n += int(x & 1)
		x >>= 1
	}
	return n
}
