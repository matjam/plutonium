//pu:build none

package rt

import "unsafe"

// In freestanding builds there is no operating system: the program provides
// its own entry point, which must call pluto.main to run package
// initialisation and main.main. Panics and print go to handlers the program
// registers.

var (
	panicHandler func()
	consoleWriter func(p unsafe.Pointer, n int)
)

// SetPanicHandler registers the function a panic calls after printing its
// message. It must not return. The default disables interrupts and halts.
func SetPanicHandler(h func()) { panicHandler = h }

// SetConsole registers where print, println and panic messages are written.
func SetConsole(w func(p unsafe.Pointer, n int)) { consoleWriter = w }

// halt disables interrupts and stops the processor (sys_none_amd64.s).
//
//pu:noreturn
func halt()

//pu:noreturn
func abort() {
	if panicHandler != nil {
		panicHandler()
	}
	halt()
}

func printBytes(p unsafe.Pointer, n int) {
	if consoleWriter != nil {
		consoleWriter(p, n)
	}
}
