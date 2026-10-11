// Package time measures and represents time: durations, instants on the
// wall clock and the monotonic clock, and the UTC calendar.
//
// It follows Go's package time where it can. The differences:
//
//   - There are no time zones: every Time is in UTC.
//   - Duration.String and Time.Format(RFC3339Nano)'s counterpart,
//     Time.RFC3339, return new strings; AppendString and AppendRFC3339
//     append to a *vec.Vec[byte] or a *vec.Fixed[byte], which never
//     allocates.
//   - ParseDuration reports one of three static errors rather than an error
//     that quotes its input.
//   - NewTimer, NewTicker and AfterFunc take an optional allocator, and
//     return an owned[Timer] or owned[Ticker], which stops the timer and
//     frees it when dropped. After and Tick return a channel that owns its
//     timer, which stops when the last reference to the channel goes.
//     Nothing is freed by hand.
//   - AfterFunc's function must not capture variables, as with a task
//     statement: it runs on a task of its own after the caller's
//     frame may be gone.
//
// Sleep and the timers park tasks on the scheduler's timers (rt.Timer,
// rt.ParkUntil): a sleeping task holds no thread. They work in hosted
// builds and in freestanding ones whose scheduler provides a clock (abi.md
// section 12.9). Now, Since and Until read the wall clock and exist in
// hosted builds only (time_linux.p); in freestanding builds the times that
// timers send carry only the scheduler's clock.
package time

import (
	"pluto/alloc"
	"pluto/errors"
	"pluto/rt"
	"pluto/vec"
)

// A Duration is the time between two instants as an int64 count of
// nanoseconds, about 292 years at most.
type Duration int64

// Common durations.
const (
	Nanosecond  Duration = 1
	Microsecond          = 1000 * Nanosecond
	Millisecond          = 1000 * Microsecond
	Second               = 1000 * Millisecond
	Minute               = 60 * Second
	Hour                 = 60 * Minute
)

const (
	minDuration Duration = -1 << 63
	maxDuration Duration = 1<<63 - 1
)

// Nanoseconds returns d as an integer nanosecond count.
func (d Duration) Nanoseconds() int64 { return int64(d) }

// Microseconds returns d as an integer microsecond count.
func (d Duration) Microseconds() int64 { return int64(d) / 1e3 }

// Milliseconds returns d as an integer millisecond count.
func (d Duration) Milliseconds() int64 { return int64(d) / 1e6 }

// Seconds returns d as a floating-point number of seconds.
func (d Duration) Seconds() float64 {
	sec := d / Second
	nsec := d % Second
	return float64(sec) + float64(nsec)/1e9
}

// Minutes returns d as a floating-point number of minutes.
func (d Duration) Minutes() float64 {
	min := d / Minute
	nsec := d % Minute
	return float64(min) + float64(nsec)/(60*1e9)
}

// Hours returns d as a floating-point number of hours.
func (d Duration) Hours() float64 {
	hour := d / Hour
	nsec := d % Hour
	return float64(hour) + float64(nsec)/(60*60*1e9)
}

// Truncate returns d rounded toward zero to a multiple of m. If m <= 0 it
// returns d unchanged.
func (d Duration) Truncate(m Duration) Duration {
	if m <= 0 {
		return d
	}
	return d - d%m
}

// Round returns d rounded to the nearest multiple of m, halfway values away
// from zero. The result saturates at the largest and smallest Durations. If
// m <= 0 it returns d unchanged.
func (d Duration) Round(m Duration) Duration {
	if m <= 0 {
		return d
	}
	r := d % m
	if d < 0 {
		r = -r
		if lessThanHalf(r, m) {
			return d + r
		}
		if d1 := d - m + r; d1 < d {
			return d1
		}
		return minDuration
	}
	if lessThanHalf(r, m) {
		return d - r
	}
	if d1 := d + m - r; d1 > d {
		return d1
	}
	return maxDuration
}

// lessThanHalf reports whether x+x < y without overflowing.
func lessThanHalf(x, y Duration) bool { return uint64(x)+uint64(x) < uint64(y) }

// Abs returns the absolute value of d; the smallest Duration becomes the
// largest.
func (d Duration) Abs() Duration {
	switch {
	case d >= 0:
		return d
	case d == minDuration:
		return maxDuration
	}
	return -d
}

// String returns d formatted as Go's Duration.String formats it, such as
// "72h3m0.5s", "1.5µs" or "0s": a new string from the current allocator
// ("0s" is static).
func (d Duration) String() string {
	if d == 0 {
		return "0s"
	}
	var arr [32]byte
	n := d.format(&arr)
	return rt.StringFromSlice(arr[n:], alloc.Default())
}

// AppendString appends d formatted as String formats it to dst, a
// *vec.Vec[byte] (which grows) or a *vec.Fixed[byte] (which keeps what
// fits).
func (d Duration) AppendString[D vec.Appender[byte]](dst D) {
	var arr [32]byte
	n := d.format(&arr)
	dst.Append(arr[n:]...)
}

