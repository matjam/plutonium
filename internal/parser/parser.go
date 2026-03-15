// Package parser implements a recursive-descent parser for Plutonium source files.
// It turns .pu source text into an AST. Plutonium is a kernel-safe subset of Go,
// so it rejects forbidden constructs: go, chan, select, map type.
package parser

import (
	"fmt"

	"github.com/matjam/plutonium/internal/ast"
	"github.com/matjam/plutonium/internal/scanner"
	"github.com/matjam/plutonium/internal/token"
)

// Mode controls optional parser behavior.
type Mode uint

const (
	ParseComments Mode = 1 << iota // preserve comments in AST (not yet used)
)

// ParseFile parses a single Plutonium source file.
// src must be []byte source text.
// Returns the AST and any errors.
func ParseFile(fset *token.FileSet, filename string, src []byte, mode Mode) (*ast.File, error) {
	file := fset.AddFile(filename, -1, len(src))
	p := &parser{}
	p.init(file, src, fset, mode)
	f := p.parseFile()
	return f, p.errors.Err()
}

type parser struct {
	file    *token.File
	fset    *token.FileSet
	scanner scanner.Scanner
	errors  ErrorList
	mode    Mode

	// current token
	pos token.Pos
	tok token.Token
	lit string

	// noCompositeLit disables composite literal parsing in primary expressions.
	// Set when parsing conditions in if/for/switch to avoid ambiguity with block stmts.
	noCompositeLit bool
}

func (p *parser) init(file *token.File, src []byte, fset *token.FileSet, mode Mode) {
	p.file = file
	p.fset = fset
	p.mode = mode
	p.scanner.Init(file, src, func(pos token.Pos, msg string) {
		p.error(pos, msg)
	})
	p.next() // prime the pump
}

func (p *parser) error(pos token.Pos, msg string) {
	p.errors.Add(p.fset.Position(pos), msg)
}

func (p *parser) errorExpected(pos token.Pos, what string) {
	msg := "expected " + what
	if pos == p.pos {
		msg += fmt.Sprintf(", found %q", p.tok)
		if p.tok.IsLiteral() {
			msg += " " + p.lit
		}
	}
	p.error(pos, msg)
}

func (p *parser) next() {
	p.pos, p.tok, p.lit = p.scanner.Scan()
}

func (p *parser) expect(tok token.Token) token.Pos {
	pos := p.pos
	if p.tok != tok {
		p.errorExpected(pos, "'"+tok.String()+"'")
	}
	p.next()
	return pos
}

func (p *parser) expectSemi() {
	if p.tok == token.SEMICOLON {
		p.next()
	} else if p.tok != token.RBRACE && p.tok != token.EOF {
		p.errorExpected(p.pos, "';'")
		p.advance(stmtStart)
	}
}

// advance skips tokens until one of the given tokens is found.
func (p *parser) advance(to map[token.Token]bool) {
	for p.tok != token.EOF {
		if to[p.tok] {
			return
		}
		p.next()
	}
}

var stmtStart = map[token.Token]bool{
	token.BREAK:       true,
	token.CONST:       true,
	token.CONTINUE:    true,
	token.DEFER:       true,
	token.FALLTHROUGH: true,
	token.FOR:         true,
	token.GOTO:        true,
	token.IF:          true,
	token.RETURN:      true,
	token.SELECT:      true, // for error recovery
	token.SWITCH:      true,
	token.TYPE:        true,
	token.VAR:         true,
}

var declStart = map[token.Token]bool{
	token.CONST: true,
	token.FUNC:  true,
	token.TYPE:  true,
	token.VAR:   true,
}

var exprEnd = map[token.Token]bool{
	token.COMMA:     true,
	token.COLON:     true,
	token.SEMICOLON: true,
	token.RPAREN:    true,
	token.RBRACK:    true,
	token.RBRACE:    true,
}

// operatorPrecedence returns the binary operator precedence for a token.
// Returns 0 for non-binary operators.
// Precedences match Go's:
//
//	5: * / % << >> & &^
//	4: + - | ^
//	3: == != < <= > >=
//	2: &&
//	1: ||
func operatorPrecedence(tok token.Token) int {
	switch tok {
	case token.LOR:
		return 1
	case token.LAND:
		return 2
	case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
		return 3
	case token.ADD, token.SUB, token.OR, token.XOR:
		return 4
	case token.MUL, token.QUO, token.REM, token.SHL, token.SHR, token.AND, token.AND_NOT:
		return 5
	}
	return 0
}

// ----------------------------------------------------------------------------
// File

func (p *parser) parseFile() *ast.File {
	// package clause
	pos := p.expect(token.PACKAGE)
	name := p.parseIdent()
	p.expectSemi()

	var decls []ast.Decl
	var imports []*ast.ImportSpec

	// import declarations
	for p.tok == token.IMPORT {
		gd := p.parseGenDecl(token.IMPORT)
		decls = append(decls, gd)
		for _, spec := range gd.Specs {
			if is, ok := spec.(*ast.ImportSpec); ok {
				imports = append(imports, is)
			}
		}
	}

	// top-level declarations
	for p.tok != token.EOF {
		decls = append(decls, p.parseTopLevelDecl())
	}

	// collect directives from scanner
	var directives []ast.Directive
	for _, d := range p.scanner.Directives() {
		directives = append(directives, ast.Directive{
			Pos:  d.Pos,
			Text: d.Text,
		})
	}

	return &ast.File{
		Package:    pos,
		Name:       name,
		Decls:      decls,
		Directives: directives,
		Imports:    imports,
	}
}

