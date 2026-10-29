# Kernel mode and freestanding builds

For PlutoniumOS and any code that runs without an operating system. The language is the same; these are the
**rules that differ from hosted code**. Spec chapter 20; ABI details in `docs/spec/abi.md`; a working kernel to
copy from: `testdata/kernel/` (smoke, irq, panic, sched, gsuser).

## 1. Build modes (sections 14.4, 20.5, 20.6)

| Mode | Command | Tags | What changes |
|---|---|---|---|
| hosted | `pluto build ./dir` | `amd64 linux hosted` | `pluto/os`, `pluto/syscall` exist; allocator, scheduler and panic handler are built in |
| freestanding | `pluto build --mode freestanding --gencheck=false --target x86_64-none -o k.elf ./dir` | `amd64 none freestanding` | no OS packages; **you** provide the entry point, the panic handler and, for tasks/channels/waiting, a scheduler; no default allocator |
| kernel | `pluto build --mode kernel --gencheck=false ...` | `amd64 none freestanding kernel` | freestanding plus ring-0 code generation: no red zone, **no floating point**, `//pu:percpu`, linked in the top 2 GiB with a default script |

- Freestanding and kernel builds **must pass `--gencheck=false`** for now: generation checks do not support them
  yet (roadmap item 101), and leaving the default is an error rather than a silent unchecked build (section
  18.11). The build is unchecked: a use of freed heap memory is not detected.
- `--ldscript file` gives your own linker script, `--format binary` writes a flat image, `-c` writes only the
  object file (the checker uses it), `--fieldnames` records struct field names for `%+v` (off in kernel mode).
- Files select themselves by tag: `//pu:build kernel` before the package clause, or a name suffix
  (`boot_none.p`, `sched_amd64.s`, `*_linux.p`). Assembly (`.s`, GNU syntax, `#pu:build`) and C (`.c`, `.o`) files
  in a package are linked in. There is no C library.
- Freestanding programs have **no default allocator** until `alloc.SetDefault(a)` runs in the entry function:
  allocating earlier (`new`, `make`, strings, `+`, `fmt.Sprintf`) panics. A package-level initializer that
  allocates is a compile error; initialize in the entry function instead. Pass an allocator explicitly where
  you can (`vec.New[T](&arena)`, `new(T, &arena)`).
- The entry point (assembly or `//pu:export`) must call package initialization and `main` on a 16-byte aligned
  stack with the direction flag clear (abi.md section 8).
- A freestanding program that starts a task, performs a channel operation or `select` that may wait, or waits in
  `pluto/sync` must bind a scheduler with `//pu:scheduler` on a package variable whose type implements
  `rt.Scheduler` (`NewTask`, `FreeTask`, `Spawn`, `Park`, `Wake`, `Yield`; optional `Init`, `InInterrupt`,
  `Now`, `ArmTimer`). Without one the build fails with E2593. The dependency can be indirect, through a library
  function that waits; the arenas of `pluto/arena` do not wait, so a kernel that allocates from an arena binds a
  scheduler only if it starts tasks (see `testdata/kernel/sched`).
- Kernel `main` normally never returns. Package cleanup at exit is a hosted notion.

```pluto-compile mode=kernel
package main

import (
	"pluto/arch/x86"
	"pluto/rt"
)

// Exactly one panic handler is required (E2580); it never returns.
//
//pu:panic
func onPanic(p *rt.Panic) {
	for {
		x86.Cli()
		x86.Hlt()
	}
}

func main() {}
```

## 2. Directives (section 20.3)

A directive is a `//pu:` line in the doc comment of a declaration (`//pu:build` before the package clause). An
unknown, retired or misplaced directive is a compile error (E2600 to E2604); `//go:` comments are errors too.
Ordinary hosted code needs only the first group.

