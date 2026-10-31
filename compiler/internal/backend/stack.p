package backend

import (
	"compiler/compat"
	"compiler/internal/ssa"
	"compiler/internal/syntax/token"
	"compiler/pu"
	"pluto/sort"
	"pluto/strings"
)

// Task stack bounds (language spec section 19.1, item 1; roadmap item 18).
// Lowering gives each task statement a read-only word that holds the stack
// its task is proven to need, and records the task starts and the callees
// of indirect calls (ssa.StackInfo, lower/taskstack.go). Once every
// function is compiled, Emit computes each bound over the final machine
// code and writes it into the word, or leaves 0 there, which asks the
// runtime for a stack of unknown need.
//
// A task's stack holds, from the top down: the argument block, which the
// runtime adds to the bound (spawnFor), up to 15 bytes of alignment below
// it (newContext), then the frames of taskMain and of what it calls. On
// top of the deepest of those frames, three more things may land:
//
//   - a fault: the fault handler, which runs on the signal stack, resumes
//     the task 128 bytes and an alignment below where it faulted, in
//     faultResume, which calls raiseFault (rt_linux.go). raiseFault runs
//     the task's deferred calls; a fault while they run is raised on the
//     signal stack, so this happens once.
//   - a preemption: the preemption signal's handler makes the task call
//     asyncPreempt, which saves every register below the red zone and
//     calls preemptPark (preempt_amd64.s). A task is preempted at most once
//     at a time (Task.preempt), but it may be preempted on the fault path.
//   - signal handlers of the runtime, which run on the signal stack
//     (SA_ONSTACK): nothing. A program that installs a handler of its own
//     with pluto/syscall must ask for the signal stack too.
//
// So the bound is the deepest call chain from taskMain, plus the fault
// path, plus the preemption path, plus slack: the alignment below the
// argument block and an allowance for what the analysis rounds. A frame
// is what the function's final code takes below its caller's call (frame,
// saved registers, alignment of a frame with a frame pointer, outgoing
// arguments, its return address, and a word for saving the flags).
//
// The panic path is part of the call graph: a panic calls raise, which
// walks the task's frames and runs the deferred calls of each through
// callDeferRunner. Its callees are the defer runners of the functions the
// task may have on its stack; they run with the task's panic set, and a
// panic in them goes straight to the panic handler, so on that path
// callDeferRunner calls nothing. ExitTask leaves frames by returning.
//
// A call through a func value or an interface reaches what lowering
// resolved for it: the call in the function, or in each function inlined
// into it. A call that may reach any function, a call of a function
// without a body whose stack use is not known, and recursion leave the
// bound 0. Assembly functions are known when they are leaves (no push,
// call or change of RSP: they use their return address), or listed in
// asmStack.

// MinStackSize is pluto/rt.MinStackSize: no task gets a smaller stack
// (language spec section 19.1, item 1).
const MinStackSize = 8 << 10

// stackSlack is added to every bound: the alignment of the first frame
// below the argument block (16) and an allowance (240).
const stackSlack = 256

// vdsoAllowance is the stack the kernel's vDSO functions may use (vdsoCall2
// calls them on the task's stack). The kernel does not document it; its
// clock functions use well under this.
const vdsoAllowance = 4096

// A StackResult is the stack bound of one task statement.
type StackResult struct {
	Pos   token.Pos
	Thunk string
	Bound int64  // 0 if not computed
	Why   string // why not
}

// An AsmFrame is the stack use of an assembly function: as amd64.Code's
// Frame, and the functions it calls.
type AsmFrame struct {
	Frame int64
	Calls []string
}

// asmStack lists the assembly functions of the runtime that are not
// leaves but that tasks call.
var asmStack = map[string]AsmFrame{
	// Pushes six registers and the flags, then switches stacks.
	"pluto/rt.swapcontext": {Frame: 64},
	// The parent returns at once; the child runs on its own stack.
	"pluto/rt.cloneThread": {Frame: 8},
	// Pushes RBP, aligns RSP and calls into the vDSO.
	"pluto/rt.vdsoCall2": {Frame: 8 + 8 + 8 + 8 + vdsoAllowance},
}

// The runtime functions the analysis treats on their own.
const (
	symDeferRunner = "pluto/rt.callDeferRunner"
	symTaskMain    = "pluto/rt.taskMain"
	symPreemptPark = "pluto/rt.preemptPark"
	symRaiseFault  = "pluto/rt.raiseFault"
)