func (p *parser) parseTopLevelDecl() ast.Decl {
	switch p.tok {
	case token.FUNC:
		return p.parseFuncDecl()
	case token.VAR, token.CONST, token.TYPE:
		return p.parseGenDecl(p.tok)
	case token.IMPORT:
		return p.parseGenDecl(token.IMPORT)
	default:
		pos := p.pos
		p.errorExpected(pos, "declaration")
		p.advance(declStart)
		return &ast.BadDecl{From: pos, To: p.pos}
	}
}

// ----------------------------------------------------------------------------
// Identifiers

func (p *parser) parseIdent() *ast.Ident {
	pos := p.pos
	name := "_"
	if p.tok == token.IDENT {
		name = p.lit
		p.next()
	} else {
		p.expect(token.IDENT)
	}
	return &ast.Ident{NamePos: pos, Name: name}
}

// badExpr creates a placeholder expression for error recovery.
func badExpr(from, to token.Pos) ast.Expr {
	return &ast.BasicLit{ValuePos: from, Kind: token.ILLEGAL, Value: ""}
}

// ----------------------------------------------------------------------------
// Types

// isTypeStart reports whether the current token can start a type.
func (p *parser) isTypeStart() bool {
	switch p.tok {
	case token.IDENT, token.MUL, token.LBRACK, token.STRUCT,
		token.INTERFACE, token.FUNC, token.MAP, token.CHAN,
		token.LPAREN:
		return true
	}
	return false
}

func (p *parser) parseType() ast.Expr {
	switch p.tok {
	case token.MUL:
		return p.parsePointerType()
	case token.LBRACK:
		return p.parseArrayOrSliceType()
	case token.STRUCT:
		return p.parseStructType()
	case token.INTERFACE:
		return p.parseInterfaceType()
	case token.FUNC:
		return p.parseFuncType()
	case token.MAP:
		pos := p.pos
		p.error(pos, "map types are not permitted in Plutonium; use pluto/hash instead")
		// consume the map type for error recovery
		p.next() // consume 'map'
		if p.tok == token.LBRACK {
			p.next() // consume '['
			p.parseType()
			p.expect(token.RBRACK)
			p.parseType()
		}
		return badExpr(pos, p.pos)
	case token.CHAN:
		pos := p.pos
		p.error(pos, "channel types are not permitted in Plutonium")
		p.next() // consume 'chan'
		if p.tok == token.ARROW {
			p.next() // consume '<-'
		}
		if p.isTypeStart() {
			p.parseType()
		}
		return badExpr(pos, p.pos)
	case token.LPAREN:
		lparen := p.pos
		p.next()
		typ := p.parseType()
		rparen := p.expect(token.RPAREN)
		return &ast.ParenExpr{Lparen: lparen, X: typ, Rparen: rparen}
	case token.IDENT:
		return p.parseTypeName()
	default:
		pos := p.pos
		p.errorExpected(pos, "type")
		p.next()
		return badExpr(pos, p.pos)
	}
}

func (p *parser) parseTypeName() *ast.Ident {
	return p.parseIdent()
}

func (p *parser) parsePointerType() *ast.PointerType {
	star := p.expect(token.MUL)
	base := p.parseType()
	return &ast.PointerType{Star: star, X: base}
}

func (p *parser) parseArrayOrSliceType() ast.Expr {
	lbrack := p.expect(token.LBRACK)
	if p.tok == token.RBRACK {
		// slice type []T
		p.next()
		elt := p.parseType()
		return &ast.ArrayType{Lbrack: lbrack, Len: nil, Elt: elt}
	}
	if p.tok == token.ELLIPSIS {
		// [...]T array type
		ellipsis := &ast.BasicLit{ValuePos: p.pos, Kind: token.ELLIPSIS, Value: "..."}
		p.next()
		p.expect(token.RBRACK)
		elt := p.parseType()
		return &ast.ArrayType{Lbrack: lbrack, Len: ellipsis, Elt: elt}
	}
	// [N]T array type
	length := p.parseExpr()
	p.expect(token.RBRACK)
	elt := p.parseType()
	return &ast.ArrayType{Lbrack: lbrack, Len: length, Elt: elt}
}

func (p *parser) parseStructType() *ast.StructType {
	pos := p.expect(token.STRUCT)
	lbrace := p.expect(token.LBRACE)

	var fields []*ast.Field
	for p.tok != token.RBRACE && p.tok != token.EOF {
		fields = append(fields, p.parseFieldDecl())
	}

	rbrace := p.expect(token.RBRACE)
	return &ast.StructType{
		Struct: pos,
		Fields: &ast.FieldList{
			Opening: lbrace,
			List:    fields,
			Closing: rbrace,
		},
	}
}

func (p *parser) parseFieldDecl() *ast.Field {
	// Parse names (possibly multiple: X, Y int32) or an embedded type.
	// Strategy:
	//   - If we see IDENT followed by COMMA or a type token, it's named.
	//   - If we see IDENT followed by SEMICOLON/RBRACE, it's embedded.
	var names []*ast.Ident
	var typ ast.Expr

	if p.tok == token.IDENT {
		// Collect all identifiers before the type.
		first := p.parseIdent()
		names = append(names, first)
		for p.tok == token.COMMA {
			p.next()
			names = append(names, p.parseIdent())
		}
		// If the next token starts a type, the names are field names.
		if p.isTypeStart() {
			typ = p.parseType()
		} else {
			// Embedded type: the single "name" is actually the type.
			if len(names) == 1 {
				typ = names[0]
				names = nil
			} else {
				// Multiple idents without a type — likely an error, treat last as type
				typ = names[len(names)-1]
				names = names[:len(names)-1]
			}
		}
	} else if p.tok == token.MUL {
		// Embedded pointer type: *T
		typ = p.parsePointerType()
	} else {
		typ = p.parseType()
	}

	// optional tag
	var tag *ast.BasicLit
	if p.tok == token.STRING {
		tag = &ast.BasicLit{ValuePos: p.pos, Kind: token.STRING, Value: p.lit}
		p.next()
	}

	p.expectSemi()
	return &ast.Field{Names: names, Type: typ, Tag: tag}
}