// format formats d into the end of buf and returns where the text starts.
// It is Go's Duration.format.
func (d Duration) format(buf *[32]byte) int {
	w := len(buf)
	u := uint64(d)
	neg := d < 0
	if neg {
		u = -u
	}
	if u < uint64(Second) {
		// Less than a second: one unit, with a fraction.
		var prec int
		w--
		buf[w] = 's'
		w--
		switch {
		case u == 0:
			buf[w] = '0'
			return w
		case u < uint64(Microsecond):
			prec = 0
			buf[w] = 'n'
		case u < uint64(Millisecond):
			prec = 3
			// U+00B5 'µ' micro sign, as UTF-8.
			w--
			copy(buf[w:], "µ")
		default:
			prec = 6
			buf[w] = 'm'
		}
		w, u = fmtFrac(buf[:w], u, prec)
		w = fmtInt(buf[:w], u)
	} else {
		w--
		buf[w] = 's'
		w, u = fmtFrac(buf[:w], u, 9)
		// u is now whole seconds.
		w = fmtInt(buf[:w], u%60)
		u /= 60
		if u > 0 {
			w--
			buf[w] = 'm'
			w = fmtInt(buf[:w], u%60)
			u /= 60
			if u > 0 {
				w--
				buf[w] = 'h'
				w = fmtInt(buf[:w], u)
			}
		}
	}
	if neg {
		w--
		buf[w] = '-'
	}
	return w
}

// fmtFrac formats the fraction of v/10**prec (such as ".12345") into the
// tail of buf, omitting trailing zeros and the point if the fraction is 0.
// It returns where the output starts and v/10**prec.
func fmtFrac(buf []byte, v uint64, prec int) (int, uint64) {
	w := len(buf)
	print := false
	for i := 0; i < prec; i++ {
		digit := v % 10
		print = print || digit != 0
		if print {
			w--
			buf[w] = byte(digit) + '0'
		}
		v /= 10
	}
	if print {
		w--
		buf[w] = '.'
	}
	return w, v
}

// fmtInt formats v into the tail of buf and returns where the output
// starts.
func fmtInt(buf []byte, v uint64) int {
	w := len(buf)
	if v == 0 {
		w--
		buf[w] = '0'
	} else {
		for v > 0 {
			w--
			buf[w] = byte(v%10) + '0'
			v /= 10
		}
	}
	return w
}

// Errors from ParseDuration.
var (
	ErrInvalidDuration = errors.New("time: invalid duration")
	ErrMissingUnit     = errors.New("time: missing unit in duration")
	ErrUnknownUnit     = errors.New("time: unknown unit in duration")
)

var errLeadingInt = errors.New("time: bad [0-9]*")

// leadingInt consumes the leading [0-9]* from s.
func leadingInt(s string) (x uint64, rem string, err error) {
	i := 0
	for ; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			break
		}
		if x > 1<<63/10 {
			return 0, rem, errLeadingInt
		}
		x = x*10 + uint64(c) - '0'
		if x > 1<<63 {
			return 0, rem, errLeadingInt
		}
	}
	return x, s[i:], nil
}

// leadingFraction consumes the leading [0-9]* from s, as a fraction:
// x/scale, ignoring digits beyond what fits.
func leadingFraction(s string) (x uint64, scale float64, rem string) {
	i := 0
	scale = 1
	overflow := false
	for ; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			break
		}
		if overflow {
			continue
		}
		if x > (1<<63-1)/10 {
			overflow = true
			continue
		}
		y := x*10 + uint64(c) - '0'
		if y > 1<<63 {
			overflow = true
			continue
		}
		x = y
		scale *= 10
	}
	return x, scale, s[i:]
}

// unitOf returns the length in nanoseconds of a unit name, or 0.
func unitOf(u string) uint64 {
	switch u {
	case "ns":
		return uint64(Nanosecond)
	case "us", "µs", "μs": // U+00B5 micro sign and U+03BC Greek mu
		return uint64(Microsecond)
	case "ms":
		return uint64(Millisecond)
	case "s":
		return uint64(Second)
	case "m":
		return uint64(Minute)
	case "h":
		return uint64(Hour)
	}
	return 0
}

// ParseDuration parses a duration string: a possibly signed sequence of
// decimal numbers, each with an optional fraction and a unit suffix, such
// as "300ms", "-1.5h" or "2h45m". Valid units are "ns", "us" (or "µs"),
// "ms", "s", "m", "h". It is Go's ParseDuration, with static errors.
func ParseDuration(s string) (Duration, error) {
	var d uint64
	neg := false
	if s != "" {
		c := s[0]
		if c == '-' || c == '+' {
			neg = c == '-'
			s = s[1:]
		}
	}
	if s == "0" {
		return 0, nil
	}
	if s == "" {
		return 0, ErrInvalidDuration
	}
	for s != "" {
		var v, f uint64
		scale := 1.0
		var err error
		if !(s[0] == '.' || '0' <= s[0] && s[0] <= '9') {
			return 0, ErrInvalidDuration
		}
		pl := len(s)
		v, s, err = leadingInt(s)
		if err != nil {
			return 0, ErrInvalidDuration
		}
		pre := pl != len(s)
		post := false
		if s != "" && s[0] == '.' {
			s = s[1:]
			pl := len(s)
			f, scale, s = leadingFraction(s)
			post = pl != len(s)
		}
		if !pre && !post {
			return 0, ErrInvalidDuration
		}
		i := 0
		for ; i < len(s); i++ {
			c := s[i]
			if c == '.' || '0' <= c && c <= '9' {
				break
			}
		}
		if i == 0 {
			return 0, ErrMissingUnit
		}
		unit := unitOf(s[:i])
		if unit == 0 {
			return 0, ErrUnknownUnit
		}
		s = s[i:]
		if v > 1<<63/unit {
			return 0, ErrInvalidDuration
		}
		v *= unit
		if f > 0 {
			v += uint64(float64(f) * (float64(unit) / scale))
			if v > 1<<63 {
				return 0, ErrInvalidDuration
			}
		}
		d += v
		if d > 1<<63 {
			return 0, ErrInvalidDuration
		}
	}
	if neg {
		return -Duration(d), nil
	}
	if d > 1<<63-1 {
		return 0, ErrInvalidDuration
	}
	return Duration(d), nil
}
