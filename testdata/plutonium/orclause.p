package main

// Or clauses (language spec section 11.27): the forms of the statement,
// the short forms of return, the clause as an exit from its blocks, and
// the order of evaluation.

import (
	"pluto/errors"
	"pluto/fmt"
	"pluto/strconv"
	"pluto/strings"
)

var errEmpty = errors.New("no numbers")
var errOdd = errors.New("odd")

// sum adds the comma-separated numbers in text (the complete program of
// section 11.27).
func sum(text string) (int, error) {
	total, count := 0, 0
	for field := range strings.SplitSeq(text, ",") {
		n := strconv.Atoi(strings.TrimSpace(field)) or return fmt.Errorf("number %d: %w", count+1, err)
		total += n
		count++
	}
	if count == 0 {
		return 0, errEmpty
	}
	return total, nil
}

// skipBad adds the numbers of text that parse and counts the rest.
func skipBad(text string) (total, bad int) {
	for field := range strings.SplitSeq(text, ",") {
		n := strconv.Atoi(strings.TrimSpace(field)) or {
			bad++
			continue
		}
		total += n
	}
	return
}

// parseAll keeps the count of the fields parsed before a failure: the
// short form leaves named results as they are.
func parseAll(fields []string) (parsed int, err error) {
	for _, f := range fields {
		strconv.Atoi(f) or return // parsed keeps the count so far, and err is set
		parsed++
	}
	return
}

func half(n int) (int, error) {
	if n%2 != 0 {
		return n, errOdd // a value that matters is not seen by the clause
	}
	return n / 2, nil
}

func check(n int) error {
	if n < 0 {
		return errors.New("negative")
	}
	return nil
}

func pair(n int) (int, string, error) {
	if n == 0 {
		return 0, "", errEmpty
	}
	return n, strconv.Itoa(n), nil
}

// shortZero returns zero values and the error.
func shortZero(n int) (int, string, error) {
	h := half(n) or return
	return h, "ok", nil
}

// shortExpr returns E as the error.
func shortExpr(n int) (int, error) {
	h := half(n) or return fmt.Errorf("half of %d: %w", n, err)
	return h, nil
}

// errOnly passes the error up from a function whose only result is error.
func errOnly(n int) error {
	check(n) or return
	half(n) or return fmt.Errorf("wrapped: %w", err) // an ordinary return
	return nil
}

// plain returns both results itself, an ordinary return.
func plain(n int) (int, error) {
	h := half(n) or return -1, err
	return h, nil
}

// headers uses or statements in the init statements of if, switch and for.
func headers(xs []string) {
	for _, s := range xs {
		if v := strconv.Atoi(s) or continue; v > 10 {
			fmt.Println("if: big", v)
		} else {
			fmt.Println("if: small", v)
		}
		switch n := strconv.Atoi(s) or continue; {
		case n > 10:
			fmt.Println("switch: big")
		default:
			fmt.Println("switch: small")
		}
		for i := strconv.Atoi(s) or break; i < 2; i++ {
			fmt.Println("for:", i)
		}
	}
	fmt.Println("headers done")
}

// switchBreak: a break in the clause of a switch statement's init
// statement leaves the enclosing for statement, not the switch.
func switchBreak(xs []string) {
	for _, s := range xs {
		switch n := strconv.Atoi(s) or break; {
		case n > 10:
			fmt.Println("switch: big", n)
		default:
			fmt.Println("switch: small", n)
		}
		fmt.Println("after switch", s)
	}
	for _, s := range xs {
		switch x := strconv.Atoi(s) or break; v := any(x).(type) {
		case int:
			fmt.Println("type switch", v)
		}
	}
	fmt.Println("switchBreak done")
}

// branches leaves loops and switches from clauses.
func branches() {
	n := 0
Outer:
	for i := 0; i < 5; i++ {
		for j := 0; j < 5; j++ {
			half(j) or continue
			half(i) or continue Outer
			if j > 2 {
				half(i + 1) or break Outer
			}
			n++
		}
	}
	fmt.Println("branches", n)
	for _, s := range []string{"1", "x", "3"} {
		switch s {
		case "x":
			strconv.Atoi(s) or break // breaks the switch
			fmt.Println("not reached")
		default:
			fmt.Println("case", s)
		}
	}
	i := 0
again:
	i++
	half(i) or goto again
	fmt.Println("goto", i)
}

// shadow shows err as an ordinary variable.
func shadow(n int) error {
	check(n) or {
		err = fmt.Errorf("checked %d: %w", n, err) // the clause may assign it
		return err
	}
	half(n) or {
		err := errors.New("inner") // a block clause may declare its own
		return err
	}
	return nil
}