func (p *parser) parseInterfaceType() *ast.InterfaceType {
	pos := p.expect(token.INTERFACE)
	lbrace := p.expect(token.LBRACE)

	var methods []*ast.Field
	for p.tok != token.RBRACE && p.tok != token.EOF {
		methods = append(methods, p.parseInterfaceMethod())
	}

	rbrace := p.expect(token.RBRACE)
	return &ast.InterfaceType{
		Interface: pos,
		Methods: &ast.FieldList{
			Opening: lbrace,
			List:    methods,
			Closing: rbrace,
		},
	}
}

func (p *parser) parseInterfaceMethod() *ast.Field {
	// Either a method signature "Name(params) results" or an embedded type
	if p.tok == token.IDENT {
		name := p.parseIdent()
		if p.tok == token.LPAREN {
			// method signature
			params := p.parseParameters()
			var results *ast.FieldList
			if p.tok == token.LPAREN {
				results = p.parseParameters()
			} else if p.isTypeStart() {
				typ := p.parseType()
				results = &ast.FieldList{
					List: []*ast.Field{{Type: typ}},
				}
			}
			ft := &ast.FuncType{Params: params, Results: results}
			p.expectSemi()
			return &ast.Field{Names: []*ast.Ident{name}, Type: ft}
		}
		// embedded interface type
		p.expectSemi()
		return &ast.Field{Type: name}
	}
	// Other embedded type
	typ := p.parseType()
	p.expectSemi()
	return &ast.Field{Type: typ}
}

func (p *parser) parseFuncType() *ast.FuncType {
	pos := p.expect(token.FUNC)
	params := p.parseParameters()
	var results *ast.FieldList
	if p.tok == token.LPAREN {
		results = p.parseParameters()
	} else if p.isTypeStart() {
		typ := p.parseType()
		results = &ast.FieldList{
			List: []*ast.Field{{Type: typ}},
		}
	}
	return &ast.FuncType{Func: pos, Params: params, Results: results}
}

// ----------------------------------------------------------------------------
// Declarations

func (p *parser) parseFuncDecl() *ast.FuncDecl {
	pos := p.expect(token.FUNC)

	// optional receiver
	var recv *ast.FieldList
	if p.tok == token.LPAREN {
		recv = p.parseParameters()
	}

	// function name
	name := p.parseIdent()

	// optional type parameters [T constraint]
	var typeParams *ast.FieldList
	if p.tok == token.LBRACK {
		typeParams = p.parseTypeParams()
	}

	// parameters
	params := p.parseParameters()

	// optional results
	var results *ast.FieldList
	if p.tok == token.LPAREN {
		results = p.parseParameters()
	} else if p.isTypeStart() {
		typ := p.parseType()
		results = &ast.FieldList{
			List: []*ast.Field{{Type: typ}},
		}
	}

	// optional body (nil for assembly stubs)
	var body *ast.BlockStmt
	if p.tok == token.LBRACE {
		body = p.parseBlockStmt()
		p.expectSemi()
	} else {
		p.expectSemi()
	}

	ft := &ast.FuncType{
		Func:       pos,
		TypeParams: typeParams,
		Params:     params,
		Results:    results,
	}

	return &ast.FuncDecl{
		Recv: recv,
		Name: name,
		Type: ft,
		Body: body,
	}
}

// parseTypeParams parses [T constraint | constraint2, U constraint]
func (p *parser) parseTypeParams() *ast.FieldList {
	lbrack := p.expect(token.LBRACK)
	var fields []*ast.Field
	for p.tok != token.RBRACK && p.tok != token.EOF {
		// Parse type parameter name
		name := p.parseIdent()
		// Parse constraint (union of types joined by |)
		constraint := p.parseTypeConstraint()
		fields = append(fields, &ast.Field{
			Names: []*ast.Ident{name},
			Type:  constraint,
		})
		if p.tok == token.COMMA {
			p.next()
		} else {
			break
		}
	}
	rbrack := p.expect(token.RBRACK)
	return &ast.FieldList{Opening: lbrack, List: fields, Closing: rbrack}
}

// parseTypeConstraint parses a constraint which may be a union: int32 | int64 | uint32
// or a plain type like "any"
func (p *parser) parseTypeConstraint() ast.Expr {
	// Parse first type (constraints are type names in Plutonium)
	var result ast.Expr
	if p.tok == token.IDENT {
		result = p.parseIdent()
	} else {
		result = p.parseType()
	}

	// Union type: T | U | ...
	for p.tok == token.OR {
		opPos := p.pos
		p.next() // consume '|'
		var right ast.Expr
		if p.tok == token.IDENT {
			right = p.parseIdent()
		} else {
			right = p.parseType()
		}
		result = &ast.BinaryExpr{X: result, OpPos: opPos, Op: token.OR, Y: right}
	}
	return result
}

func (p *parser) parseGenDecl(tok token.Token) *ast.GenDecl {
	pos := p.pos
	p.expect(tok)

	var lparen, rparen token.Pos
	var specs []ast.Spec

	if p.tok == token.LPAREN {
		lparen = p.pos
		p.next()
		for p.tok != token.RPAREN && p.tok != token.EOF {
			specs = append(specs, p.parseSpec(tok))
		}
		rparen = p.expect(token.RPAREN)
		p.expectSemi()
	} else {
		specs = append(specs, p.parseSpec(tok))
	}

	return &ast.GenDecl{
		TokPos: pos,
		Tok:    tok,
		Lparen: lparen,
		Specs:  specs,
		Rparen: rparen,
	}
}

