// Package testpanic tells the compiler's unit tests whether a function
// panics. Plutonium has no recover (section 13.1): a panic ends the program.
// So the Go tests' "defer func() { if recover() == nil { t.Fatal(...) } }()"
// becomes a check in a child process: Panics forks, runs f in the child with
// its standard error sent to /dev/null, and reports whether the child ended
// other than by f returning.
package testpanic

import (
	"pluto/syscall"

	"compiler/compat"
)

// Panics reports whether f panics (or otherwise ends the process) when run.
// f runs in a forked copy of the test process, so nothing it does is seen
// by the caller. The caller must not have tasks running.
func Panics(f func()) bool {
	pid, err := syscall.Fork()
	if err != nil {
		panic("testpanic: fork failed")
	}
	if pid == 0 {
		fd, oerr := syscall.Openat(syscall.AT_FDCWD, compat.Bytes("/dev/null\x00"), syscall.O_WRONLY, 0)
		if oerr == nil {
			syscall.Dup3(fd, 2, 0)
		}
		f()
		syscall.Exit(0)
	}
	var status int32
	for {
		_, werr := syscall.Wait4(pid, &status, 0)
		if werr != syscall.EINTR {
			if werr != nil {
				panic("testpanic: wait failed")
			}
			break
		}
	}
	exited := status&0x7f == 0
	return !exited || (status>>8)&0xff != 0
}
