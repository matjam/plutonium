package main

// The library functions that build strings (language spec section 6.10),
// with Go's behaviour: run with Go's fmt, strings and strconv (and without
// the Builder's Free and errors.Free, which Go does not need) this program
// prints the same.

import (
	"pluto/errors"
	"pluto/fmt"
	"pluto/strconv"
	"pluto/strings"
)

type point struct{ x, y int }

func (p point) String() string { return "(" + strconv.Itoa(p.x) + "," + strconv.Itoa(p.y) + ")" }

func main() {
	s := fmt.Sprintf("%d-%s-%q-%5.2f|%-4d|%x", 42, "go", "q\"t", 3.14159, 7, "hi")
	fmt.Println(s, len(s))
	fmt.Println(fmt.Sprint("a", 1, 2, "b", point{1, 2}), fmt.Sprintln("x", 3))
	fmt.Println(fmt.Sprintf("%v %s", point{3, 4}, []string{"no"}[0]))

	line := ""
	for _, n := range []int{0, 7, 42, 99, 100, -5, 1234567, -9223372036854775808} {
		line += strconv.Itoa(n) + " "
	}
	fmt.Println(strings.TrimSpace(line))
	fmt.Println(strconv.FormatInt(255, 16), strconv.FormatInt(-8, 2), strconv.FormatUint(1<<63, 36), strconv.FormatBool(true))
	fmt.Println(strconv.Quote("tab\there \"q\" é\x01"), strconv.QuoteRune('x'), strconv.QuoteRune('\n'))
	fmt.Println(strconv.FormatFloat(1.5, 'f', 3, 64), strconv.FormatFloat(-0.000125, 'e', -1, 64), strconv.FormatFloat(1e21, 'g', -1, 64))

	fmt.Println(strings.Repeat("ab", 3), strings.Repeat("x", 0) == "", strings.Repeat("é", 2))
	fmt.Println(strings.Join([]string{"a", "b", "c"}, ", "), strings.Join(nil, "-") == "", strings.Join([]string{"solo"}, "+"))
	fmt.Println(strings.Replace("oink oink oink", "k", "ky", 2), strings.Replace("oink oink oink", "oink", "moo", -1))
	fmt.Println(strings.ReplaceAll("banana", "a", "o"), strings.Replace("abc", "", "-", -1), strings.Replace("abc", "x", "y", -1))
	// Only ASCII letters change case (there are no Unicode tables yet).
	fmt.Println(strings.ToUpper("Hello, world!"), strings.ToLower("HELLO ABC é"), strings.ToUpper("ALREADY"))
	fmt.Println(strings.Map(func(r rune) rune {
		if r == 'a' {
			return 'A'
		}
		if r == 'x' {
			return -1
		}
		return r
	}, "banxana"))

	var b strings.Builder
	for i := 0; i < 5; i++ {
		b.WriteString(strconv.Itoa(i))
		b.WriteByte(' ')
	}
	b.WriteRune('ß')
	first := b.String()
	b.WriteString(" more")
	second := b.String()
	b.Reset()
	b.WriteString("fresh")
	fmt.Println(first, "|", second, "|", b.String(), b.Len())
	b.Free()

	var arr [8]string
	parts := arr[:0]
	for i := 0; i < 4; i++ {
		parts = append(parts, fmt.Sprintf("p%d", i*i))
	}
	fmt.Println(strings.Join(parts, "/"), len(parts))

	err := fmt.Errorf("wrap %d: %w", 1, errors.New("inner"))
	fmt.Println(err, errors.Unwrap(err))
	msg := err.Error()
	fmt.Println(msg[:6], len(msg))
	errors.Free(err) // msg keeps the message
	fmt.Println(msg)
}