| Directive | On | Meaning |
|---|---|---|
| `//pu:build expr` | file | include only when the tag expression holds (`kernel && !hosted`) |
| `//pu:inline`, `//pu:noinline` | function | request or forbid inlining |
| `//pu:stack 8M` | `func main`, hosted | main runs on a stack of that size |
| `//pu:extern name`, `//pu:export [name]` | function | C linkage (System V convention); signatures limited to integers, floats, bool, pointers, boxes, packed, structs of those (E2511) |
| `//pu:section name` | function or variable | link into that section |
| `//pu:interrupt` | function | interrupt or exception handler, entered through an interrupt gate (`x86.InterruptGate`): its body is an interrupt-disabled region until `x86.Sti` (below) |
| `//pu:interrupt trap` | function | the same handler entered through a trap gate (`x86.TrapGate`), so it can be re-entered: its body is not an interrupt-disabled region. `trap` is the only argument (E2603 otherwise) |
| `//pu:panic` | function | the panic handler `func(p *rt.Panic)` |
| `//pu:scheduler` | package variable | the scheduler backend |
| `//pu:percpu` | package variable | one copy per CPU, via `GS` (kernel mode only: E2551 elsewhere) |
| `//pu:fpu` | function | may use floats in kernel mode; caller saved FPU state |
| `//pu:blocking` | function | may wait in a way the compiler cannot see; handlers must not reach it |
| `//pu:nosimd` | function | no vector instructions (implied in kernel mode and for handlers of either form) |
| `//pu:noescape`, `//pu:noreturn` | body-less function | promises about assembly/C (retired on functions with a body) |
| `//pu:nochecks` | function | removes bounds and generation checks: unsafe code, measured use only |
| `//pu:resultholds p` | standard library only | (E3008 elsewhere) |

The compiler **infers** which functions never return, which wait, which keep their pointer arguments: do not
write directives for them.

## 3. Hardware types (sections 7.14 to 7.17)

- `volatile[T]`: every access is exactly one load or store, never merged or reordered. For device registers.
  `&u.data` has type `*volatile[uint8]`. It is not atomic and gives no cross-CPU ordering (use `sync/atomic`).
- `packed [le|be] [backing] { name bits[T, N]; name bits { A B C }; name T; _ bits[uint, 4] }`: exact bit layout,
  first field in bit 0. Converts to and from an integer or byte array of its size keeping reserved bits. A `be`
  array-backed type may not have a bit field crossing a byte boundary (E2021): nest an integer-backed packed type
  (see `Frag` below). Comparison ignores reserved bits; two tasks writing different fields of one packed value race.
- `aligned[T, N]`: storage on an `N`-byte boundary (`aligned[[512]uint64, 4096]` is a page table).
- `ring[T, N]`: fixed-capacity inline ring buffer (N a power of two): `Push`, `Pop`, `Peek`, `Len`, `Full`,
  `Reset`, `range r`. Not safe for concurrent use.
- `enum` and `union` (also `packed union` for tagged hardware words) work as in hosted code.
- `unsafe.Pointer` to `uintptr` conversions are unrestricted; a pointer made from an integer is assumed to point
  outside every frame and is never checked. `uintptr` sizes are 64 bits.

```pluto-compile mode=kernel
package main

import (
	"pluto/arch/x86"
	"pluto/rt"
	"unsafe"
)

//pu:panic
func onPanic(p *rt.Panic) {
	for {
		x86.Hlt()
	}
}

type UART struct {
	data   volatile[uint8]
	status volatile[uint8]
}

func put(u *UART, b uint8) {
	for u.status&0x20 == 0 {
	}
	u.data = b
}

type Frag packed be uint16 { // an integer-backed field inside a big-endian header
	offset bits[uint16, 13]
	flags  bits[uint8, 3]
}

type IPv4 packed be [20]byte {
	ihl     bits[uint8, 4]
	version bits[uint8, 4]
	tos     uint8
	length  uint16
	id      uint16
	frag    Frag
	_       [12]byte
}

var uart = (*UART)(unsafe.Pointer(uintptr(0x3F8))) // memory nobody owns: never freed

var q ring[uint8, 16]

func main() {
	put(uart, 'x')
	var h IPv4
	h.version = 4
	h.ihl = 5
	h.length = 40
	h.frag.flags = 2
	q.Push(h.version)
	if v, ok := q.Pop(); ok {
		put(uart, v)
	}
}
```

## 4. `pluto/arch/x86` (section 20.8)

