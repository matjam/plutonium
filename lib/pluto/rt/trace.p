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
//
//pu:noescape
func callerFrame(out *[3]uintptr)

// traceTable returns the address of pluto.tracetab (trace_amd64.s).
func traceTable() unsafe.Pointer

// Layout of the table; keep in sync with internal/debuginfo/trace.go.
const (
	traceHeaderSize    = 24
	traceFuncSize      = 32
	traceInlineSize    = 16
	traceDeferSize     = 16
	traceFlagFP        = 1
	traceFlagInterrupt = 2
	maxTraceFrames     = 64
)

// Traceback prints the stack trace of the machine state pc, sp and bp (the
// RBP register), as captured by a fault or interrupt handler at the
// faulting instruction, to the console. A kernel can call it from its
// exception handlers; to stop with a panic instead, call PanicFault.
func Traceback(pc, sp, bp uintptr) {
	traceback(Console{}, pc, sp, bp, true, false, nil)
}

// TraceTask writes to w the stack trace of t, a task that is not running,
// one frame per line, innermost first, in the format of Panic.PrintTrace;
// the frames of package rt that park it are left out. It starts from the
// state Switch saved in t's context: below the saved stack pointer lie
// RFLAGS and the callee-saved registers, then the return address into the
// code that called swapcontext (ctx_amd64.s). It reads the trace table and
// the stack and allocates nothing, so a panic handler or a deadlock report
// can use it for every task. t must be switched out (parked, runnable or
// yielded), and its stack must stay as it is meanwhile. A task that was
// preempted asynchronously has the preemption stub's frame at the top,
// where the walk stops; one that has not run yet has no frames. Like
// Panic.PrintTrace, it is generic in the type of w.
func TraceTask[W Writer](t *Task, w W) {
	sp := t.ctx.sp
	if sp == 0 {
		return
	}
	bp := *(*uintptr)(unsafe.Pointer(sp + 48))
	pc := *(*uintptr)(unsafe.Pointer(sp + 56))
	traceback(w, pc, sp+64, bp, false, true, nil)
}

// traceback prints to w the stack walked from pc, sp and bp. exact says pc
// is the faulting instruction rather than a return address. skipRuntime
// omits the innermost frames of package rt, which belong to the panic
// itself. during, if set, is the panic whose deferred calls were running
// when this one happened: the walk goes on from its start once it reaches
// the runtime's call of the deferred calls (walkDeferred).
func traceback[W Writer](w W, pc, sp, bp uintptr, exact, skipRuntime bool, during *Panic) {
	var fw frameWalk
	fw.start(traceTable(), pc, sp, bp, exact)
	for depth := 0; fw.fn != nil; depth++ {
		if depth == maxTraceFrames {
			writeString(w, "    ...additional frames elided...\n")
			return
		}
		name := fw.name()
		if name == "pluto.main" {
			// main.main may be inlined into it.
			printInlinedMain(w, fw.tab, fw.fn, fw.look())
			return
		}
		if fw.bottom() {
			return
		}
		if during != nil && name == "pluto/rt.walkDeferred" {
			fw.start(fw.tab, during.pc, during.sp, during.bp, during.exact)
			skipRuntime = during.skipRuntime
			during = during.During
			continue
		}
		skipRuntime = printFrame(w, fw.tab, fw.fn, name, fw.look(), skipRuntime)
		fw.next()
	}
}

// A frameWalk walks a stack from the innermost frame out, with the trace
// table: fn is the trace table entry of the current frame's function (nil
// once the walk ends), pc the instruction it is at (exact) or the return
// address into it, and sp and bp the stack pointer and RBP there.
type frameWalk struct {
	tab        unsafe.Pointer
	fn         unsafe.Pointer
	pc, sp, bp uintptr
	exact      bool
}

// start begins a walk at the machine state pc, sp and bp.
func (w *frameWalk) start(tab unsafe.Pointer, pc, sp, bp uintptr, exact bool) {
	w.tab, w.pc, w.sp, w.bp, w.exact = tab, pc, sp, bp, exact
	w.fn = findFunc(tab, w.look())
	if w.fn == nil && exact && sp != 0 {
		// A fault in assembly the table does not describe, such as an
		// atomic operation: such functions are leaves with no frame, so
		// the return address is at the stack pointer.
		w.pc = *(*uintptr)(unsafe.Pointer(sp))
		w.sp = sp + 8
		w.exact = false
		w.fn = findFunc(tab, w.look())
	}
}

// look returns the address that identifies the current frame's code: the
// call, not the instruction after it, for a return address.
func (w *frameWalk) look() uintptr {
	if w.exact {
		return w.pc
	}
	return w.pc - 1
}

// name returns the current frame's function name.
func (w *frameWalk) name() string {
	return traceString(w.tab, *(*uint32)(unsafe.Add(w.fn, 12)))
}

// bottom reports whether the current frame is the first of the main
// thread's stack or of a task's.
func (w *frameWalk) bottom() bool {
	name := w.name()
	return name == "pluto.main" || name == "pluto/rt.start" || name == "pluto/rt.taskMain"
}