// defers runs the deferred calls on a return from a clause.
func defers(n int) (res int, err error) {
	defer func() {
		fmt.Println("deferred: res", res, "err", err)
	}()
	res = 7
	half(n) or return
	res = 8
	return
}

// order: the operands of the left side other than calls are evaluated
// only where the call succeeds.
func order(n int) (int, error) {
	a := []int{0, 0}
	i := 5
	a[i] = half(n) or return 0, err // i is out of range: no panic when half fails
	return a[0], nil
}

func index(n int) int {
	fmt.Println("index call", n)
	return n
}

// orderCalls: the calls of the left side come before the call or follows.
func orderCalls() error {
	a := []int{0, 0}
	a[index(1)] = halfLoud(4) or return
	fmt.Println("a", a[0], a[1])
	return nil
}

func halfLoud(n int) (int, error) {
	fmt.Println("halfLoud", n)
	return half(n)
}

// Generic functions.
func parse[T any](s string, conv func(int) T) (T, error) {
	n := strconv.Atoi(s) or {
		var zero T
		return zero, err
	}
	return conv(n), nil
}

func twice[T any](s string, conv func(int) T) ([2]T, error) {
	v := parse(s, conv) or return
	w := parse[T](s, conv) or return
	return [2]T{v, w}, nil
}

// lit uses or in a function literal and a range-over-func loop.
func lit() {
	f := func(s string) (int, error) {
		n := strconv.Atoi(s) or return
		return n * 10, nil
	}
	v := f("4") or panic(err)
	fmt.Println("lit", v)
	_, e := f("q")
	fmt.Println("lit error", e != nil)
}

func firstOdd(text string) (int, error) {
	for field := range strings.SplitSeq(text, ",") {
		n := strconv.Atoi(field) or continue
		half(n) or return n, nil // a return from the range-over-func body
	}
	return 0, errEmpty
}

func sumUntilBad(text string) int {
	total := 0
	for field := range strings.SplitSeq(text, ",") {
		n := strconv.Atoi(field) or break
		total += n
	}
	return total
}

func shortInRange(text string) (int, error) {
	for field := range strings.SplitSeq(text, ",") {
		strconv.Atoi(field) or return
	}
	return 1, nil
}

func report() {
	sum("4, five") or {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("not reached")
}

func pairs() {
	x, s := pair(3) or panic(err)
	fmt.Println("pair", x, s)
	x, s = pair(0) or {
		fmt.Println("pair failed, x and s unchanged:", x, s)
		return
	}
	fmt.Println("not reached")
}

func main() {
	total := sum("1, 2, 3") or panic(err)
	fmt.Println("total", total)
	good, bad := skipBad("1, x, 3, y")
	fmt.Println("skipBad", good, bad)
	report()
	_, e := sum("")
	fmt.Println("empty:", e)

	p, e := parseAll([]string{"1", "2", "x", "4"})
	fmt.Println("parseAll", p, e != nil)

	a, b, e := shortZero(3)
	fmt.Println("shortZero", a, b == "", e)
	a, b, e = shortZero(4)
	fmt.Println("shortZero", a, b, e)
	h, e := shortExpr(5)
	fmt.Println("shortExpr", h, e)
	fmt.Println("errOnly", errOnly(-1), errOnly(3), errOnly(2))
	h, e = plain(3)
	fmt.Println("plain", h, e)

	pairs()
	headers([]string{"1", "20", "q", "0"})
	switchBreak([]string{"1", "20", "q", "3"})
	branches()
	fmt.Println("shadow", shadow(-2), shadow(3), shadow(4))
	r, e := defers(3)
	fmt.Println("defers", r, e)
	r, e = defers(4)
	fmt.Println("defers", r, e)
	o, e := order(3)
	fmt.Println("order", o, e)
	orderCalls()

	ints, e := twice("21", func(n int) int { return n * 2 })
	fmt.Println("generic", ints[0], ints[1], e)
	strs, e := twice("x", func(n int) string { return strconv.Itoa(n) })
	fmt.Println("generic", strs[0] == "", e != nil)

	lit()
	odd, e := firstOdd("q,4,6,7,9")
	fmt.Println("firstOdd", odd, e)
	fmt.Println("sumUntilBad", sumUntilBad("1,2,x,4"))
	n, e := shortInRange("1,2,z")
	fmt.Println("shortInRange", n, e != nil)
	n, e = shortInRange("1,2")
	fmt.Println("shortInRange", n, e)
}
