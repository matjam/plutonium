// Command stringers formats strings, integers, booleans and values that
// have String and Error methods, with every verb, flag and receiver kind
// go2pu's routing of formatting calls to compat's formatter (stringers.go,
// selfhost/compat/printfb.p) meets: the calls it routes, with the operands
// it replaces by a call of their method, and the ones it must leave to fmt.
// TestDifferential requires that Go and the translation print the same.
package main

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
)

// name has a value String method on a struct: left to fmt, as the result
// could be a view of the receiver's copy.
type name struct{ s string }

func (n name) String() string { return "name(" + n.s + ")" }

// node has a pointer String method that dereferences its receiver: fmt
// prints <nil> for a nil *node.
type node struct{ id int }

func (n *node) String() string { return fmt.Sprintf("node%d", n.id) }

// safe has a pointer String method that handles nil itself.
type safe struct{ v int }

func (s *safe) String() string {
	if s == nil {
		return "safe(nil)"
	}
	return fmt.Sprintf("safe%d", s.v)
}

// konst has a pointer String method that never uses its receiver.
type konst struct{}

func (*konst) String() string { return "konst" }

// code is an integer error and Stringer: fmt calls Error.
type code int

func (c code) Error() string  { return fmt.Sprintf("code %d", int(c)) }
func (c code) String() string { return "not this" }

// pfault is an error with a pointer receiver.
type pfault struct{ msg string }

func (p *pfault) Error() string { return "pfault: " + p.msg }

// gostr has a GoString method, used by %#v.
type gostr int

func (gostr) String() string   { return "gostr" }
func (gostr) GoString() string { return "GoStr{}" }

// ptrOnly's String is in *ptrOnly's method set only: a ptrOnly value is
// formatted as a struct.
type ptrOnly struct{ n int }

func (p *ptrOnly) String() string { return "ptrOnly" }

// count is a named integer with a String method.
type count int

func (c count) String() string { return fmt.Sprintf("count=%d", int(c)) }

// flag is a named bool with a String method.
type flag bool

func (f flag) String() string {
	if f {
		return "on"
	}
	return "off"
}

// label and level are named basic types without methods.
type label string
type level uint8

// logger is a printf-like helper: its calls stay as they are (and pluto/fmt
// does not print a nil pointer whose String panics, so none is passed).
type logger struct{ lines []string }

