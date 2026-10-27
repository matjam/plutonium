// Translated by go2pu from Go 1.27.3's go/doc/comment/print.go; edited by hand since.

// Copyright 2022 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package comment

import (
	"pluto/fmt"
	"pluto/strings"
	"compiler/compat"
	"compiler/pu"
)

// A Printer is a doc comment printer. This copy keeps only Comment, the
// form gofmt uses.
type Printer struct{}

type commentPrinter struct {
	*Printer
}

// Comment returns the standard Go formatting of the [Doc],
// without any comment markers.
func (p *Printer) Comment(d *Doc) []byte {
	cp := &commentPrinter{Printer: p}
	out := pu.Alloc[compat.Buffer]()
	for i, x := range d.Content {
		if i > 0 && blankBefore(x) {
			out.WriteString("\n")
		}
		cp.block(out, x)
	}

	// Print one block containing all the link definitions that were used,
	// and then a second block containing all the unused ones.
	// This makes it easy to clean up the unused ones: gofmt and
	// delete the final block. And it's a nice visual signal without
	// affecting the way the comment formats for users.
	for i := 0; i < 2; i++ {
		used := i == 0
		first := true
		for _, def := range d.Links {
			if def.Used == used {
				if first {
					out.WriteString("\n")
					first = false
				}
				out.WriteString("[")
				out.WriteString(def.Text)
				out.WriteString("]: ")
				out.WriteString(def.URL)
				out.WriteString("\n")
			}
		}
	}

	return out.Bytes()
}

// blankBefore reports whether the block x requires a blank line before it.
// All blocks do, except for Lists that return false from x.BlankBefore().
func blankBefore(x Block) bool {
	if x, ok := x.(*List); ok {
		return x.BlankBefore()
	}
	return true
}

// block prints the block x to out.
func (p *commentPrinter) block(out *compat.Buffer, x Block) {
	switch x := x.(type) {
	default:
		fmt.Fprintf(out, "?%T", x)

	case *Paragraph:
		p.text(out, "", x.Text)
		out.WriteString("\n")

	case *Heading:
		out.WriteString("# ")
		p.text(out, "", x.Text)
		out.WriteString("\n")

	case *Code:
		md := x.Text
		for md != "" {
			var line string
			line, md, _ = strings.Cut(md, "\n")
			if line != "" {
				out.WriteString("\t")
				out.WriteString(line)
			}
			out.WriteString("\n")
		}

	case *List:
		loose := x.BlankBetween()
		for i, item := range x.Items {
			if i > 0 && loose {
				out.WriteString("\n")
			}
			out.WriteString(" ")
			if item.Number == "" {
				out.WriteString(" - ")
			} else {
				out.WriteString(item.Number)
				out.WriteString(". ")
			}
			for i, blk := range item.Content {
				const fourSpace = "    "
				if i > 0 {
					out.WriteString("\n" + fourSpace)
				}
				p.text(out, fourSpace, blk.(*Paragraph).Text)
				out.WriteString("\n")
			}
		}
	}
}

// text prints the text sequence x to out.
func (p *commentPrinter) text(out *compat.Buffer, indent string, x []Text) {
	for _, t := range x {
		switch t := t.(type) {
		case Plain:
			p.indent(out, indent, string(t))
		case Italic:
			p.indent(out, indent, string(t))
		case *Link:
			if t.Auto {
				p.text(out, indent, t.Text)
			} else {
				out.WriteString("[")
				p.text(out, indent, t.Text)
				out.WriteString("]")
			}
		case *DocLink:
			out.WriteString("[")
			p.text(out, indent, t.Text)
			out.WriteString("]")
		}
	}
}

// indent prints s to out, indenting with the indent string
// after each newline in s.
func (p *commentPrinter) indent(out *compat.Buffer, indent, s string) {
	for s != "" {
		line, rest, ok := strings.Cut(s, "\n")
		out.WriteString(line)
		if ok {
			out.WriteString("\n")
			out.WriteString(indent)
		}
		s = rest
	}
}
