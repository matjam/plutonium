//pu:build linux

package time

import (
	"pluto/syscall"
	"unsafe"
)

type timespec struct {
	sec, nsec int64
}

const (
	clockRealtime  = 0
	clockMonotonic = 1
)

func clock(id uintptr) timespec {
	var ts timespec
	syscall.Syscall(syscall.SYS_CLOCK_GETTIME, id, uintptr(unsafe.Pointer(&ts)), 0)
	return ts
}

// Now returns the current time, with a monotonic clock reading.
func Now() Time {
	wall := clock(clockRealtime)
	mono := clock(clockMonotonic)
	t := Unix(wall.sec, wall.nsec)
	t.hasMono = true
	t.mono = mono.sec*1e9 + mono.nsec
	return t
}

// Since returns the time elapsed since t: Now().Sub(t).
func Since(t Time) Duration { return Now().Sub(t) }

// Until returns the duration until t: t.Sub(Now()).
func Until(t Time) Duration { return t.Sub(Now()) }

// Sleep pauses the calling thread for at least d. A negative or zero d
// returns at once.
func Sleep(d Duration) {
	if d <= 0 {
		return
	}
	req := timespec{sec: int64(d / Second), nsec: int64(d % Second)}
	for {
		var rem timespec
		r := syscall.Syscall(syscall.SYS_NANOSLEEP, uintptr(unsafe.Pointer(&req)), uintptr(unsafe.Pointer(&rem)), 0)
		if int64(r) != -int64(syscall.EINTR) {
			return
		}
		req = rem
	}
}
