package main

// pluto/time: Duration formatting and parsing, the UTC calendar, Time
// arithmetic and the clocks. The expected values come from Go's package
// time; the program prints a line per failed check and one per group.
// (The checks are a program rather than pluto/time tests because
// pluto/testing imports pluto/os, which imports pluto/time.)

import (
	"pluto/fmt"
	. "pluto/time"
	"unsafe"
)

// T stands in for testing.T.
type T struct {
	name   string
	failed bool
}

func (t *T) Errorf(format string, args ...any) {
	t.failed = true
	fmt.Printf("FAIL %s: ", t.name)
	fmt.Printf(format, args...)
	fmt.Printf("\n")
}

func (t *T) Error(msg string) { t.Errorf("%s", msg) }

func run(name string, f func(t *T)) {
	t := T{name: name}
	f(&t)
	if !t.failed {
		fmt.Printf("ok %s\n", name)
	}
}

func str(b []byte) string { return unsafe.String(unsafe.SliceData(b), len(b)) }

func main() {
	run("DurationString", testDurationString)
	run("ParseDuration", testParseDuration)
	run("DurationMethods", testDurationMethods)
	run("Calendar", testCalendar)
	run("Arithmetic", testArithmetic)
	run("Now", testNow)
}

func testDurationString(t *T) {
	tests := [...]struct {
		d    Duration
		want string
	}{
		{0, "0s"}, {1 * Nanosecond, "1ns"}, {1100 * Nanosecond, "1.1µs"}, {2200 * Microsecond, "2.2ms"},
		{3300 * Millisecond, "3.3s"}, {4*Minute + 5*Second, "4m5s"}, {4*Minute + 5001*Millisecond, "4m5.001s"},
		{5*Hour + 6*Minute + 7001*Millisecond, "5h6m7.001s"}, {8*Minute + 1*Nanosecond, "8m0.000000001s"},
		{1<<63 - 1, "2562047h47m16.854775807s"}, {-1 << 63, "-2562047h47m16.854775808s"},
		{-1 * Nanosecond, "-1ns"}, {-90 * Minute, "-1h30m0s"}, {999 * Microsecond, "999µs"},
	}
	var buf [40]byte
	for _, tt := range tests {
		if got := str(tt.d.AppendString(buf[:0])); got != tt.want {
			t.Errorf("Duration(%d) = %q, want %q", int64(tt.d), got, tt.want)
		}
	}
}

func testParseDuration(t *T) {
	tests := [...]struct {
		in   string
		want Duration
	}{
		{"0", 0}, {"5s", 5 * Second}, {"30s", 30 * Second}, {"1478s", 1478 * Second},
		{"-5s", -5 * Second}, {"+5s", 5 * Second}, {"-0", 0}, {"+0", 0},
		{"5.0s", 5 * Second}, {"5.6s", 5*Second + 600*Millisecond}, {"5.s", 5 * Second},
		{".5s", 500 * Millisecond}, {"1.0s", 1 * Second}, {"1.00s", 1 * Second},
		{"1.004s", 1*Second + 4*Millisecond}, {"1.0040s", 1*Second + 4*Millisecond},
		{"100.00100s", 100*Second + 1*Millisecond}, {"10ns", 10 * Nanosecond},
		{"11us", 11 * Microsecond}, {"12µs", 12 * Microsecond}, {"12μs", 12 * Microsecond},
		{"13ms", 13 * Millisecond}, {"14s", 14 * Second}, {"15m", 15 * Minute}, {"16h", 16 * Hour},
		{"3h30m", 3*Hour + 30*Minute}, {"10.5s4m", 4*Minute + 10*Second + 500*Millisecond},
		{"-2m3.4s", -(2*Minute + 3*Second + 400*Millisecond)},
		{"1h2m3s4ms5us6ns", 1*Hour + 2*Minute + 3*Second + 4*Millisecond + 5*Microsecond + 6*Nanosecond},
		{"39h9m14.425s", 39*Hour + 9*Minute + 14*Second + 425*Millisecond},
		{"52763797000ns", 52763797000 * Nanosecond}, {"0.3333333333333333333h", 20 * Minute},
		{"9007199254740993ns", (1<<53 + 1) * Nanosecond}, {"9223372036854775807ns", (1<<63 - 1) * Nanosecond},
		{"9223372036854775.807us", (1<<63 - 1) * Nanosecond}, {"-9223372036854775808ns", -1 << 63 * Nanosecond},
		{"0.100000000000000000000h", 6 * Minute}, {"0.830103483285477580700h", 49*Minute + 48*Second + 372539827*Nanosecond},
	}
	for _, tt := range tests {
		d, err := ParseDuration(tt.in)
		if err != nil || d != tt.want {
			t.Errorf("ParseDuration(%q) = %d, %v, want %d", tt.in, int64(d), err, int64(tt.want))
		}
	}
	bad := [...]struct {
		in   string
		want error
	}{
		{"", ErrInvalidDuration}, {"3", ErrMissingUnit}, {"-", ErrInvalidDuration}, {"s", ErrInvalidDuration},
		{".", ErrInvalidDuration}, {"-.", ErrInvalidDuration}, {".s", ErrInvalidDuration}, {"+.s", ErrInvalidDuration},
		{"1d", ErrUnknownUnit}, {"\x85\x85", ErrInvalidDuration}, {"\xffff", ErrInvalidDuration},
		{"9223372036854775808ns", ErrInvalidDuration}, {"9223372036854775.808us", ErrInvalidDuration},
	}
	for _, tt := range bad {
		if _, err := ParseDuration(tt.in); err != tt.want {
			t.Errorf("ParseDuration(%q) error = %v, want %v", tt.in, err, tt.want)
		}
	}
}