func (p *parser) parseSpec(tok token.Token) ast.Spec {
	switch tok {
	case token.IMPORT:
		return p.parseImportSpec()
	case token.CONST, token.VAR:
		return p.parseValueSpec()
	case token.TYPE:
		return p.parseTypeSpec()
	default:
		pos := p.pos
		p.errorExpected(pos, "spec")
		p.advance(declStart)
		return &ast.ImportSpec{Path: &ast.BasicLit{ValuePos: pos, Kind: token.STRING, Value: ""}}
	}
}

func (p *parser) parseImportSpec() *ast.ImportSpec {
	var name *ast.Ident
	if p.tok == token.IDENT {
		name = p.parseIdent()
	} else if p.tok == token.PERIOD {
		name = &ast.Ident{NamePos: p.pos, Name: "."}
		p.next()
	}
	path := &ast.BasicLit{ValuePos: p.pos, Kind: token.STRING, Value: p.lit}
	p.expect(token.STRING)
	p.expectSemi()
	return &ast.ImportSpec{Name: name, Path: path}
}

func (p *parser) parseValueSpec() *ast.ValueSpec {
	var names []*ast.Ident
	names = append(names, p.parseIdent())
	for p.tok == token.COMMA {
		p.next()
		names = append(names, p.parseIdent())
	}

	var typ ast.Expr
	if p.isTypeStart() {
		typ = p.parseType()
	}

	var values []ast.Expr
	if p.tok == token.ASSIGN {
		p.next()
		values = p.parseExprList()
	}

	p.expectSemi()
	return &ast.ValueSpec{Names: names, Type: typ, Values: values}
}

func (p *parser) parseTypeSpec() *ast.TypeSpec {
	name := p.parseIdent()

	// optional type parameters
	var typeParams *ast.FieldList
	if p.tok == token.LBRACK {
		typeParams = p.parseTypeParams()
	}

	// optional type alias
	var assign token.Pos
	if p.tok == token.ASSIGN {
		assign = p.pos
		p.next()
	}

	typ := p.parseType()
	p.expectSemi()
	return &ast.TypeSpec{Name: name, TypeParams: typeParams, Assign: assign, Type: typ}
}

// ----------------------------------------------------------------------------
// Parameters and field lists

// parseParameters parses a parameter list enclosed in parentheses.
func (p *parser) parseParameters() *ast.FieldList {
	lparen := p.expect(token.LPAREN)
	var fields []*ast.Field

	if p.tok != token.RPAREN {
		fields = p.parseParameterList()
	}

	rparen := p.expect(token.RPAREN)
	return &ast.FieldList{Opening: lparen, List: fields, Closing: rparen}
}

// parseParameterList parses a comma-separated list of parameter declarations.
// It handles both named (a, b int32) and unnamed (int32, int32) parameters,
// as well as grouped names (a, b, c int32) and mixed forms.
//
// The key ambiguity: "a, b" could be two unnamed params where a and b are types,
// or it could be names that need a following type. We resolve this by checking
// if what follows the ident group is a type start (→ named) or ), EOF (→ unnamed).
func (p *parser) parseParameterList() []*ast.Field {
	var fields []*ast.Field

	for {
		if p.tok == token.RPAREN || p.tok == token.EOF {
			break
		}

		// Handle unnamed variadic: ...T (no name before ellipsis)
		if p.tok == token.ELLIPSIS {
			ellipsisPos := p.pos
			p.next() // consume '...'
			elt := p.parseType()
			typ := &ast.Ellipsis{Ellipsis: ellipsisPos, Elt: elt}
			fields = append(fields, &ast.Field{Type: typ})
			break // variadic must be last param
		}

		if p.tok != token.IDENT {
			// Non-ident type at start (e.g., *T, []T, func(...))
			typ := p.parseType()
			fields = append(fields, &ast.Field{Type: typ})
			if p.tok == token.COMMA {
				p.next()
				continue
			}
			break
		}

		// We have an IDENT. Need to decide if it's a name or a type.
		// Collect one ident, then determine context.
		//
		// Cases:
		//   (T)           → unnamed, T is type
		//   (a T)         → named, a is name, T is type
		//   (a, b)        → unnamed, a and b are types
		//   (a, b T)      → named, a and b are names, T is type
		//   (a, b, c T)   → named, a b c are names, T is type
		//
		// Strategy: collect idents until we see something that's clearly
		// a type start (non-ident type token like *, [, struct, etc.) or
		// until the last ident is NOT followed by comma.
		// After collecting, if current token is a type start → named;
		// otherwise → unnamed.
		//
		// For "a, b T": after consuming "a", we see comma. After comma, "b".
		// After "b", we see "T" (IDENT). But T could be name or type.
		// We need to keep collecting until we see a non-IDENT type or end.
		//
		// The rule: if the LAST ident in the group is followed by another ident
		// or a non-ident type start, the group before the last ident are names
		// and the last ident (or non-ident type) is the type.
		// If all idents are followed by ) or ,, they're all unnamed types.

		// Collect idents separated by commas
		var idents []*ast.Ident
		idents = append(idents, p.parseIdent())

		for p.tok == token.COMMA {
			p.next() // consume ','
			if p.tok == token.IDENT {
				idents = append(idents, p.parseIdent())
			} else if p.tok == token.RPAREN {
				// trailing comma
				break
			} else {
				// non-ident type after comma → all previous idents are unnamed
				for _, id := range idents {
					fields = append(fields, &ast.Field{Type: id})
				}
				idents = nil
				typ := p.parseType()
				fields = append(fields, &ast.Field{Type: typ})
				if p.tok == token.COMMA {
					p.next()
				}
				goto continueOuter
			}
		}

		// Now decide: are idents names or types?
		if p.tok == token.ELLIPSIS {
			// Variadic parameter: name ...T
			ellipsisPos := p.pos
			p.next() // consume '...'
			elt := p.parseType()
			typ := &ast.Ellipsis{Ellipsis: ellipsisPos, Elt: elt}
			fields = append(fields, &ast.Field{Names: idents, Type: typ})
		} else if p.tok == token.IDENT || (p.isTypeStart() && p.tok != token.RPAREN) {
			// There's a type following → idents are names (or idents plus type)
			// But if the only token is IDENT and we have one ident, ambiguous.
			// Actually: if p.tok is IDENT, that IDENT is the type.
			// If p.tok is non-ident type start (like *), that's clearly the type.
			typ := p.parseType()
			fields = append(fields, &ast.Field{Names: idents, Type: typ})
		} else {
			// No type follows → idents are unnamed type names
			for _, id := range idents {
				fields = append(fields, &ast.Field{Type: id})
			}
		}

		if p.tok == token.COMMA {
			p.next()
		} else {
			break
		}

	continueOuter:
	}

	return fields
}

