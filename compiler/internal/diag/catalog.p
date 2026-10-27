// Translated by go2pu from internal/diag/catalog.go at the go-cutover tag; edited by hand since.

package diag

import (
	"compiler/compat"
	"compiler/pu"
	"pluto/slices"
	"pluto/strings"
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
	// Spec names the section of the specification that has the rule, in
	// the form pspec produces: `"Assignability" in the Plutonium spec,
	// section 8.4`.
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
			panic(pu.Box(compat.Concat("diag: duplicate catalog entry ", string(e.Code))))
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
	codes := compat.MakeCap[Code](0, len(catalog))
	for c := range catalog {
		codes = compat.Append(codes, c)
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
		return "", compat.NewError(compat.Sprintfb("no such error code %q (codes look like E0024)", compat.ArgS(string(code))))
	}
	b := pu.Alloc[compat.Builder]()
	compat.Fprintfb(b, "%s: %s\n", compat.ArgS(string(e.Code)), compat.ArgS(e.Name))
	for _, para := range compat.Split(compat.Concat(e.Text, "\n", e.Why), "\n") {
		if strings.TrimSpace(para) != "" {
			b.WriteByte('\n')
			writeParas(b, para, "", width)
		}
	}
	if e.Example != "" {
		b.WriteString("\nFor example:\n\n")
		writeCode(b, e.Example, "    ")
	}
	if e.Fixed != "" {
		b.WriteString("\nWritten correctly:\n\n")
		writeCode(b, e.Fixed, "    ")
	}
	if e.Fix != nil && e.Fixed == "" {
		compat.Fprintfb(b, "\n%s:\n\n", compat.ArgS(e.Fix.Intro))
		writeCode(b, e.Fix.Code, "    ")
	}
	if e.Spec != "" {
		compat.Fprintfb(b, "\nSee %s.\n", compat.ArgS(e.Spec))
	}
	return b.String(), nil
}