// preemptFrame is what asyncPreempt puts on the stack below the interrupted
// RSP before it calls preemptPark: the red zone and the resume slot (136),
// the flags and 15 registers (128), alignment (15), the FPU state (512).
const preemptFrame = 136 + 128 + 15 + 512

// faultFrame is the gap faultResume leaves below the faulting RSP: the red
// zone (128) and the alignment (15).
const faultFrame = 128 + 15

// A funcStack is what the analysis keeps of a compiled function.
type funcStack struct {
	frame    int64
	calls    []string
	indirect bool
	inlines  []string
	runner   string
}

// recordStack keeps what the stack analysis needs of f, compiled as code.
func (e *emitter) recordStack(f *ssa.Func, frame int64, calls []*ssa.Sym, indirect bool) {
	fs := pu.New(funcStack{frame: frame, indirect: indirect})
	for _, c := range calls {
		fs.calls = compat.Append(fs.calls, e.name(c.Name))
	}
	for _, in := range f.Inlines {
		fs.inlines = compat.Append(fs.inlines, e.name(in.Func))
	}
	if f.DeferRunner != nil {
		fs.runner = e.name(f.DeferRunner.Name)
	}
	name := e.name(f.Sym.Name)
	e.stacks[name] = fs
	for _, x := range f.Exports {
		e.aliases[e.name(x)] = name
	}
}

// A stackKey is a function on one of the paths: mode 1 is the deferred
// calls of a panic, where callDeferRunner calls nothing.
type stackKey struct {
	name string
	mode int
}

// A stackDepth is the deepest stack below a function's caller's call that
// calling it may use, or why it has no bound.
type stackDepth struct {
	depth int64
	why   string
}

// A stackWalk computes the depths of one task start.
type stackWalk struct {
	e       *emitter
	st      *ssa.StackInfo
	asm     map[string]AsmFrame
	runners []string // callDeferRunner's callees in mode 0
	memo    map[stackKey]stackDepth
	active  map[stackKey]bool
	path    []string // the functions being walked, outermost first
}

// stackBounds computes the bound of every task start of p and writes it
// into its word.
func (e *emitter) stackBounds(p *ssa.Program) []StackResult {
	st := p.Stack
	asm := map[string]AsmFrame{}
	for name, af := range e.cfg.AsmStack {
		asm[name] = af
	}
	for name, af := range asmStack {
		asm[name] = af
	}
	words := map[*ssa.Sym]*ssa.Data{}
	for _, d := range p.Data {
		words[d.Sym] = d
	}
	// The depth of a function in mode 1 is the same for every task.
	shared := map[stackKey]stackDepth{}
	var out []StackResult
	for _, r := range st.Roots {
		w := pu.New(stackWalk{e: e, st: st, asm: asm, memo: map[stackKey]stackDepth{}, active: map[stackKey]bool{}})
		for k, d := range shared {
			w.memo[k] = d
		}
		w.runners = w.reachableRunners(r.Thunk)
		res := StackResult{Pos: r.Pos, Thunk: r.Thunk}
		root := w.rootDepth(r.Thunk)
		pre := w.depth(stackKey{symPreemptPark, 0})
		fault := w.depth(stackKey{symRaiseFault, 0})
		switch {
		case root.why != "":
			res.Why = root.why
		case pre.why != "":
			res.Why = pre.why
		case fault.why != "":
			res.Why = fault.why
		default:
			b := root.depth + preemptFrame + pre.depth + faultFrame + fault.depth + stackSlack
			b = max((b+15)&^15, MinStackSize)
			res.Bound = b
			if d := words[r.Bound]; d != nil && len(d.Bytes) >= 8 {
				for i := range 8 {
					d.Bytes[i] = byte(uint64(b) >> (8 * i))
				}
			}
		}
		for k, d := range w.memo {
			if k.mode == 1 {
				shared[k] = d
			}
		}
		out = compat.Append(out, res)
	}
	sort.SortStableFunc(out, func(a, b StackResult) bool { return a.Pos < b.Pos })
	return out
}

// resolve returns the function a symbol name stands for: itself, or the
// function an exported name is another name of.
func (w *stackWalk) resolve(name string) string {
	if n, ok := w.e.aliases[name]; ok {
		return n
	}
	return name
}