// ----------------------------------------------------------------------------
// Statements

func (p *parser) parseBlockStmt() *ast.BlockStmt {
	lbrace := p.expect(token.LBRACE)
	var stmts []ast.Stmt
	for p.tok != token.RBRACE && p.tok != token.EOF {
		stmts = append(stmts, p.parseStmt())
	}
	rbrace := p.expect(token.RBRACE)
	return &ast.BlockStmt{Lbrace: lbrace, List: stmts, Rbrace: rbrace}
}

func (p *parser) parseStmt() ast.Stmt {
	switch p.tok {
	case token.CONST, token.TYPE, token.VAR:
		return &ast.DeclStmt{Decl: p.parseGenDecl(p.tok)}
	case token.LBRACE:
		s := p.parseBlockStmt()
		p.expectSemi()
		return s
	case token.IF:
		return p.parseIfStmt()
	case token.FOR:
		return p.parseForStmt()
	case token.SWITCH:
		return p.parseSwitchStmt()
	case token.RETURN:
		return p.parseReturnStmt()
	case token.BREAK, token.CONTINUE, token.GOTO, token.FALLTHROUGH:
		return p.parseBranchStmt()
	case token.DEFER:
		return p.parseDeferStmt()
	case token.GO:
		pos := p.pos
		p.error(pos, "goroutines are not permitted in Plutonium ('go' statement is forbidden)")
		p.next() // consume 'go'
		// consume the call expression for error recovery
		p.parsePrimaryExpr()
		p.expectSemi()
		return &ast.BadStmt{From: pos, To: p.pos}
	case token.SELECT:
		pos := p.pos
		p.error(pos, "select statements are not permitted in Plutonium (requires runtime)")
		p.next() // consume 'select'
		// consume the body for error recovery
		if p.tok == token.LBRACE {
			p.next()
			depth := 1
			for depth > 0 && p.tok != token.EOF {
				if p.tok == token.LBRACE {
					depth++
				} else if p.tok == token.RBRACE {
					depth--
				}
				p.next()
			}
		}
		return &ast.BadStmt{From: pos, To: p.pos}
	case token.SEMICOLON:
		pos := p.pos
		p.next()
		return &ast.EmptyStmt{Semicolon: pos, Implicit: false}
	default:
		return p.parseSimpleStmt(true)
	}
}

// parseSimpleStmt parses an expression statement, assignment, or short variable declaration.
// If consumeSemi is true, a semicolon is consumed at the end.
func (p *parser) parseSimpleStmt(consumeSemi bool) ast.Stmt {
	x := p.parseExprList()

	switch p.tok {
	case token.DEFINE, token.ASSIGN,
		token.ADD_ASSIGN, token.SUB_ASSIGN, token.MUL_ASSIGN, token.QUO_ASSIGN,
		token.REM_ASSIGN, token.AND_ASSIGN, token.OR_ASSIGN, token.XOR_ASSIGN,
		token.SHL_ASSIGN, token.SHR_ASSIGN, token.AND_NOT_ASSIGN:
		tokPos := p.pos
		tok := p.tok
		p.next()
		rhs := p.parseExprList()
		stmt := &ast.AssignStmt{Lhs: x, TokPos: tokPos, Tok: tok, Rhs: rhs}
		if consumeSemi {
			p.expectSemi()
		}
		return stmt
	case token.INC, token.DEC:
		// post-increment/decrement
		if len(x) != 1 {
			p.error(p.pos, "expected single expression before ++ or --")
		}
		tokPos := p.pos
		tok := p.tok
		p.next()
		stmt := &ast.IncDecStmt{X: x[0], TokPos: tokPos, Tok: tok}
		if consumeSemi {
			p.expectSemi()
		}
		return stmt
	}

	if len(x) == 1 {
		// Check for label: "ident:"
		if id, ok := x[0].(*ast.Ident); ok && p.tok == token.COLON {
			colon := p.pos
			p.next()
			stmt := p.parseStmt()
			return &ast.LabeledStmt{Label: id, Colon: colon, Stmt: stmt}
		}
		// expression statement
		stmt := &ast.ExprStmt{X: x[0]}
		if consumeSemi {
			p.expectSemi()
		}
		return stmt
	}

	// multi-expression without assignment is an error
	p.error(p.pos, "expected assignment or short variable declaration")
	if consumeSemi {
		p.expectSemi()
	}
	return &ast.ExprStmt{X: x[0]}
}

