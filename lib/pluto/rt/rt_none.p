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

// signalName names a signal for Panic.Print: there are none.
func signalName(sig int) string { return "" }

// otherPanicking stops a processor that panics while another handles a
// panic.
//
//pu:noreturn
func otherPanicking() { halt() }

// lastResort stops the processor after a panic inside the panic handler,
// or while a panic's message was being written: it disables interrupts
// and halts, without touching memory or devices.
//
//pu:noreturn
func lastResort() { halt() }