Each function expands inline to its instruction and is a compiler barrier: `Cli`, `Sti`, `StiHlt`, `Hlt`,
`Pause`, `SaveFlagsCli`, `RestoreFlags`; port I/O `Inb/Inw/Inl`, `Outb/Outw/Outl`; `Rdmsr`, `Wrmsr`;
`ReadCR0/2/3/4`, `WriteCR0/3/4`, `Invlpg`; `Lgdt`, `Lidt`, `Ltr`; `Cpuid`, `Rdtsc`, `Rdtscp`; `Swapgs`,
`ReadGSBase`, `WriteGSBase`, `GSLoad64/32`, `GSStore64/32`; `FPUBegin(area)`, `FPUEnd(area)`;
`InterruptGate(h, selector, ist)` and `TrapGate(h, selector, ist)` (the 16-byte `Gate` that enters a handler; `h` must name a
`//pu:interrupt` function, `TrapGate` a `//pu:interrupt trap` one, E2572);
`NewDescriptorTablePointer`; types `Gate`, `DescriptorTablePointer`, `FPUState` (`aligned[[512]byte, 16]`),
`InterruptFrame` (`RIP, CS, RFLAGS, RSP, SS`). The package is unsafe code except `Cli`, `Sti`, `SaveFlagsCli`,
`RestoreFlags`. See `stdlib-system.md` for the exact list.

## 5. Interrupt handlers (sections 19.3, 20.10)

A `//pu:interrupt` function is entered by the processor: a plain function with a body, no results, one parameter
(the frame: any pointer type, `unsafe.Pointer` or `uintptr`) or two (the frame and a `uint64`/`uintptr` error code,
for the vectors that push one; the signature decides the stack layout). The compiler saves registers and returns
with `iretq`; changing the frame (`f.RIP += 2`) takes effect on return. It is never inlined, cannot be called,
deferred or used as a value (E2573); install it in the interrupt table with a typed gate, `x86.InterruptGate(timer, 0x08, 0)`,
which returns the `x86.Gate` (a table is an array of `Gate`; `x86.Lidt` loads it).

- **The gate is what makes the handler's body safe.** An interrupt gate clears the interrupt flag on entry, so the body
  of a plain `//pu:interrupt` handler is a region with interrupts disabled, until a call of `x86.Sti` (or
  `StiHlt`) or of a function that may switch tasks. Per-CPU data the handler shares with code it interrupts needs no
  other protection inside that stretch. A raw word written into the table (an address stored as an integer through
  `unsafe.Pointer`) is unsafe code: the typed gate is how the compiler knows every installed handler is entered with
  interrupts disabled.
- **A handler that can be entered again** says so: `//pu:interrupt trap` (the only argument the directive takes; any
  other is E2603). It is installed only with `x86.TrapGate`, which leaves the interrupt flag as it was, and its body is
  not an interrupt-disabled region: per-CPU data it shares is protected by a region the handler opens itself
  (`x86.SaveFlagsCli` ... `x86.RestoreFlags`). The wrong constructor for the form is E2572, and so is a gate
  constructor used as a func value or given anything but the name of a handler.
- Exceptions and nonmaskable interrupts are not held off by an interrupt gate; a handler for one must not use data the
  interrupted code may be changing.

