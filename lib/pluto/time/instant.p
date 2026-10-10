package time

// A Time is an instant with nanosecond precision, in UTC. Times from Now
// also carry a reading of the monotonic clock, which Sub, Since, Until and
// the comparisons use when both operands have one, so that measuring
// elapsed time is immune to changes of the wall clock (as in Go).
//
// The zero Time is January 1, year 1, 00:00:00 UTC.
type Time struct {
	sec     int64 // seconds since January 1, year 1, 00:00:00 UTC
	nsec    int32 // [0, 999999999]
	hasMono bool
	mono    int64 // monotonic clock reading in nanoseconds, if hasMono
}

const (
	secondsPerMinute = 60
	secondsPerHour   = 60 * secondsPerMinute
	secondsPerDay    = 24 * secondsPerHour

	// unixToInternal is the number of seconds from year 1 to 1970.
	unixToInternal int64 = (1969*365 + 1969/4 - 1969/100 + 1969/400) * secondsPerDay
)

// Unix returns the Time sec seconds and nsec nanoseconds after January 1,
// 1970 UTC. nsec may be outside [0, 999999999].
func Unix(sec int64, nsec int64) Time {
	if nsec < 0 || nsec >= 1e9 {
		n := nsec / 1e9
		sec += n
		nsec -= n * 1e9
		if nsec < 0 {
			nsec += 1e9
			sec--
		}
	}
	return Time{sec: sec + unixToInternal, nsec: int32(nsec)}
}

// UnixMilli returns the Time msec milliseconds after January 1, 1970 UTC.
func UnixMilli(msec int64) Time { return Unix(msec/1e3, (msec%1e3)*1e6) }

// Date returns the Time of the given date and time of day in UTC. Values
// outside their usual ranges are normalised, as in Go: October 32 is
// November 1.
func Date(year int, month Month, day, hour, min, sec, nsec int) Time {
	// Normalise the month, then the clock, into the day.
	m := int(month) - 1
	year, m = norm(year, m, 12)
	sec, nsec = norm(sec, nsec, 1e9)
	min, sec = norm(min, sec, 60)
	hour, min = norm(hour, min, 60)
	day, hour = norm(day, hour, 24)
	days := daysFromCivil(int64(year), int64(m+1), 1) + int64(day-1)
	s := days*secondsPerDay + int64(hour*secondsPerHour+min*secondsPerMinute+sec)
	return Time{sec: s + unixToInternal, nsec: int32(nsec)}
}

// norm returns hi and lo adjusted so that lo is in [0, base).
func norm(hi, lo, base int) (int, int) {
	if lo < 0 {
		n := (-lo-1)/base + 1
		hi -= n
		lo += n * base
	}
	if lo >= base {
		n := lo / base
		hi += n
		lo -= n * base
	}
	return hi, lo
}

// IsZero reports whether t is the zero Time.
func (t Time) IsZero() bool { return t.sec == 0 && t.nsec == 0 }

// Unix returns t as seconds since January 1, 1970 UTC.
func (t Time) Unix() int64 { return t.sec - unixToInternal }

// UnixMilli returns t as milliseconds since January 1, 1970 UTC.
func (t Time) UnixMilli() int64 { return t.Unix()*1e3 + int64(t.nsec)/1e6 }

// UnixNano returns t as nanoseconds since January 1, 1970 UTC. The result
// is undefined outside the years 1678 to 2262.
func (t Time) UnixNano() int64 { return t.Unix()*1e9 + int64(t.nsec) }

// Nanosecond returns the nanosecond offset within the second.
func (t Time) Nanosecond() int { return int(t.nsec) }

// StripMono returns t without its monotonic clock reading (Go's
// t.Round(0)), so that comparisons use the wall clock.
func (t Time) StripMono() Time {
	t.hasMono = false
	t.mono = 0
	return t
}

// Add returns t+d.
func (t Time) Add(d Duration) Time {
	dsec := int64(d / 1e9)
	nsec := t.nsec + int32(d%1e9)
	if nsec >= 1e9 {
		dsec++
		nsec -= 1e9
	} else if nsec < 0 {
		dsec--
		nsec += 1e9
	}
	t.sec += dsec
	t.nsec = nsec
	if t.hasMono {
		t.mono += int64(d)
	}
	return t
}

