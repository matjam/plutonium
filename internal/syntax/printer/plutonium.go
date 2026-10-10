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
	p.packedLayout(x.Order, x.Backing, x.Fields)
}

// packedLayout prints the byte order, the backing type and the fields of a
// packed type or packed union.
func (p *printer) packedLayout(order ast.Endianness, backing ast.Expr, fields *ast.FieldList) {
	switch order {
	case ast.LittleEndian:
		p.print(blank, &ast.Ident{Name: "le"})
	case ast.BigEndian:
		p.print(blank, &ast.Ident{Name: "be"})
	}
	if backing != nil {
		p.print(blank)
		p.expr(backing)
	}
	p.blankFieldList(fields)
}

// blankFieldList prints a field list after a blank, in the one-line form as
// well as the multi-line one.
func (p *printer) blankFieldList(fields *ast.FieldList) {
	if p.fieldListIsOneLine(fields, false) {
		// fieldList prints its own blank only in the multi-line form.
		p.print(blank)
	}
	p.fieldList(fields, true, false)
}

// unionType prints "union { variants }" like a struct type, and
// "packed union [le|be] Backing { fields }" like a packed type.
func (p *printer) unionType(x *ast.UnionType) {
	if x.Packed.IsValid() {
		p.setPos(x.Packed)
		p.print(token.PACKED, blank)
	}
	p.setPos(x.Union)
	p.print(&ast.Ident{NamePos: x.Union, Name: "union"})
	if x.Packed.IsValid() {
		p.packedLayout(x.Order, x.Backing, x.Fields)
		return
	}
	p.fieldList(x.Fields, true, false)
}

// variantCase prints the body of a packed union variant:
// "case v1, v2 { fields }".
func (p *printer) variantCase(x *ast.VariantCase) {
	p.setPos(x.Case)
	p.print(token.CASE)
	for i, v := range x.Values {
		if i > 0 {
			p.print(token.COMMA)
		}
		p.print(blank)
		p.expr(v)
	}
	p.blankFieldList(x.Fields)
}

// enumType prints "enum [Base] { members }". Members are laid out like a
// const block: names in one column, "= value" in the next, comments after.
// An enum written on one line without comments stays on one line, with
// members separated by "; ".
func (p *printer) enumType(x *ast.EnumType) {
	p.setPos(x.Enum)
	p.print(token.ENUM)
	if x.Base != nil {
		p.print(blank)
		p.expr(x.Base)
	}
	p.print(blank)
	p.setPos(x.Lbrace)
	p.print(token.LBRACE)

	hasComments := p.commentBefore(p.posFor(x.Rbrace))
	if !hasComments && p.lineFor(x.Lbrace) == p.lineFor(x.Rbrace) {
		for i, m := range x.Members {
			if i > 0 {
				p.print(token.SEMICOLON)
			}
			p.print(blank)
			p.enumMember(m, blank)
		}
		if len(x.Members) > 0 {
			p.print(blank)
		}
		p.setPos(x.Rbrace)
		p.print(token.RBRACE)
		return
	}

	p.print(indent)
	if hasComments || len(x.Members) > 0 {
		p.print(formfeed)
	}
	var line int
	for i, m := range x.Members {
		if i > 0 {
			p.linebreak(p.lineFor(m.Pos()), 1, ignore, p.linesFrom(line) > 0)
		}
		p.setComment(m.Doc)
		p.recordLine(&line)
		extraTabs := p.enumMember(m, vtab)
		if m.Comment != nil {
			for ; extraTabs > 0; extraTabs-- {
				p.print(vtab)
			}
			p.setComment(m.Comment)
		}
	}
	p.print(unindent, formfeed)
	p.setPos(x.Rbrace)
	p.print(token.RBRACE)
}

// enumMember prints one member, with sep before its "= value", and returns
// the number of columns left empty before a line comment.
func (p *printer) enumMember(m *ast.EnumMember, sep whiteSpace) (extraTabs int) {
	p.expr(m.Name)
	if m.Value == nil {
		return 2
	}
	p.print(sep)
	p.setPos(m.Assign)
	p.print(token.ASSIGN, blank)
	p.expr(m.Value)
	return 1
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