- **Nothing a handler (or the panic handler, or the scheduler's `Wake`) can reach may wait.** No channel send or
  receive except as a `select` with `default`, no `range` over a channel, no `sync.Mutex.Lock`, `WaitGroup.Wait`,
  `Scope.Wait` or `time.Sleep`, no task start (E2585), no allocator whose `Alloc`/`Free` waits. The check follows
  every call across all packages, including drops the compiler inserts, and through interface and func-value calls
  (conservatively): E2584 names the path. Allowed: `select` with `default`, `close`, `len`, `cap`, `TryLock`,
  `rt.Ready`, atomics, runtime timers, and interrupt-safe spin locks (`rt.Lock`).
- **A handler is a party in the data-race rules.** Anything it touches (except per-CPU variables) must be atomic or
  guarded by an `rt.Lock` (a `sync.Mutex` cannot be taken in a handler: E2584). `volatile` alone and disabling
  interrupts exclude only the same processor. A race is E3502 or E3503 naming the handler.
- To hand work to normal code, send without waiting: `select { case work <- item: default: /* full */ }`.
- Format text in a handler with `fmt.Appendf` or `strconv.Append...` into a `vec.Fixed[byte]` over an array; those
  never allocate.

```pluto-compile mode=kernel
package main

import (
	"pluto/arch/x86"
	"pluto/rt"
	"pluto/sync/atomic"
	"unsafe"
)

//pu:panic
func onPanic(p *rt.Panic) {
	for {
		x86.Hlt()
	}
}

var ticks atomic.Uint64
var lock rt.Lock // may be taken in a handler
var queued uint64

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	ticks.Add(1)
	lock.Lock()
	queued++
	lock.Unlock()
	x86.Outb(0x20, 0x20) // end of interrupt
}

//pu:interrupt
func pageFault(f *x86.InterruptFrame, code uint64) { // a vector that pushes an error code
	rt.PanicFault(rt.MemoryFault, uintptr(x86.ReadCR2()), uintptr(f.RIP), uintptr(f.RSP), 0)
}

var idt [256]x86.Gate

func main() {
	idt[32] = x86.InterruptGate(timer, 0x08, 0)
	idt[14] = x86.InterruptGate(pageFault, 0x08, 0)
	idtr := x86.NewDescriptorTablePointer(uintptr(unsafe.Pointer(&idt)), len(idt)*16)
	x86.Lidt(&idtr)
	lock.Lock()
	queued = 0
	lock.Unlock()
}
```

## 6. Per-CPU variables (section 20.9)

`//pu:percpu var v T` gives each processor its own copy, addressed through `GS` (the kernel points `GS` at the CPU's
block, `rt.SetCPU(n)` tells the runtime which CPU it is). Constant initializers only (E2550), not with
`//pu:section` (E2552), kernel mode only (E2551). A **lone load or store** of a whole variable of at most a word is
allowed anywhere, provided the store does not depend on an earlier load of the same variable (below); through a pointer from `&v` the rule is stricter: when a handler also uses the variable, every
load or store through the pointer needs interrupts disabled, except a store that is a whole statement and writes
the whole word-sized variable, D34). Every other access (`v++`, `v += x`, read and write in one expression, larger values,
pointer-receiver calls, `&v`, and a **store whose value or controlling condition depends on an earlier load of the same
variable**) must sit in a **non-migrating region**: after `rt.DisablePreemption()` (usually
`defer rt.EnablePreemption()`), with interrupts masked (`x86.Cli`/`Sti`, `SaveFlagsCli`/`RestoreFlags`), or with an
`rt.Lock` held; and in an interrupt-disabled region if a handler also touches it. Otherwise E2022. A pointer from
`&v` may not be used after its region (E2023).