// Sub returns t-u, saturating at the largest and smallest Durations. If
// both carry monotonic readings, those are used.
func (t Time) Sub(u Time) Duration {
	if t.hasMono && u.hasMono {
		return subMono(t.mono, u.mono)
	}
	d := Duration(t.sec-u.sec)*Second + Duration(t.nsec-u.nsec)
	switch {
	case u.Add(d).Equal(t):
		return d
	case t.Before(u):
		return minDuration
	}
	return maxDuration
}

func subMono(t, u int64) Duration {
	d := Duration(t - u)
	if d < 0 && t > u {
		return maxDuration
	}
	if d > 0 && t < u {
		return minDuration
	}
	return d
}

// After reports whether t is after u.
func (t Time) After(u Time) bool {
	if t.hasMono && u.hasMono {
		return t.mono > u.mono
	}
	return t.sec > u.sec || t.sec == u.sec && t.nsec > u.nsec
}

// Before reports whether t is before u.
func (t Time) Before(u Time) bool {
	if t.hasMono && u.hasMono {
		return t.mono < u.mono
	}
	return t.sec < u.sec || t.sec == u.sec && t.nsec < u.nsec
}

// Equal reports whether t and u are the same instant.
func (t Time) Equal(u Time) bool {
	if t.hasMono && u.hasMono {
		return t.mono == u.mono
	}
	return t.sec == u.sec && t.nsec == u.nsec
}

// Compare returns -1, 0 or +1 as t is before, equal to or after u.
func (t Time) Compare(u Time) int {
	switch {
	case t.Before(u):
		return -1
	case t.After(u):
		return 1
	}
	return 0
}

// A Month is a month of the year, January = 1.
type Month int

// The months.
const (
	January Month = 1 + iota
	February
	March
	April
	May
	June
	July
	August
	September
	October
	November
	December
)

var monthNames = [...]string{
	"January", "February", "March", "April", "May", "June",
	"July", "August", "September", "October", "November", "December",
}

// String returns the month's English name, or "%!Month()" for a value that
// is not a month.
func (m Month) String() string {
	if January <= m && m <= December {
		return monthNames[m-1]
	}
	return "%!Month()"
}

// A Weekday is a day of the week, Sunday = 0.
type Weekday int

// The days of the week.
const (
	Sunday Weekday = iota
	Monday
	Tuesday
	Wednesday
	Thursday
	Friday
	Saturday
)

var dayNames = [...]string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}

// String returns the day's English name, or "%!Weekday()" for a value that
// is not a day.
func (d Weekday) String() string {
	if Sunday <= d && d <= Saturday {
		return dayNames[d]
	}
	return "%!Weekday()"
}

// days returns the days since 1970-01-01 and the seconds into that day.
func (t Time) days() (int64, int) {
	s := t.sec - unixToInternal
	d := s / secondsPerDay
	r := s % secondsPerDay
	if r < 0 {
		r += secondsPerDay
		d--
	}
	return d, int(r)
}

// Date returns the year, month and day of t.
func (t Time) Date() (year int, month Month, day int) {
	d, _ := t.days()
	y, m, dd := civilFromDays(d)
	return int(y), Month(m), int(dd)
}

// Year returns the year of t.
func (t Time) Year() int {
	y, _, _ := t.Date()
	return y
}

// Month returns the month of t.
func (t Time) Month() Month {
	_, m, _ := t.Date()
	return m
}

// Day returns the day of the month of t.
func (t Time) Day() int {
	_, _, d := t.Date()
	return d
}

// Clock returns the hour, minute and second of t.
func (t Time) Clock() (hour, min, sec int) {
	_, s := t.days()
	return s / secondsPerHour, s % secondsPerHour / secondsPerMinute, s % secondsPerMinute
}

// Hour returns the hour of t, in [0, 23].
func (t Time) Hour() int {
	h, _, _ := t.Clock()
	return h
}

