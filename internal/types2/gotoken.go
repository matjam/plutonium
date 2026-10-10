package types2

import (
	"go/constant"
	gotoken "go/token"

	"github.com/matjam/plutonium/internal/syntax/token"
)

// The syntax/token package keeps every Go token at its go/token ordinal
// (Plutonium tokens are appended after Go's), so operator and literal
// tokens convert to go/token for go/constant by a plain conversion.

func constBinaryOp(x constant.Value, op token.Token, y constant.Value) constant.Value {
	return constant.BinaryOp(x, gotoken.Token(op), y)
}

func constUnaryOp(op token.Token, y constant.Value, prec uint) constant.Value {
	return constant.UnaryOp(gotoken.Token(op), y, prec)
}

func constCompare(x constant.Value, op token.Token, y constant.Value) bool {
	return constant.Compare(x, gotoken.Token(op), y)
}

func constShift(x constant.Value, op token.Token, s uint) constant.Value {
	return constant.Shift(x, gotoken.Token(op), s)
}

func constMakeFromLiteral(lit string, tok token.Token, zero uint) constant.Value {
	return constant.MakeFromLiteral(lit, gotoken.Token(tok), zero)
}