The dependent store is a read-modify-write written as two statements: `x := ticks; ticks = x + 1`, or
`if current == idle { current = next }`. The dependence is followed through locals, copies of structs, arithmetic,
conditions, loops and calls (a result depends on its arguments and on what the callee loads; a callee that stores the
variable from its argument, or in a branch the caller's condition controls, counts as a store at the call); where it
cannot see a call (a func value, a body-less function) it assumes the result depends on all the arguments, and the
error says so. The load and the store must be in **one** region, not in two regions one after the other. Two lone
accesses that do not depend on each other (`log(current)` here, `current = t` there, `t` not computed from `current`)
stay allowed anywhere.

```pluto-compile mode=kernel
package main

import (
	"pluto/arch/x86"
	"pluto/rt"
)

//pu:panic
func onPanic(p *rt.Panic) {
	for {
		x86.Hlt()
	}
}

type Thread struct{ id int }

var idle Thread

//pu:percpu
var current *Thread // a lone load or store of one word needs no region

//pu:percpu
var ticks uint64

func tick() {
	rt.DisablePreemption()
	defer rt.EnablePreemption()
	ticks++ // a compound access: inside a non-migrating region
}

func main() {
	tick()
	current = &idle
}
```

```pluto-error E2022 mode=kernel
package main

import (
	"pluto/arch/x86"
	"pluto/rt"
)

//pu:panic
func onPanic(p *rt.Panic) {
	for {
		x86.Hlt()
	}
}

//pu:percpu
var ticks uint64

func main() {
	ticks++ // the task could migrate between the load and the store
}
```

```pluto-error E2022 mode=kernel
package main

import (
	"pluto/arch/x86"
	"pluto/rt"
)

//pu:panic
func onPanic(p *rt.Panic) {
	for {
		x86.Hlt()
	}
}

type Thread struct{ id int }

var idle, next Thread

//pu:percpu
var current *Thread

func main() {
	if current == &idle {
		current = &next // controlled by the load of current: needs one region around both
	}
	rt.DisablePreemption()
	if current == &idle {
		current = &next // fine: the load and the store are in one region
	}
	rt.EnablePreemption()
}
```

## 7. Floating point (section 20.7)

In kernel mode any floating-point operation (arithmetic, comparison, conversion, a float constant, passing or
returning a float, converting a float to an interface) in a function the program uses is E2520 unless the function
is `//pu:fpu`. Copying float-typed values is fine. Whoever calls a `//pu:fpu` function must have saved the
interrupted vector state: bracket it with `x86.FPUBegin(&area)` and `x86.FPUEnd(&area)`. The library keeps its
float code behind `//pu:build !kernel`; a float printed by `fmt` shows as its bits in a kernel build, and
`print` of a float shows the bit pattern.

```pluto-error E2520 mode=kernel
package main

import (
	"pluto/arch/x86"
	"pluto/rt"
)

//pu:panic
func onPanic(p *rt.Panic) {
	for {
		x86.Hlt()
	}
}

func half(x float64) float64 { return x / 2 } // needs //pu:fpu

func main() { _ = half(3) }
```

```pluto-compile mode=kernel
package main

import (
	"pluto/arch/x86"
	"pluto/rt"
)

//pu:panic
func onPanic(p *rt.Panic) {
	for {
		x86.Hlt()
	}
}

type vec struct{ X, Y float64 }

var area x86.FPUState
var v = vec{1.5, 2}

//pu:fpu
//pu:noinline
func scale(p *vec, k int) {
	p.X = p.X*float64(k) + p.Y
}

func main() {
	x86.FPUBegin(&area)
	scale(&v, 3)
	x86.FPUEnd(&area)
}
```

## 8. Panics and the console (sections 17.4, 13.12)

- `//pu:panic func(p *rt.Panic)`: exactly one in a freestanding or kernel build (E2580; two is E2581). It never
  returns on any path. `p.Print(rt.Console{})` writes `panic: ...`, the fault if any and the stack trace.
  Freestanding builds run **no deferred calls** on a panic. `rt.PanicFault(kind, addr, pc, sp, bp)` reports a
  fault from an exception handler. `rt.Abort()` stops after a custom report.
- `print` and `println` (any integer, bool, string, pointer, float) write to the function registered with
  `rt.SetConsole(func(p unsafe.Pointer, n int))`; each statement is atomic and masks interrupts in kernel mode so
  a handler may print. `rt.Console{}` is an `io.Writer` that writes to the same place:
  `fmt.Fprintf(rt.Console{}, "x=%d\n", x)`. `fmt.Println` and `fmt.Print` (which need `os`) do not exist.
- Inside a task, a stack overflow is a panic with a trace; `rt.StackLeft()` reports the bytes left.

```pluto-compile mode=kernel
package main

import (
	"pluto/arch/x86"
	"pluto/fmt"
	"pluto/rt"
	"unsafe"
)

const com1 = 0x3F8

func serialWrite(p unsafe.Pointer, n int) {
	for i := 0; i < n; i++ {
		for x86.Inb(com1+5)&0x20 == 0 {
			x86.Pause()
		}
		x86.Outb(com1, *(*uint8)(unsafe.Add(p, i)))
	}
}

//pu:panic
func onPanic(p *rt.Panic) {
	p.Print(rt.Console{})
	for {
		x86.Cli()
		x86.Hlt()
	}
}

func main() {
	rt.SetConsole(serialWrite)
	println("booted")
	fmt.Fprintf(rt.Console{}, "cpu %d ok\n", 0)
}
```

## 9. Checklist for kernel code

1. `pluto build --mode kernel --gencheck=false -c` the package often; E2520, E2584, E2593 and E2022 are the kernel-specific errors.
2. Floats only in `//pu:fpu` functions; no `fmt.Sprintf` of floats in kernel paths.
3. Handlers: atomics and `rt.Lock` only; send with `select`/`default`; no allocation unless the allocator never waits.
4. Per-CPU state: lone word accesses or an explicit non-migrating region.
5. Raw memory is `unsafe`: keep it in a few small packages and audit them with `pluto vet -unsafe --mode kernel`.
6. Allocation: install the allocator in the entry function (`alloc.SetDefault`); give long-lived and interrupt-time
   code explicit allocators; strings never allocate unless made (`+`, `Sprintf`, `Itoa`).
7. `%+v` field names are omitted in kernel builds unless you pass `--fieldnames`.
