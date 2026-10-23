// Package stringerunit holds formatting calls go2pu must leave to fmt
// (stringers.go), some of which the stringers demo cannot show because
// Plutonium has no fmt.Formatter or explicit argument indexes, and the
// forms of the calls it routes. TestStringerRewrites translates it and
// checks the result.
package stringerunit

import (
	"fmt"
	"io"
	"strings"
)

type formatted struct{}

func (formatted) Format(f fmt.State, verb rune) { fmt.Fprintf(f, "F%c", verb) }
func (formatted) String() string                { return "never" }

// name's String has a value receiver of a struct type: its result could be
// a view of the copy, so a name is left to fmt.
type name struct{ s string }

func (n name) String() string { return n.s }

// kind's String has a value receiver of an integer type.
type kind int

func (k kind) String() string { return "kind" }

// node's String dereferences its pointer receiver.
type node struct{ id int }

func (n *node) String() string { return fmt.Sprint(n.id) }

type label string

func cases(b *strings.Builder, s fmt.Stringer, w io.Writer, np *node) []string {
	f := 1.5
	return []string{
		fmt.Sprintf("%v", formatted{}),  // a Formatter
		fmt.Sprintf("%[1]v %[1]v", 1),   // an argument index
		fmt.Sprintf("%v", b),            // a pointer with a method of another package
		fmt.Sprintf("%v", s),            // an interface
		fmt.Sprintf("%v %s", name{}, 1), // a struct's value String
		fmt.Sprintf("%v %f", np, f),     // a float: the call is left, its operands too
		fmt.Sprintf("%+v %#x %05s %.2d %t", 1, 2, "s", 3, 4),
		fmt.Sprintf("%d %d", 1),               // missing
		fmt.Sprintf("%d", 1, 2),               // extra
		fmt.Sprintf("plain"),                  // nothing to route
		fmt.Sprint(name{}, 1),                 // Sprint of a struct
		fmt.Errorf("%v: %w", np, nil).Error(), // %w
		rewrites("%v", name{}),                // a helper: its calls are left
		// Routed.
		fmt.Sprintf("%v|%s|%d|%x|%c", kind(1), np, kind(2), kind(3), kind(65)),
		fmt.Sprintf("%s %v %d %t", label("l"), uint8(3), 'r', true),
		fmt.Sprint(kind(1), "a", 2, 3, label("b"), np),
		fmt.Sprintln(kind(1), "a", 2),
		fmt.Errorf("e %d", 4).Error(),
	}
}

func prints(w io.Writer, np *node) {
	fmt.Fprintf(w, "%s\n", np)
	fmt.Fprint(w, 1, 2)
	fmt.Fprintln(w, "x", 1)
	fmt.Printf("%d\n", 5)
	fmt.Print("a", "b")
	fmt.Println(np)
}

// rewrites formats name operands its own way before fmt sees them, as
// types2's sprintf does with types and operands.
func rewrites(format string, args ...any) string {
	for i, a := range args {
		if n, ok := a.(name); ok {
			args[i] = "N:" + n.String()
		}
	}
	return fmt.Sprintf(format, args...)
}
