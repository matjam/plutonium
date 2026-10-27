// Translated by go2pu from internal/lsp/signature.go; edited by hand since.

package lsp

import (
	"pluto/strings"
	"pluto/unicode/utf8"
	"compiler/compat"
	"compiler/internal/syntax/ast"
	"compiler/internal/types2"
	"compiler/pu"
)

func (s *server) signatureHelp(p *TextDocumentPositionParams) (*SignatureHelp, *rpcError) {
	f, off, rerr := s.fileAt(p.TextDocument.URI, p.Position)
	if f == nil {
		return nil, rerr
	}
	var call *ast.CallExpr
	for _, n := range enclosing(f.ast, f.tok.Pos(off)) {
		if c, ok := n.(*ast.CallExpr); ok && f.tok.Offset(c.Lparen) < off && (!c.Rparen.IsValid() || off <= f.tok.Offset(c.Rparen)) {
			call = c
		}
	}
	if call == nil {
		return nil, nil
	}
	tv, ok := f.pkg.info.Types[call.Fun]
	if !ok || tv.IsType() || tv.Type == nil {
		return nil, nil
	}
	sig, ok := tv.Type.Underlying().(*types2.Signature)
	if !ok {
		return nil, nil
	}
	var obj types2.Object
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		obj = f.pkg.info.Uses[fun]
	case *ast.SelectorExpr:
		obj = f.pkg.info.Uses[fun.Sel]
	case *ast.IndexExpr: // an instantiated generic function
		if id, ok := fun.X.(*ast.Ident); ok {
			obj = f.pkg.info.Uses[id]
		}
	}
	name := "func"
	if obj != nil {
		name = obj.Name()
	}
	info := signatureInfo(name, sig, qualifier(f.pkg.types))
	if obj != nil {
		if doc := s.ws.docOf(obj); doc != nil {
			if text := strings.TrimSpace(doc.Text()); text != "" {
				info.Documentation = pu.New(MarkupContent{Kind: "markdown", Value: text})
			}
		}
	}
	active := argIndex(f.src, f.tok.Offset(call.Lparen)+1, off)
	if n := len(info.Parameters); n > 0 && active >= n && sig.Variadic() {
		active = n - 1
	}
	return pu.New(SignatureHelp{Signatures: compat.Clone([]SignatureInformation{info}), ActiveParameter: active}), nil
}

// signatureInfo builds the label of a signature, recording where each
// parameter is in it (in UTF-16 code units).
func signatureInfo(name string, sig *types2.Signature, qf types2.Qualifier) SignatureInformation {
	var b compat.Builder
	b.WriteString(name)
	b.WriteString("(")
	info := SignatureInformation{Parameters: compat.Clone([]ParameterInformation{})}
	params := sig.Params()
	for i := range params.Len() {
		if i > 0 {
			b.WriteString(", ")
		}
		start := utf16Len(b.Bytes())
		v := params.At(i)
		if v.Name() != "" {
			b.WriteString(v.Name())
			b.WriteString(" ")
		}
		if sig.Variadic() && i == params.Len()-1 {
			b.WriteString("...")
			if s, ok := v.Type().(*types2.Slice); ok {
				b.WriteString(types2.TypeString(s.Elem(), qf))
			} else {
				b.WriteString(types2.TypeString(v.Type(), qf))
			}
		} else {
			b.WriteString(types2.TypeString(v.Type(), qf))
		}
		info.Parameters = compat.Append(info.Parameters, ParameterInformation{Label: [2]int{start, utf16Len(b.Bytes())}})
	}
	b.WriteString(")")
	if res := sig.Results(); res.Len() > 0 {
		b.WriteString(" ")
		r := types2.TypeString(res, qf)
		if res.Len() == 1 && res.At(0).Name() == "" {
			r = strings.TrimSuffix(strings.TrimPrefix(r, "("), ")")
		}
		b.WriteString(r)
	}
	info.Label = b.String()
	return info
}

// utf16Len returns the length of s in UTF-16 code units.
func utf16Len(s []byte) int {
	n := 0
	for len(s) > 0 {
		r, size := utf8.DecodeRune(s)
		n += runeUnits(r, size)
		s = s[size:]
	}
	return n
}

// argIndex returns the index of the argument at off in a call whose
// arguments start at start: the number of commas between them that are
// not inside brackets, strings or comments.
func argIndex(src []byte, start, off int) int {
	n, depth := 0, 0
	for i := start; i < off && i < len(src); i++ {
		switch c := src[i]; c {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				n++
			}
		case '"', '\'', '`':
			for i++; i < off && src[i] != c; i++ {
				if src[i] == '\\' && c != '`' {
					i++
				}
			}
		case '/':
			if i+1 < len(src) && src[i+1] == '/' {
				for i < off && src[i] != '\n' {
					i++
				}
			}
		}
	}
	return n
}