// next moves the walk to the caller of the current frame. The walk ends at
// an interrupt frame, which has no caller to return to, and wherever the
// table cannot tell where the caller is.
func (w *frameWalk) next() {
	fn := w.fn
	w.fn = nil
	flags := *(*uint8)(unsafe.Add(fn, 23))
	if flags&traceFlagInterrupt != 0 {
		return
	}
	var cfa, callerBP uintptr
	start := *(*uintptr)(fn)
	mask := *(*uint8)(unsafe.Add(fn, 22))
	prologueEnd := uintptr(*(*uint16)(unsafe.Add(fn, 20)))
	switch {
	case w.exact && w.pc-start < prologueEnd:
		cfa, callerBP = prologueCFA(w.pc-start, mask, flags&traceFlagFP != 0, w.sp, w.bp)
	case flags&traceFlagFP != 0:
		cfa = w.bp + 16
		callerBP = *(*uintptr)(unsafe.Pointer(w.bp))
	default:
		frameSize := uintptr(*(*uint32)(unsafe.Add(fn, 16)))
		cfa = w.sp + frameSize + 8*uintptr(popcount(mask)) + 8
		callerBP = w.bp
		if mask&2 != 0 {
			// RBP is pushed second, after RBX if that is saved.
			at := cfa - 16
			if mask&1 != 0 {
				at -= 8
			}
			callerBP = *(*uintptr)(unsafe.Pointer(at))
		}
	}
	if cfa <= w.sp || cfa&7 != 0 {
		return
	}
	w.pc = *(*uintptr)(unsafe.Pointer(cfa - 8))
	w.sp, w.bp = cfa, callerBP
	w.exact = false
	w.fn = findFunc(w.tab, w.look())
}

// deferRecord returns the address of the current frame's defer record and
// the code address of its runner (internal/lower/deferrec.go), or zeros if
// the function has no defer statements, or if the frame is not set up yet
// (a fault in the prologue).
func (w *frameWalk) deferRecord() (uintptr, uintptr) {
	if w.exact && w.pc-*(*uintptr)(w.fn) < uintptr(*(*uint16)(unsafe.Add(w.fn, 20))) {
		return 0, 0
	}
	idx := uint32((uintptr(w.fn) - uintptr(w.tab) - traceHeaderSize) / traceFuncSize)
	n := int(*(*uint32)(unsafe.Add(w.tab, 16)))
	defers := unsafe.Add(w.tab, int(*(*uint32)(unsafe.Add(w.tab, 20))))
	lo, hi := 0, n
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		e := unsafe.Add(defers, mid*traceDeferSize)
		at := *(*uint32)(e)
		switch {
		case at == idx:
			return w.sp + uintptr(*(*uint32)(unsafe.Add(e, 4))), *(*uintptr)(unsafe.Add(e, 8))
		case at < idx:
			lo = mid + 1
		default:
			hi = mid
		}
	}
	return 0, 0
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
//
// skip says the frames so far belong to the panic or to the parking of a
// task (package rt), which a trace leaves out: a function of package rt is
// not printed, and nor are calls of package rt inlined into the function.
// It reports whether the next frame is still to be skipped.
func printFrame[W Writer](w W, tab, fn unsafe.Pointer, name string, pc uintptr, skip bool) bool {
	if skip && hasPrefix(name, "pluto/rt.") {
		return true
	}
	file, line := printInlined(w, tab, fn, pc, 0, skip)
	if !hasSuffix(name, "·defers") {
		// Not the runner of a defer record (internal/lower/deferrec.go),
		// the runtime's means of calling deferred calls, which a trace
		// leaves out as Go leaves out its panic frame.
		printLocation(w, name, file, line)
	}
	return false
}

// printInlined prints a frame for each inlined call the code at pc in fn is
// in, innermost first, up to and including inline site last (1-based within
// fn; 0 for all of them). It returns the file and line in the code the last
// one printed was inlined into.
//
// With skip set, inlined calls of package rt are not printed, up to the
// first call that is not (see printFrame).
func printInlined[W Writer](w W, tab, fn unsafe.Pointer, pc uintptr, last int, skip bool) (string, int) {
	file, line, inl := findLine(tab, fn, pc-*(*uintptr)(fn))
	for inl != 0 {
		site := inlineSite(tab, fn, inl)
		name := traceString(tab, *(*uint32)(unsafe.Add(site, 4)))
		if skip && hasPrefix(name, "pluto/rt.") {
			// part of the panic or the parking
		} else {
			skip = false
			printLocation(w, name, file, line)
		}
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
			printInlined(w, tab, fn, pc, inl, false)
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
	// The compiler wrote the table with valid UTF-8 only: file names are
	// made valid and cut at a rune boundary (internal/debuginfo/trace.go).
	return unsafe.String((*byte)(unsafe.Add(p, 2)), n)
}

// hasSuffix and hasPrefix compare bytes: the end of a name such as
// "shared[T]·release" is not cut at a rune boundary by a shorter suffix
// that starts with a multi-byte rune ("·defers").
func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && strequal(SliceBytewise(s, len(s)-len(suffix), len(s)), suffix)
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && strequal(SliceBytewise(s, 0, len(prefix)), prefix)
}

func popcount(x uint8) int {
	n := 0
	for x != 0 {
		n += int(x & 1)
		x >>= 1
	}
	return n
}
