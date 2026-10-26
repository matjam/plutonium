// Translated by go2pu from internal/diag/render.go at the go-cutover tag; edited by hand since.

package diag

import (
	"pluto/io"
	"pluto/strings"
	"selfhost/compat"
	"selfhost/internal/syntax/scanner"
	"selfhost/pu"
)

// width is the width explanations are wrapped to, indent included.
const width = 80

// indent starts each line of an explanation.
const indent = "    "

// A Printer writes errors with their explanations. Explanations that are
// the same as one already printed are not repeated.
type Printer struct {
	W     io.Writer
	Brief bool // print only each error's message

	seen  map[string]Code // explanations printed, to their code
	count int             // errors printed
}

// Print writes err to w, as a Printer does.
func Print(w io.Writer, err error, brief bool) {
	(&Printer{W: w, Brief: brief}).Print(err)
}

// Print writes err: each diagnostic in it with its explanation, unless
// p.Brief is set, and any other error as its message. A List, a
// scanner.ErrorList and errors joined with errors.Join are printed one
// error at a time.
func (p *Printer) Print(err error) {
	switch e := err.(type) {
	case nil:
	case *Diagnostic:
		p.diagnostic(e)
	case *List:
		for _, d := range *e {
			p.diagnostic(d)
		}
	case scanner.ErrorList:
		for _, se := range e {
			p.diagnostic(FromSyntax(se, nil))
		}
	case *scanner.ErrorList:
		for _, se := range *e {
			p.diagnostic(FromSyntax(se, nil))
		}
	case interface{ Unwrap() []error }:
		for _, e := range e.Unwrap() {
			p.Print(e)
		}
	default:
		p.separate()
		_, _ = compat.Fprintfb(p.W, "%v\n", compat.ArgS(strings.TrimRight(err.Error(), "\n")))
	}
}

// separate puts a blank line between errors when they have explanations.
func (p *Printer) separate() {
	if p.count > 0 && !p.Brief {
		_, _ = io.WriteString(p.W, "\n")
	}
	p.count++
}

func (p *Printer) diagnostic(d *Diagnostic) {
	p.separate()
	_, _ = compat.Fprintfb(p.W, "%v\n", compat.ArgS(d.Error()))
	if p.Brief {
		return
	}
	body := Explanation(d)
	if body == "" {
		return
	}
	if p.seen == nil {
		p.seen = map[string]Code{}
	}
	if _, ok := p.seen[body]; ok {
		_, _ = compat.Fprintfb(p.W, "%s(explained above; run \"pluto explain %s\" for more)\n", compat.ArgS(indent), compat.ArgS(string(d.Code)))
		return
	}
	p.seen[body] = d.Code
	_, _ = io.WriteString(p.W, body)
}

// Explanation returns the indented lines Print writes under d's message:
// what happened, why, a fix, and where the rule is specified. It is empty
// if d has no code.
func Explanation(d *Diagnostic) string {
	e := Lookup(d.Code)
	if e == nil && d.Text == "" {
		return ""
	}
	b := pu.Alloc[compat.Builder]()
	text, fixes := d.Text, compat.Append([]*Fix{d.Fix}, d.More...)
	if text == "" && e != nil {
		text, fixes = e.Text, []*Fix{e.Fix}
	}
	writeParas(b, text, indent, width)
	if e != nil && e.Why != "" {
		writeParas(b, e.Why, indent, width)
	}
	for _, fix := range fixes {
		switch {
		case fix == nil:
		case fix.Code == "":
			writeParas(b, fix.Intro, indent, width)
		default:
			writeParas(b, compat.Concat(fix.Intro, ":"), indent, width)
			writeCode(b, fix.Code, indent+indent)
		}
	}
	if e != nil && e.Spec != "" {
		writeParas(b, compat.Concat("(see ", e.Spec, ")"), indent, width)
	}
	if e != nil {
		compat.Fprintfb(b, "%s(run \"pluto explain %s\" for a longer explanation)\n", compat.ArgS(indent), compat.ArgS(string(d.Code)))
	}
	return b.String()
}

// writeParas writes text, whose paragraphs are separated by "\n", with
// each line starting with prefix and no longer than width where the words
// allow.
func writeParas(b *compat.Builder, text, prefix string, width int) {
	for _, para := range compat.Split(strings.TrimSpace(text), "\n") {
		if strings.TrimSpace(para) == "" {
			continue
		}
		line := prefix
		empty := true
		for _, w := range compat.Fields(para) {
			if !empty && len(line)+1+len(w) > width {
				b.WriteString(line)
				b.WriteByte('\n')
				line, empty = prefix, true
			}
			if !empty {
				line = compat.Concat(line, " ")
			}
			line = compat.Concat(line, w)
			empty = false
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
}

// writeCode writes lines of code, each starting with prefix, with tabs
// shown as four spaces.
func writeCode(b *compat.Builder, code, prefix string) {
	for _, line := range compat.Split(strings.Trim(code, "\n"), "\n") {
		line = strings.ReplaceAll(line, "\t", "    ")
		if strings.TrimSpace(line) == "" {
			b.WriteByte('\n')
			continue
		}
		b.WriteString(prefix)
		b.WriteString(line)
		b.WriteByte('\n')
	}
}