func (p *parser) parseIfStmt() *ast.IfStmt {
	pos := p.expect(token.IF)

	// optional init stmt; we need to detect if it's "init; cond" or just "cond"
	// Strategy: parse expr list, check for assignment or semicolon.
	// Disable composite literals in condition to avoid ambiguity with {.
	var init ast.Stmt
	var cond ast.Expr

	x := p.parseExprListNoCompositelit()

	if p.tok == token.DEFINE || p.tok == token.ASSIGN ||
		p.tok == token.ADD_ASSIGN || p.tok == token.SUB_ASSIGN ||
		p.tok == token.MUL_ASSIGN || p.tok == token.QUO_ASSIGN ||
		p.tok == token.REM_ASSIGN {
		// init stmt = assignment
		tokPos := p.pos
		tok := p.tok
		p.next()
		rhs := p.parseExprList()
		init = &ast.AssignStmt{Lhs: x, TokPos: tokPos, Tok: tok, Rhs: rhs}
		p.expect(token.SEMICOLON)
		cond = p.parseExpr()
	} else if p.tok == token.SEMICOLON {
		// init stmt = expr stmt
		if len(x) > 0 {
			init = &ast.ExprStmt{X: x[0]}
		}
		p.next()
		cond = p.parseExprNoCompositelit()
	} else {
		// just a condition
		if len(x) == 1 {
			cond = x[0]
		} else if len(x) > 0 {
			cond = x[0] // best effort
		}
	}

	body := p.parseBlockStmt()

	var els ast.Stmt
	if p.tok == token.ELSE {
		p.next()
		switch p.tok {
		case token.IF:
			els = p.parseIfStmt()
		case token.LBRACE:
			els = p.parseBlockStmt()
			p.expectSemi()
		default:
			p.errorExpected(p.pos, "if or block statement")
			els = &ast.BadStmt{From: p.pos, To: p.pos}
		}
	} else {
		p.expectSemi()
	}

	return &ast.IfStmt{If: pos, Init: init, Cond: cond, Body: body, Else: els}
}

func (p *parser) parseForStmt() ast.Stmt {
	pos := p.expect(token.FOR)

	// Infinite loop: for {}
	if p.tok == token.LBRACE {
		body := p.parseBlockStmt()
		p.expectSemi()
		return &ast.ForStmt{For: pos, Body: body}
	}

	// for range x {}  (Go 1.22+ style, no key/value)
	if p.tok == token.RANGE {
		p.next()
		x := p.parseExprNoCompositelit()
		body := p.parseBlockStmt()
		p.expectSemi()
		return &ast.RangeStmt{For: pos, X: x, Body: body}
	}

	// Parse expression list (disable composite lits to avoid ambiguity with {)
	x := p.parseExprListNoCompositelit()

	// Check for range assignment: "k, v := range x" or "k, v = range x"
	if p.tok == token.DEFINE || p.tok == token.ASSIGN {
		tokPos := p.pos
		tok := p.tok
		p.next()
		if p.tok == token.RANGE {
			// range statement
			p.next()
			rangeX := p.parseExprNoCompositelit()
			body := p.parseBlockStmt()
			p.expectSemi()
			var key, val ast.Expr
			if len(x) >= 1 {
				key = x[0]
			}
			if len(x) >= 2 {
				val = x[1]
			}
			return &ast.RangeStmt{
				For:    pos,
				Key:    key,
				Value:  val,
				TokPos: tokPos,
				Tok:    tok,
				X:      rangeX,
				Body:   body,
			}
		}
		// regular for loop init stmt with assignment
		rhs := p.parseExprList()
		init := &ast.AssignStmt{Lhs: x, TokPos: tokPos, Tok: tok, Rhs: rhs}
		return p.parseForWithInit(pos, init)
	}

	if p.tok == token.INC || p.tok == token.DEC {
		tokPos := p.pos
		tok := p.tok
		p.next()
		var expr ast.Expr
		if len(x) > 0 {
			expr = x[0]
		} else {
			expr = badExpr(pos, p.pos)
		}
		init := &ast.IncDecStmt{X: expr, TokPos: tokPos, Tok: tok}
		return p.parseForWithInit(pos, init)
	}

	if p.tok == token.SEMICOLON {
		// "for ; cond ; post {}" or "for init_expr ; cond ; post {}"
		var init ast.Stmt
		if len(x) > 0 {
			init = &ast.ExprStmt{X: x[0]}
		}
		p.next() // consume ';'
		return p.parseForWithInitAndSemi(pos, init)
	}

	// "for cond {}" - x is the condition
	if len(x) == 1 {
		body := p.parseBlockStmt()
		p.expectSemi()
		return &ast.ForStmt{For: pos, Cond: x[0], Body: body}
	}

	// fallback
	body := p.parseBlockStmt()
	p.expectSemi()
	return &ast.ForStmt{For: pos, Body: body}
}

func (p *parser) parseForWithInit(pos token.Pos, init ast.Stmt) ast.Stmt {
	if p.tok == token.SEMICOLON {
		p.next()
		return p.parseForWithInitAndSemi(pos, init)
	}
	// for init { ... } -- init is the only clause (unusual)
	body := p.parseBlockStmt()
	p.expectSemi()
	return &ast.ForStmt{For: pos, Init: init, Body: body}
}

func (p *parser) parseForWithInitAndSemi(pos token.Pos, init ast.Stmt) ast.Stmt {
	// We're past the first semicolon; now parse cond ; post
	// Disable composite lits in condition to avoid ambiguity with {
	var cond ast.Expr
	if p.tok != token.SEMICOLON && p.tok != token.LBRACE {
		cond = p.parseExprNoCompositelit()
	}
	p.expect(token.SEMICOLON)
	var post ast.Stmt
	if p.tok != token.LBRACE {
		post = p.parseSimpleStmt(false)
	}
	body := p.parseBlockStmt()
	p.expectSemi()
	return &ast.ForStmt{For: pos, Init: init, Cond: cond, Post: post, Body: body}
}

