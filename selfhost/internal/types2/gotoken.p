// Translated by go2pu from internal/types2/gotoken.go at the go-cutover tag; edited by hand since.

package types2

import (
	"selfhost/internal/constant"
	"selfhost/internal/syntax/token"
)

// The constant package takes the syntax/token tokens, which keep every Go
// token at its go/token ordinal.

func constBinaryOp(x constant.Value, op token.Token, y constant.Value) constant.Value {
	return constant.BinaryOp(x, op, y)
}

func constUnaryOp(op token.Token, y constant.Value, prec uint) constant.Value {
	return constant.UnaryOp(op, y, prec)
}

func constCompare(x constant.Value, op token.Token, y constant.Value) bool {
	return constant.Compare(x, op, y)
}

func constShift(x constant.Value, op token.Token, s uint) constant.Value {
	return constant.Shift(x, op, s)
}

func constMakeFromLiteral(lit string, tok token.Token, zero uint) constant.Value {
	return constant.MakeFromLiteral(lit, tok, zero)
}
