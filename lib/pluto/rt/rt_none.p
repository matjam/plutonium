//pu:build none

package rt

import "unsafe"

// In freestanding builds there is no operating system: the program provides
// its own entry point, which must call pluto.main to run package
// initialisation and main.main, and its own panic handler, the function it
// marks //pu:panic. print and println go to the writer it registers.

// A kernel starts processors of its own, so string counts are atomic from
// the start (str.p).
const atomicCountsAtStart = true

// printReentrant says a print statement must detect that its processor is
// already in one (rt.p, printBegin): a panic handler, which must work from
// any state, may print after a fault inside the console write.
const printReentrant = true

var consoleWriter func(p unsafe.Pointer, n int)

// SetConsole registers where print and println write, and Console.
func SetConsole(w func(p unsafe.Pointer, n int)) { consoleWriter = w }

// halt disables interrupts and stops the processor (sys_none_amd64.s).
//
//pu:noreturn
func halt()

// consoleWrite writes n bytes at p to the registered console, if any.
func consoleWrite(p unsafe.Pointer, n int) {
	if consoleWriter != nil {
		consoleWriter(p, n)
	}
}

// selfID identifies the calling processor: its CPU number. Processors the
// kernel has not numbered (SetCPU) count as one, so a panic on one of them
// while another handles a panic goes to lastResort, which halts it.
func selfID() uint64 { return uint64(cpuNumber()) }

// deferOnPanic says a panic runs the panicking task's deferred calls
// before the panic handler: not in freestanding builds, whose handler must
// work from any state (language spec section 8.1).
const deferOnPanic = false

// signalName names a signal for Panic.Print: there are none.
func signalName(sig int) string { return "" }

// otherPanicking stops a processor that panics while another handles a
// panic.
func otherPanicking() { halt() }

// lastResort stops the processor after a panic inside the panic handler,
// or while a panic's message was being written: it disables interrupts
// and halts, without touching memory or devices.
func lastResort() { halt() }

// exit stops the program: a freestanding program has no system to return a
// status to, so the processor halts.
func exit(code int) { halt() }
