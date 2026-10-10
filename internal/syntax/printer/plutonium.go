// This file prints the Plutonium-specific AST nodes. Type constructors
// (volatile[T], aligned[T, N], ring[T, N], bits[T, N], owned[T] and so on)
// are ordinary index expressions and need nothing here; methods with their
// own type parameters print through signature, like generic functions.

package printer

import (
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
)

// packedType prints "packed [le|be] [Backing] { fields }". The fields are
// laid out like struct fields, but a blank always separates the brace from
// what precedes it, since "packed uint32{" would read as a composite
// literal.
func (p *printer) packedType(x *ast.PackedType) {
	p.setPos(x.Packed)
	p.print(token.PACKED)
	switch x.Order {
	case ast.LittleEndian:
		p.print(blank, &ast.Ident{Name: "le"})
	case ast.BigEndian:
		p.print(blank, &ast.Ident{Name: "be"})
	}
	if x.Backing != nil {
		p.print(blank)
		p.expr(x.Backing)
	}
	if p.fieldListIsOneLine(x.Fields, false) {
		// fieldList prints its own blank only in the multi-line form.
		p.print(blank)
	}
	p.fieldList(x.Fields, true, false)
}

// bitsGroup prints a group of one-bit flags in a packed type:
// "bits { A B C }". A group written on one line stays on one line; a group
// spread over several lines keeps the source's line breaks between names,
// with the first name on the line after the opening brace.
func (p *printer) bitsGroup(x *ast.BitsGroup) {
	p.setPos(x.Bits)
	p.print(&ast.Ident{NamePos: x.Bits, Name: "bits"}, blank)
	p.setPos(x.Lbrace)
	p.print(token.LBRACE)
	if len(x.Names) == 0 {
		p.setPos(x.Rbrace)
		p.print(token.RBRACE)
		return
	}
	if p.lineFor(x.Lbrace) == p.lineFor(x.Rbrace) {
		for _, name := range x.Names {
			p.print(blank)
			p.expr(name)
		}
		p.print(blank)
		p.setPos(x.Rbrace)
		p.print(token.RBRACE)
		return
	}
	p.print(indent)
	for i, name := range x.Names {
		minBreaks := 0
		if i == 0 {
			minBreaks = 1
		}
		if p.linebreak(p.lineFor(name.Pos()), minBreaks, ignore, false) == 0 {
			p.print(blank)
		}
		p.expr(name)
	}
	p.print(unindent)
	p.linebreak(p.lineFor(x.Rbrace), 1, ignore, false)
	p.setPos(x.Rbrace)
	p.print(token.RBRACE)
}
