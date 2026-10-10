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

// fireTime returns the time a timer sends when it fires: the wall clock
// now, with the monotonic reading mono that fired it (the same clock as
// Now's).
func fireTime(mono int64) Time {
	wall := clock(clockRealtime)
	t := Unix(wall.sec, wall.nsec)
	t.hasMono = true
	t.mono = mono
	return t
}