func (p *parser) parseSwitchStmt() ast.Stmt {
	pos := p.expect(token.SWITCH)

	// optional init and/or tag
	var init ast.Stmt
	var tag ast.Expr

	if p.tok != token.LBRACE {
		// Could be: switch tag {} or switch init; tag {} or switch init; {}
		// Disable composite lits to avoid ambiguity with {
		x := p.parseExprListNoCompositelit()
		if p.tok == token.DEFINE || p.tok == token.ASSIGN {
			tokPos := p.pos
			tok := p.tok
			p.next()
			rhs := p.parseExprListNoCompositelit()
			init = &ast.AssignStmt{Lhs: x, TokPos: tokPos, Tok: tok, Rhs: rhs}
			if p.tok == token.SEMICOLON {
				p.next()
				if p.tok != token.LBRACE {
					tag = p.parseExprNoCompositelit()
				}
			}
		} else if p.tok == token.SEMICOLON {
			if len(x) > 0 {
				init = &ast.ExprStmt{X: x[0]}
			}
			p.next()
			if p.tok != token.LBRACE {
				tag = p.parseExprNoCompositelit()
			}
		} else {
			if len(x) == 1 {
				tag = x[0]
			}
		}
	}

	// parse switch body
	lbrace := p.expect(token.LBRACE)
	var cases []ast.Stmt
	for p.tok != token.RBRACE && p.tok != token.EOF {
		cases = append(cases, p.parseCaseClause())
	}
	rbrace := p.expect(token.RBRACE)
	body := &ast.BlockStmt{Lbrace: lbrace, List: cases, Rbrace: rbrace}
	p.expectSemi()

	return &ast.SwitchStmt{Switch: pos, Init: init, Tag: tag, Body: body}
}

func (p *parser) parseCaseClause() *ast.CaseClause {
	pos := p.pos
	var list []ast.Expr

	if p.tok == token.CASE {
		p.next()
		list = p.parseExprList()
	} else {
		p.expect(token.DEFAULT)
	}

	colon := p.expect(token.COLON)
	var body []ast.Stmt
	for p.tok != token.CASE && p.tok != token.DEFAULT && p.tok != token.RBRACE && p.tok != token.EOF {
		body = append(body, p.parseStmt())
	}

	return &ast.CaseClause{Case: pos, List: list, Colon: colon, Body: body}
}

func (p *parser) parseReturnStmt() *ast.ReturnStmt {
	pos := p.expect(token.RETURN)
	var results []ast.Expr
	if p.tok != token.SEMICOLON && p.tok != token.RBRACE && p.tok != token.EOF {
		results = p.parseExprList()
	}
	p.expectSemi()
	return &ast.ReturnStmt{Return: pos, Results: results}
}

func (p *parser) parseBranchStmt() *ast.BranchStmt {
	pos := p.pos
	tok := p.tok
	p.next()
	var label *ast.Ident
	if (tok == token.BREAK || tok == token.CONTINUE || tok == token.GOTO) && p.tok == token.IDENT {
		label = p.parseIdent()
	}
	p.expectSemi()
	return &ast.BranchStmt{TokPos: pos, Tok: tok, Label: label}
}

func (p *parser) parseDeferStmt() *ast.DeferStmt {
	pos := p.expect(token.DEFER)
	call := p.parseCallExpr()
	p.expectSemi()
	return &ast.DeferStmt{Defer: pos, Call: call}
}

// parseCallExpr parses a call expression (used by defer).
func (p *parser) parseCallExpr() *ast.CallExpr {
	expr := p.parsePrimaryExpr()
	if call, ok := expr.(*ast.CallExpr); ok {
		return call
	}
	// Wrap non-call in a call expression for error recovery
	return &ast.CallExpr{Fun: expr, Lparen: p.pos, Rparen: p.pos}
}

// ----------------------------------------------------------------------------
// Expressions

func (p *parser) parseExprList() []ast.Expr {
	var list []ast.Expr
	list = append(list, p.parseExpr())
	for p.tok == token.COMMA {
		p.next()
		list = append(list, p.parseExpr())
	}
	return list
}

// parseExprListNoCompositelit parses an expression list with composite literals disabled.
// Used for conditions in if/for/switch to avoid ambiguity with block statements.
func (p *parser) parseExprListNoCompositelit() []ast.Expr {
	old := p.noCompositeLit
	p.noCompositeLit = true
	list := p.parseExprList()
	p.noCompositeLit = old
	return list
}

func (p *parser) parseExpr() ast.Expr {
	return p.parseBinaryExpr(1)
}

// parseExprNoCompositelit parses an expression with composite literals disabled.
func (p *parser) parseExprNoCompositelit() ast.Expr {
	old := p.noCompositeLit
	p.noCompositeLit = true
	x := p.parseExpr()
	p.noCompositeLit = old
	return x
}

// parseBinaryExpr parses binary expressions with operator precedence.
// prec1 is the minimum precedence to consider (1 = lowest).
func (p *parser) parseBinaryExpr(prec1 int) ast.Expr {
	x := p.parseUnaryExpr()
	for {
		oprec := operatorPrecedence(p.tok)
		if oprec < prec1 {
			return x
		}
		op := p.tok
		opPos := p.pos
		p.next()
		y := p.parseBinaryExpr(oprec + 1)
		x = &ast.BinaryExpr{X: x, OpPos: opPos, Op: op, Y: y}
	}
}

func (p *parser) parseUnaryExpr() ast.Expr {
	switch p.tok {
	case token.ADD, token.SUB, token.NOT, token.XOR, token.AND:
		pos := p.pos
		op := p.tok
		p.next()
		x := p.parseUnaryExpr()
		return &ast.UnaryExpr{OpPos: pos, Op: op, X: x}
	case token.MUL:
		// pointer dereference or pointer type in expression context
		pos := p.pos
		p.next()
		x := p.parseUnaryExpr()
		return &ast.StarExpr{Star: pos, X: x}
	case token.ARROW:
		// channel receive (forbidden, but parse for recovery)
		pos := p.pos
		p.error(pos, "channel operations are not permitted in Plutonium")
		p.next()
		x := p.parseUnaryExpr()
		return &ast.UnaryExpr{OpPos: pos, Op: token.ARROW, X: x}
	}
	return p.parsePrimaryExpr()
}