func testDurationMethods(t *T) {
	d := 90*Minute + 30*Second
	if d.Hours() != 1.5083333333333333 || d.Minutes() != 90.5 || d.Seconds() != 5430 || d.Milliseconds() != 5430000 {
		t.Errorf("conversions: %v %v %v %d", d.Hours(), d.Minutes(), d.Seconds(), d.Milliseconds())
	}
	if d.Truncate(Hour) != Hour || d.Round(Hour) != 2*Hour || (-d).Round(Hour) != -2*Hour || d.Round(0) != d {
		t.Error("Truncate/Round")
	}
	if Duration(-1<<63).Abs() != 1<<63-1 || (-d).Abs() != d {
		t.Error("Abs")
	}
	if Duration(1<<63-1).Round(Hour) != 1<<63-1 {
		t.Error("Round saturates")
	}
}

func testCalendar(t *T) {
	tests := [...]struct {
		unix                      int64
		year                      int
		month                     Month
		day, hour, min, sec, yday int
		wday                      Weekday
		rfc                       string
	}{
		{0, 1970, January, 1, 0, 0, 0, 1, Thursday, "1970-01-01T00:00:00Z"},
		{951782400, 2000, February, 29, 0, 0, 0, 60, Tuesday, "2000-02-29T00:00:00Z"},
		{1700000000, 2023, November, 14, 22, 13, 20, 318, Tuesday, "2023-11-14T22:13:20Z"},
		{-1, 1969, December, 31, 23, 59, 59, 365, Wednesday, "1969-12-31T23:59:59Z"},
		{-62135596800, 1, January, 1, 0, 0, 0, 1, Monday, "0001-01-01T00:00:00Z"},
		{4102444799, 2099, December, 31, 23, 59, 59, 365, Thursday, "2099-12-31T23:59:59Z"},
	}
	var buf [40]byte
	for _, tt := range tests {
		tm := Unix(tt.unix, 0)
		y, m, d := tm.Date()
		h, mi, s := tm.Clock()
		if y != tt.year || m != tt.month || d != tt.day || h != tt.hour || mi != tt.min || s != tt.sec ||
			tm.YearDay() != tt.yday || tm.Weekday() != tt.wday {
			t.Errorf("Unix(%d): %d-%d-%d %d:%d:%d yday %d %s", tt.unix, y, int(m), d, h, mi, s, tm.YearDay(), tm.Weekday().String())
		}
		if got := str(tm.AppendRFC3339(buf[:0])); got != tt.rfc {
			t.Errorf("Unix(%d) = %q, want %q", tt.unix, got, tt.rfc)
		}
		if back := Date(y, m, d, h, mi, s, 0); back.Unix() != tt.unix {
			t.Errorf("Date(%d-%d-%d) = %d, want %d", y, int(m), d, back.Unix(), tt.unix)
		}
	}
	if !Unix(-62135596800, 0).IsZero() || !(Time{}).IsZero() {
		t.Error("zero time")
	}
	if n := Date(2023, October, 32, 25, 0, 0, 0); n.Day() != 2 || n.Month() != November || n.Hour() != 1 {
		t.Errorf("Date normalises: %d %s %d", n.Day(), n.Month().String(), n.Hour())
	}
	if got := str(Unix(1, 500000000).AppendRFC3339(buf[:0])); got != "1970-01-01T00:00:01.5Z" {
		t.Errorf("fraction: %q", got)
	}
	if Unix(1, -1).UnixNano() != 999999999 || UnixMilli(1500).UnixNano() != 1500000000 || Unix(3, 7).UnixMilli() != 3000 {
		t.Error("Unix normalisation")
	}
	if Month(13).String() != "%!Month()" || Saturday.String() != "Saturday" {
		t.Error("names")
	}
}

func testArithmetic(t *T) {
	a := Unix(100, 900000000)
	b := a.Add(200 * Millisecond)
	if b.Unix() != 101 || b.Nanosecond() != 100000000 || b.Sub(a) != 200*Millisecond || a.Sub(b) != -200*Millisecond {
		t.Errorf("Add/Sub: %d %d %d", b.Unix(), b.Nanosecond(), int64(b.Sub(a)))
	}
	if !a.Before(b) || !b.After(a) || a.Equal(b) || a.Compare(b) != -1 || b.Compare(a) != 1 || a.Compare(a) != 0 {
		t.Error("comparisons")
	}
	if c := a.Add(-1 * Second); c.Unix() != 99 || c.Nanosecond() != 900000000 {
		t.Error("Add negative")
	}
	far := Unix(1<<62, 0)
	if far.Sub(Unix(0, 0)) != 1<<63-1 || Unix(0, 0).Sub(far) != -1<<63 {
		t.Error("Sub saturates")
	}
}

func testNow(t *T) {
	start := Now()
	if start.Year() < 2024 {
		t.Errorf("Now is in %d", start.Year())
	}
	Sleep(2 * Millisecond)
	el := Since(start)
	if el < 2*Millisecond || el > 5*Second {
		t.Errorf("slept 2ms, Since says %d ns", int64(el))
	}
	later := start.Add(Hour)
	if later.Sub(start) != Hour || Until(later) <= 0 || !later.After(Now()) {
		t.Error("monotonic arithmetic")
	}
	if s := start.StripMono(); s.Sub(start.StripMono()) != 0 || !s.Equal(start) {
		t.Error("StripMono")
	}
}
