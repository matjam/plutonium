package compat

import (
	"pluto/strconv"
	"pluto/strings"
	"pluto/syscall"
	"unsafe"
)

// The time the compiler's Go sources measure (how long the new escape
// analysis's rounds take, for its traces and for the order of its solves)
// is a monotonic clock reading. go2pu maps time.Time, time.Duration,
// time.Now and time.Since here rather than to pluto/time, whose timers and
// Sleep bring in the scheduler, which the compiler, running one task, does
// not start.

// A Duration is time.Duration: nanoseconds.
type Duration int64

// A Time is time.Time as the compiler uses it: a monotonic clock reading,
// in nanoseconds.
type Time struct {
	mono int64
}

// Now is time.Now: the monotonic clock.
func Now() Time {
	var ts struct{ sec, nsec int64 }
	syscall.Syscall(syscall.SYS_CLOCK_GETTIME, 1, uintptr(unsafe.Pointer(&ts)), 0) // CLOCK_MONOTONIC
	return Time{mono: ts.sec*1e9 + ts.nsec}
}

// Since is time.Since.
func Since(t Time) Duration { return Now().Sub(t) }

// Sub is t-u.
func (t Time) Sub(u Time) Duration { return Duration(t.mono - u.mono) }

// String formats d as time.Duration does: "1m30.5s", "1.5s", "250ms",
// "12.345µs", "800ns".
func (d Duration) String() string {
	neg := d < 0
	u := uint64(d)
	if neg {
		u = -u
	}
	var s string
	switch {
	case u == 0:
		return "0s"
	case u < 1e3:
		s = Concat(strconv.FormatUint(u, 10), "ns")
	case u < 1e6:
		s = Concat(fraction(u, 1e3), "µs")
	case u < 1e9:
		s = Concat(fraction(u, 1e6), "ms")
	case u < 60e9:
		s = Concat(fraction(u, 1e9), "s")
	default:
		// "1m30.5s", "2h0m0s"
		s = Concat(strconv.FormatUint(u/60e9%60, 10), "m", fraction(u%60e9, 1e9), "s")
		if u >= 3600e9 {
			s = Concat(strconv.FormatUint(u/3600e9, 10), "h", s)
		}
	}
	if neg {
		return Concat("-", s)
	}
	return s
}

// fraction formats u/unit with the fraction's trailing zeros dropped.
func fraction(u, unit uint64) string {
	whole := strconv.FormatUint(u/unit, 10)
	frac := u % unit
	if frac == 0 {
		return whole
	}
	digits := 0
	for p := unit; p > 1; p /= 10 {
		digits++
	}
	f := strconv.FormatUint(frac, 10)
	for len(f) < digits {
		f = Concat("0", f)
	}
	end := len(f)
	for end > 0 && f[end-1] == '0' {
		end--
	}
	return Concat(whole, ".", strings.Slice(f, 0, end))
}