// Minute returns the minute of t, in [0, 59].
func (t Time) Minute() int {
	_, m, _ := t.Clock()
	return m
}

// Second returns the second of t, in [0, 59].
func (t Time) Second() int {
	_, _, s := t.Clock()
	return s
}

// Weekday returns the day of the week of t.
func (t Time) Weekday() Weekday {
	d, _ := t.days()
	w := (d + 4) % 7 // 1970-01-01 was a Thursday
	if w < 0 {
		w += 7
	}
	return Weekday(w)
}

// YearDay returns the day of the year of t, in [1, 365] or [1, 366].
func (t Time) YearDay() int {
	d, _ := t.days()
	y, _, _ := civilFromDays(d)
	return int(d-daysFromCivil(y, 1, 1)) + 1
}

// civilFromDays converts days since 1970-01-01 to a proleptic Gregorian
// date (Howard Hinnant's algorithm).
func civilFromDays(z int64) (y, m, d int64) {
	z += 719468
	era := z / 146097
	if z < 0 && z%146097 != 0 {
		era--
	}
	doe := z - era*146097
	yoe := (doe - doe/1460 + doe/36524 - doe/146096) / 365
	y = yoe + era*400
	doy := doe - (365*yoe + yoe/4 - yoe/100)
	mp := (5*doy + 2) / 153
	d = doy - (153*mp+2)/5 + 1
	if mp < 10 {
		m = mp + 3
	} else {
		m = mp - 9
	}
	if m <= 2 {
		y++
	}
	return y, m, d
}

// daysFromCivil converts a proleptic Gregorian date to days since
// 1970-01-01.
func daysFromCivil(y, m, d int64) int64 {
	if m <= 2 {
		y--
	}
	era := y / 400
	if y < 0 && y%400 != 0 {
		era--
	}
	yoe := y - era*400
	mp := m - 3
	if m <= 2 {
		mp = m + 9
	}
	doy := (153*mp+2)/5 + d - 1
	doe := yoe*365 + yoe/4 - yoe/100 + doy
	return era*146097 + doe - 719468
}

// RFC3339 returns t in RFC 3339 format with nanoseconds, as AppendRFC3339
// formats it, in a new string.
func (t Time) RFC3339() string {
	var buf [64]byte
	return string(t.AppendRFC3339(buf[:0]))
}

// AppendRFC3339 appends t in RFC 3339 format with nanoseconds, as Go's
// t.Format(time.RFC3339Nano) prints a UTC time ("2006-01-02T15:04:05.999999999Z",
// trailing zeros of the fraction removed).
func (t Time) AppendRFC3339(dst []byte) []byte {
	y, mo, d := t.Date()
	h, mi, s := t.Clock()
	dst = appendInt(dst, y, 4)
	dst = append(dst, '-')
	dst = appendInt(dst, int(mo), 2)
	dst = append(dst, '-')
	dst = appendInt(dst, d, 2)
	dst = append(dst, 'T')
	dst = appendInt(dst, h, 2)
	dst = append(dst, ':')
	dst = appendInt(dst, mi, 2)
	dst = append(dst, ':')
	dst = appendInt(dst, s, 2)
	if t.nsec != 0 {
		var frac [10]byte
		frac[0] = '.'
		n := int(t.nsec)
		for i := 9; i >= 1; i-- {
			frac[i] = byte('0' + n%10)
			n /= 10
		}
		end := 10
		for frac[end-1] == '0' {
			end--
		}
		dst = append(dst, frac[:end]...)
	}
	return append(dst, 'Z')
}

// appendInt appends x with at least width digits, zero-padded.
func appendInt(dst []byte, x, width int) []byte {
	if x < 0 {
		dst = append(dst, '-')
		x = -x
	}
	var buf [20]byte
	i := len(buf)
	for x >= 10 || width > 1 {
		i--
		buf[i] = byte('0' + x%10)
		x /= 10
		width--
	}
	i--
	buf[i] = byte('0' + x)
	return append(dst, buf[i:]...)
}