// indirectTargets returns the callees of fs's indirect calls, or why they
// are not known. fs is the function name.
func (w *stackWalk) indirectTargets(name string, fs *funcStack) ([]string, string) {
	if !fs.indirect {
		return nil, ""
	}
	var out []string
	for _, n := range compat.Append([]string{name}, fs.inlines...) {
		if w.st.Open[n] {
			return nil, compat.Sprintfb("%s calls a func value or interface method that may be any function", compat.ArgS(n))
		}
		out = compat.Append(out, w.st.Indirect[n]...)
	}
	return out, ""
}

// callees returns the functions name calls in mode, with the mode each
// runs in, or why they are not known; frame is name's own stack use.
func (w *stackWalk) callees(k stackKey) (frame int64, calls []stackKey, why string) {
	name := w.resolve(k.name)
	if name == symDeferRunner {
		// It jumps to the runner, which returns to its caller: no frame
		// of its own.
		if k.mode == 0 {
			for _, r := range w.runners {
				calls = compat.Append(calls, stackKey{r, 1})
			}
		}
		return 0, calls, ""
	}
	if fs := w.e.stacks[name]; fs != nil {
		for _, c := range fs.calls {
			calls = compat.Append(calls, stackKey{c, k.mode})
		}
		ts, why := w.indirectTargets(name, fs)
		if why != "" {
			return 0, nil, why
		}
		for _, t := range ts {
			calls = compat.Append(calls, stackKey{t, k.mode})
		}
		return fs.frame, calls, ""
	}
	if af, ok := w.asm[name]; ok {
		for _, c := range af.Calls {
			calls = compat.Append(calls, stackKey{c, k.mode})
		}
		return af.Frame, calls, ""
	}
	return 0, nil, compat.Sprintfb("%s has no body whose stack use is known", compat.ArgS(name))
}

// depth returns the depth of k.
func (w *stackWalk) depth(k stackKey) stackDepth {
	if d, ok := w.memo[k]; ok {
		return d
	}
	if w.active[k] {
		return stackDepth{why: w.cycle(k.name)}
	}
	w.active[k] = true
	w.path = compat.Append(w.path, k.name)
	frame, calls, why := w.callees(k)
	d := stackDepth{why: why}
	if why == "" {
		d = w.deepest(frame, calls)
	}
	w.path = w.path[:len(w.path)-1]
	w.active[k] = false
	w.memo[k] = d
	return d
}

// cycle returns the reason for a call of name, which is being walked: the
// calls from name back to itself.
func (w *stackWalk) cycle(name string) string {
	i := len(w.path) - 1
	for i > 0 && w.path[i] != name {
		i--
	}
	chain := compat.Append(compat.Clone(w.path[i:]), name)
	return compat.Sprintfb("%s is recursive: %s", compat.ArgS(name), compat.ArgS(strings.Join(chain, " calls ")))
}

// deepest returns frame plus the deepest of calls.
func (w *stackWalk) deepest(frame int64, calls []stackKey) stackDepth {
	var most int64
	for _, c := range calls {
		cd := w.depth(c)
		if cd.why != "" {
			return cd
		}
		most = max(most, cd.depth)
	}
	return stackDepth{depth: frame + most}
}

// rootDepth returns the depth of taskMain running thunk: its call through
// the task's func value reaches thunk only.
func (w *stackWalk) rootDepth(thunk string) stackDepth {
	fs := w.e.stacks[symTaskMain]
	if fs == nil {
		return stackDepth{why: "the runtime's taskMain was not compiled"}
	}
	calls := compat.Clone([]stackKey{{thunk, 0}})
	for _, c := range fs.calls {
		calls = compat.Append(calls, stackKey{c, 0})
	}
	for _, n := range fs.inlines {
		if w.st.Open[n] {
			return stackDepth{why: compat.Sprintfb("%s calls a func value or interface method that may be any function", compat.ArgS(n))}
		}
		for _, t := range w.st.Indirect[n] {
			calls = compat.Append(calls, stackKey{t, 0})
		}
	}
	return w.deepest(fs.frame, calls)
}

