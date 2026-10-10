package diag

//go:generate go run ./gencatalog

import (
	"fmt"
	"slices"
	"strings"
)

// An Entry is the catalog's explanation of a code.
type Entry struct {
	Code Code
	Name string // a short name, such as IncompatibleAssign

	// Text explains the error in plain words: what went wrong and the idea
	// behind the rule. Paragraphs are separated by "\n". It is printed
	// under an error that has no explanation of its own.
	Text string
	// Why says why Plutonium differs from Go here: no hidden allocation, no
	// garbage collector, kernel safety. It is printed under every error with
	// the code, after the explanation. Empty where Plutonium follows Go.
	Why string
	// Fix is the usual fix, or nil.
	Fix *Fix
	// Spec names the section of the specification that has the rule, as
	// `"Assignability" in the Go spec; Plutonium follows Go here` or
	// `section 4.5 of the Plutonium spec`.
	Spec string

	// Example is a program, or part of one, with the error, and Fixed is
	// the same code corrected. pluto explain prints them.
	Example string
	Fixed   string
}

var catalog = map[Code]*Entry{}

// register adds entries to the catalog.
func register(entries ...*Entry) {
	for _, e := range entries {
		if _, dup := catalog[e.Code]; dup {
			panic("diag: duplicate catalog entry " + string(e.Code))
		}
		catalog[e.Code] = e
	}
}

// Lookup returns the catalog's entry for code, or nil.
func Lookup(code Code) *Entry {
	return catalog[code]
}

// Codes returns every code in the catalog, in order.
func Codes() []Code {
	codes := make([]Code, 0, len(catalog))
	for c := range catalog {
		codes = append(codes, c)
	}
	slices.Sort(codes)
	return codes
}

// Explain returns the long explanation of code that pluto explain prints:
// the explanation, the reason Plutonium differs from Go if it does, an
// example and its fix, and where the rule is specified.
func Explain(code Code) (string, error) {
	e := catalog[Code(strings.ToUpper(string(code)))]
	if e == nil {
		return "", fmt.Errorf("no such error code %q (codes look like E0024)", code)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s\n", e.Code, e.Name)
	for _, para := range strings.Split(e.Text+"\n"+e.Why, "\n") {
		if strings.TrimSpace(para) != "" {
			b.WriteByte('\n')
			writeParas(&b, para, "", width)
		}
	}
	if e.Example != "" {
		b.WriteString("\nFor example:\n\n")
		writeCode(&b, e.Example, "    ")
	}
	if e.Fixed != "" {
		b.WriteString("\nWritten correctly:\n\n")
		writeCode(&b, e.Fixed, "    ")
	}
	if e.Fix != nil && e.Fixed == "" {
		fmt.Fprintf(&b, "\n%s:\n\n", e.Fix.Intro)
		writeCode(&b, e.Fix.Code, "    ")
	}
	if e.Spec != "" {
		fmt.Fprintf(&b, "\nSee %s.\n", e.Spec)
	}
	return b.String(), nil
}