func (p *parser) parsePrimaryExpr() ast.Expr {
	x := p.parseOperand()
	for {
		switch p.tok {
		case token.PERIOD:
			p.next()
			switch p.tok {
			case token.IDENT:
				sel := p.parseIdent()
				x = &ast.SelectorExpr{X: x, Sel: sel}
			case token.LPAREN:
				// type assertion x.(T)
				lparen := p.pos
				p.next()
				var typ ast.Expr
				if p.tok == token.TYPE {
					// x.(type) for type switch
					p.next()
				} else {
					typ = p.parseType()
				}
				rparen := p.expect(token.RPAREN)
				x = &ast.TypeAssertExpr{X: x, Lparen: lparen, Type: typ, Rparen: rparen}
			default:
				p.errorExpected(p.pos, "field name or type assertion")
				p.next()
			}
		case token.LBRACK:
			// index or slice
			lbrack := p.pos
			p.next()
			x = p.parseIndexOrSlice(x, lbrack)
		case token.LPAREN:
			// call
			x = p.parseCallArgs(x)
		case token.LBRACE:
			// composite literal — only valid if x is a type-like expression
			if p.isCompositeLitType(x) {
				x = p.parseCompositeLit(x)
			} else {
				return x
			}
		default:
			return x
		}
	}
}

func (p *parser) isCompositeLitType(x ast.Expr) bool {
	if p.noCompositeLit {
		return false
	}
	switch x.(type) {
	case *ast.Ident, *ast.SelectorExpr, *ast.ArrayType, *ast.StructType:
		return true
	}
	return false
}

func (p *parser) parseIndexOrSlice(x ast.Expr, lbrack token.Pos) ast.Expr {
	// Could be index expr x[i] or slice expr x[lo:hi] or x[lo:hi:max]
	if p.tok == token.COLON {
		// x[:hi] or x[:]
		p.next()
		var high ast.Expr
		if p.tok != token.RBRACK && p.tok != token.COLON {
			high = p.parseExpr()
		}
		var max ast.Expr
		slice3 := false
		if p.tok == token.COLON {
			slice3 = true
			p.next()
			max = p.parseExpr()
		}
		rbrack := p.expect(token.RBRACK)
		return &ast.SliceExpr{X: x, Lbrack: lbrack, Low: nil, High: high, Max: max, Slice3: slice3, Rbrack: rbrack}
	}

	index := p.parseExpr()

	if p.tok == token.COLON {
		// x[lo:hi] or x[lo:hi:max] or x[lo:]
		p.next()
		var high ast.Expr
		if p.tok != token.RBRACK && p.tok != token.COLON {
			high = p.parseExpr()
		}
		var max ast.Expr
		slice3 := false
		if p.tok == token.COLON {
			slice3 = true
			p.next()
			max = p.parseExpr()
		}
		rbrack := p.expect(token.RBRACK)
		return &ast.SliceExpr{X: x, Lbrack: lbrack, Low: index, High: high, Max: max, Slice3: slice3, Rbrack: rbrack}
	}

	rbrack := p.expect(token.RBRACK)
	return &ast.IndexExpr{X: x, Lbrack: lbrack, Index: index, Rbrack: rbrack}
}

func (p *parser) parseCallArgs(fun ast.Expr) *ast.CallExpr {
	lparen := p.expect(token.LPAREN)
	var args []ast.Expr
	var ellipsis token.Pos

	for p.tok != token.RPAREN && p.tok != token.EOF {
		args = append(args, p.parseExpr())
		if p.tok == token.ELLIPSIS {
			ellipsis = p.pos
			p.next()
		}
		if p.tok == token.COMMA {
			p.next()
		} else {
			break
		}
	}

	rparen := p.expect(token.RPAREN)
	return &ast.CallExpr{Fun: fun, Lparen: lparen, Args: args, Ellipsis: ellipsis, Rparen: rparen}
}

func (p *parser) parseCompositeLit(typ ast.Expr) *ast.CompositeLit {
	lbrace := p.expect(token.LBRACE)
	var elts []ast.Expr

	for p.tok != token.RBRACE && p.tok != token.EOF {
		elt := p.parseExpr()
		// Check for key: value
		if p.tok == token.COLON {
			colon := p.pos
			p.next()
			val := p.parseExpr()
			elt = &ast.KeyValueExpr{Key: elt, Colon: colon, Value: val}
		}
		elts = append(elts, elt)
		if p.tok == token.COMMA {
			p.next()
		} else {
			break
		}
	}

	rbrace := p.expect(token.RBRACE)
	return &ast.CompositeLit{Type: typ, Lbrace: lbrace, Elts: elts, Rbrace: rbrace}
}

func (p *parser) parseOperand() ast.Expr {
	switch p.tok {
	case token.IDENT:
		return p.parseIdent()
	case token.INT, token.FLOAT, token.IMAG, token.CHAR, token.STRING:
		x := &ast.BasicLit{ValuePos: p.pos, Kind: p.tok, Value: p.lit}
		p.next()
		return x
	case token.LPAREN:
		lparen := p.pos
		p.next()
		x := p.parseExpr()
		rparen := p.expect(token.RPAREN)
		return &ast.ParenExpr{Lparen: lparen, X: x, Rparen: rparen}
	case token.FUNC:
		// function literal
		ft := p.parseFuncType()
		body := p.parseBlockStmt()
		return &ast.FuncLit{Type: ft, Body: body}
	case token.STRUCT:
		return p.parseStructType()
	case token.LBRACK:
		return p.parseArrayOrSliceType()
	default:
		pos := p.pos
		p.errorExpected(pos, "operand")
		p.advance(stmtStart)
		return badExpr(pos, p.pos)
	}
}