// reachableRunners returns the defer runners of the functions a task that
// runs thunk may have on its stack outside a panic's deferred calls: those
// reachable from taskMain, from the preemption path and from the fault
// path without going through callDeferRunner.
func (w *stackWalk) reachableRunners(thunk string) []string {
	seen := map[string]bool{}
	queue := compat.Clone([]string{thunk, symTaskMain, symPreemptPark, symRaiseFault})
	for _, n := range queue {
		seen[n] = true
	}
	var runners []string
	for len(queue) > 0 {
		name := w.resolve(queue[0])
		queue = queue[1:]
		if name == symDeferRunner {
			continue
		}
		var next []string
		if fs := w.e.stacks[name]; fs != nil {
			if fs.runner != "" && !seen["\x00"+fs.runner] {
				seen["\x00"+fs.runner] = true
				runners = compat.Append(runners, fs.runner)
			}
			next = compat.Append(next, fs.calls...)
			if name != symTaskMain {
				ts, _ := w.indirectTargets(name, fs)
				next = compat.Append(next, ts...)
			} else {
				for _, n := range fs.inlines {
					next = compat.Append(next, w.st.Indirect[n]...)
				}
			}
		} else if af, ok := w.asm[name]; ok {
			next = af.Calls
		}
		for _, n := range next {
			if !seen[n] {
				seen[n] = true
				queue = compat.Append(queue, n)
			}
		}
	}
	sort.Strings(runners)
	return runners
}

// AsmStack adds to frames the stack use of the leaf functions of the
// assembly source src, its labels declared global or of type @function:
// those that push nothing, call nothing, do not
// change RSP and jump only to local labels, which use their return
// address only. Other functions are left out.
func AsmStack(src string, frames map[string]AsmFrame) {
	var name string
	leaf := false
	finish := func() {
		if name != "" && leaf {
			frames[name] = AsmFrame{Frame: 8}
		}
		name = ""
	}
	functions := map[string]bool{}
	for line := range strings.SplitSeq(src, "\n") {
		line = strings.TrimSpace(line)
		if f, ok := strings.CutPrefix(line, ".type"); ok {
			if sym, kind, ok := strings.Cut(f, ","); ok && strings.TrimSpace(kind) == "@function" {
				functions[strings.Trim(strings.TrimSpace(sym), "\"")] = true
			}
		}
		for _, dir := range []string{".globl", ".global"} {
			if f, ok := strings.CutPrefix(line, dir); ok && (strings.HasPrefix(f, " ") || strings.HasPrefix(f, "\t")) {
				for sym := range strings.SplitSeq(f, ",") {
					functions[strings.Trim(strings.TrimSpace(sym), "\"")] = true
				}
			}
		}
	}
	for raw := range strings.SplitSeq(src, "\n") {
		line := raw
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = strings.Slice(line, 0, i)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if label, ok := strings.CutSuffix(line, ":"); ok {
			label = strings.Trim(label, "\"")
			if functions[label] {
				finish()
				name, leaf = label, true
			}
			continue
		}
		if name == "" || strings.HasPrefix(line, ".") {
			if strings.HasPrefix(line, ".section") || strings.HasPrefix(line, ".text") {
				finish()
			}
			continue
		}
		op, args := splitInsn(line)
		for op == "lock" || strings.HasPrefix(op, "rep") {
			// a prefix: the instruction follows it
			op, args = splitInsn(args)
		}
		switch {
		case strings.HasPrefix(op, "push"), strings.HasPrefix(op, "pop"), strings.HasPrefix(op, "call"),
			op == "enter", op == "leave", strings.HasPrefix(op, "int"), strings.HasPrefix(op, "iret"),
			op == "sysenter", op == "sysret", op == "lret", op == "ljmp":
			leaf = false
		case op == "ret" && args != "":
			leaf = false
		case strings.HasPrefix(op, "j") && !localLabel(args):
			leaf = false
		}
		// AT&T syntax: the destination is the last operand.
		if strings.HasSuffix(args, "%rsp") || strings.HasSuffix(args, "%esp") || strings.HasSuffix(args, "%sp") {
			leaf = false
		}
	}
	finish()
}

// localLabel reports whether a jump's operand is a numeric local label
// (1f, 2b) or a .L label.
func localLabel(s string) bool {
	if strings.HasPrefix(s, ".L") {
		return true
	}
	if len(s) < 2 || s[len(s)-1] != 'f' && s[len(s)-1] != 'b' {
		return false
	}
	for i := 0; i < len(s)-1; i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// splitInsn splits an instruction into its mnemonic and its operands.
func splitInsn(line string) (string, string) {
	line = strings.TrimSpace(line)
	for i := 0; i < len(line); i++ {
		if line[i] == ' ' || line[i] == '\t' {
			return strings.Slice(line, 0, i), strings.TrimSpace(strings.SliceFrom(line, i+1))
		}
	}
	return line, ""
}