func (l *logger) logf(format string, args ...any) {
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

// describe returns a closure that outlives its frame (go2pu converts it,
// closures.go): its formatting operands are captured variables.
func describe(nd *node, c count, n int) func() string {
	return func() string {
		n++
		return fmt.Sprintf("%v|%s|%d", nd, c, n)
	}
}

func main() {
	n := name{"x"}
	var np *node
	nd := &node{7}
	var sp *safe
	k := (*konst)(nil)
	cd := code(3)
	pf := &pfault{"bad"}
	var nilpf *pfault
	var s fmt.Stringer = n
	var e error = cd
	c := count(4)
	on := flag(true)
	lb := label("lab")
	lv := level(200)

	// Strings: verbs, width, precision, flags, runes that take more than
	// one byte.
	str, uni := "hello", "héllo, 世界"
	fmt.Println(fmt.Sprintf("[%v|%s|%10s|%-10s|%.3s|%8.2s|%-8.4v|%.0s]", str, str, str, str, str, str, str, str))
	fmt.Println(fmt.Sprintf("[%s|%12s|%-12s|%.4s|%q|%12q|%-12q|%x|%X]", uni, uni, uni, uni, uni, uni, uni, uni, uni))
	fmt.Println(fmt.Sprintf("[%q|%q|%x|%s|%v]", "tab\there \"q\" \x01", "", "", "", lb))
	// Longer than the formatter's buffer in the frame, at once and by
	// parts.
	long := strings.Repeat("abcdéfghij", 30)
	fmt.Println(fmt.Sprintf("[%s]", long))
	fmt.Println(fmt.Sprintf("[%.250s|%.250s|%d]", long, long, 7))
	fmt.Fprintf(os.Stdout, "%s|%300s|%-300d|\n", long, "x", 5)

	// Integers: signed and unsigned, every size, flags.
	i, neg, zero := 42, -42, 0
	fmt.Println(fmt.Sprintf("[%d|%v|%5d|%-5d|%05d|%+d|% d|%+05d|% 05d|%-05d]", i, i, i, i, i, i, i, i, i, i))
	fmt.Println(fmt.Sprintf("[%d|%5d|%-5d|%05d|%+d|% d|%x|%X|%08x|%-6x|%d|%x]", neg, neg, neg, neg, neg, neg, neg, neg, neg, neg, zero, zero))
	fmt.Println(fmt.Sprintf("[%d|%d|%x|%d|%x|%X]", int64(math.MinInt64), int64(math.MaxInt64), int64(math.MinInt64), uint64(math.MaxUint64), uint64(math.MaxUint64), uint32(0xdeadbeef)))
	fmt.Println(fmt.Sprintf("[%d|%v|%x|%d|%v|%d|%v]", int8(-128), uint8(255), int16(-1), uint16(65535), uintptr(12), lv, lv))
	fmt.Println(fmt.Sprintf("[%c|%c|%c|%c|%3c|%-3c|%c]", 'a', 'é', 0x4e16, -1, 'z', 'z', 0x110000))
	fmt.Println(fmt.Sprintf("[%q|%q|%q|%q|%5q|%-5q|%q]", 'a', '\'', '\n', 0x7f, 'b', 'b', 0xD800))
	fmt.Println(fmt.Sprintf("[%v|%t|%6v|%-6t|%v]", true, false, true, false, 3 > 2))

	// Operands with methods: pointers, nil, Error before String, integer
	// and bool receivers.
	fmt.Println(fmt.Sprintf("%v|%s|%v|%s", np, nd, sp, &safe{2}))
	fmt.Println(fmt.Sprintf("%v|%v|%5v|%-7s|%.3v", k, cd, c, c, c))
	fmt.Println(fmt.Sprintf("%v|%s|%q|%x|%X", pf, nilpf, c, c, on))
	fmt.Println(fmt.Sprintf("%d|%x|%v|%c", c, c, on, count(65)))

	// Left to fmt: a struct's value String, an interface, %#v, %T, %w,
	// a value whose String is the pointer's only.
	fmt.Println(fmt.Sprintf("%v|%s|%10v|%-8s", n, n, n, n))
	fmt.Println(fmt.Sprintf("%v|%v", s, e))
	fmt.Println(fmt.Sprintf("%v|%#v", gostr(1), gostr(1)))
	fmt.Println(fmt.Sprintf("%v|%v|%v", ptrOnly{1}, &ptrOnly{2}, c))
	fmt.Println(fmt.Sprintf("%T|%d", c, c))

	// %T: the dynamic type's name, of any operand (an interface, nil,
	// a pointer, a slice), with width and precision as %s has them.
	var st fmt.Stringer = nd
	var noErr error
	var anyv any = []int{1}
	fmt.Println(fmt.Sprintf("%T|%T|%T|%T", st, noErr, anyv, pf))
	fmt.Println(fmt.Sprintf("[%12T]|[%-12T]|[%.3T]", nd, c, n))
	fmt.Println(fmt.Errorf("unexpected node type %T", st).Error())
	fmt.Println(fmt.Sprintf("%+v|%+v", 5, c))
	fmt.Println(fmt.Sprintf("%v %%v %s %d", c, nd, 5))
	fmt.Println(fmt.Sprintf("%*v|%-*s", 6, c, 4, nd))

	// Sprint's spaces (by the operand's type), Sprintln, Println.
	fmt.Println(fmt.Sprint(c, c, "s", nd, 3, 4, lb, on, "t"))
	fmt.Println(fmt.Sprint("a", "b", 1, 2, "c"))
	fmt.Println(fmt.Sprint(n, n, "s", nd, 3))
	fmt.Print(fmt.Sprintln(np, cd, pf, c, "str", 1, true))
	fmt.Print(fmt.Sprintln(n, 1))

	// Errorf, Fprintf, Printf, Fprint, Fprintln, Print.
	err := fmt.Errorf("failed %v: %d", nd, 7)
	fmt.Println(err.Error())
	werr := fmt.Errorf("wrapped %v: %w", c, pf)
	fmt.Println(werr, errors.Is(werr, pf))
	fmt.Fprintf(os.Stdout, "%s and %v\n", nd, sp)
	fmt.Printf("%v %s %d\n", c, nd, 9)
	fmt.Fprint(os.Stdout, c, 1, 2, "x\n")
	fmt.Fprintln(os.Stdout, nd, np, c)
	fmt.Fprintln(os.Stdout, n, nd)
	fmt.Print(c, "\n")
	fmt.Println(nd, 1, "two", true)
	nw, werr2 := fmt.Fprintf(os.Stdout, "%d bytes\n", 12)
	fmt.Println(nw, werr2)
	var sb strings.Builder
	fmt.Fprintf(&sb, "<%s|%d>", "b", 1)
	fmt.Println(sb.String())

	d := describe(nd, c, 1)
	d()
	fmt.Println(d())

	var l logger
	l.logf("%v and %s and %d", c, nd, 2)
	fmt.Println(strings.Join(l.lines, "\n"))
}
