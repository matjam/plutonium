package lower

import (
	"encoding/binary"
	"go/constant"
	"strconv"

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
	typerr "github.com/matjam/plutonium/internal/types2/errors"
)

// errorsNew lowers a call errors.New(c), whose message the checker requires
// to be a constant, to the address of a static, read-only errors.Text{Msg:
// c} that belongs to the call site (language spec section 14.1). The same
// call site always yields the same error, in every instance of a generic
// function and however often it runs; different call sites yield different
// errors, as errors.New gives distinct errors in Go.
func (s *fnState) errorsNew(e *ast.CallExpr, fn *types2.Func) []value {
	T := s.typeOf(e)
	tv := s.info.Types[e.Args[0]]
	if tv.Value == nil || tv.Value.Kind() != constant.String {
		s.l.errorf(e.Pos(), diag.CheckerCode(typerr.ErrorsNewNotConstant), "errors.New needs a constant message; use fmt.Errorf")
		return []value{s.zeroValue(T)}
	}
	text, ok := fn.Pkg().Scope().Lookup("Text").(*types2.TypeName)
	if !ok || s.l.sizeof(text.Type()) != 16 {
		s.l.internalf(e.Pos(), "pluto/errors.Text must be a struct holding one string")
		return []value{s.zeroValue(T)}
	}
	sym := s.l.staticError(e, constant.StringVal(tv.Value))
	ptr := scalarValue(s.symAddr(sym), types2.NewPointer(text.Type()))
	return []value{s.assignConvert(ptr, T, e)}
}

// staticError returns the read-only errors.Text for the errors.New call e.
func (l *Lowerer) staticError(e *ast.CallExpr, msg string) *ssa.Sym {
	if sym := l.errorSites[e]; sym != nil {
		return sym
	}
	sym := l.out.Sym(".errors.New."+strconv.Itoa(len(l.errorSites)), ssa.SymData)
	l.errorSites[e] = sym
	// Text{Msg: msg}: the string's data pointer, then its length.
	data := make([]byte, 16)
	binary.LittleEndian.PutUint64(data[8:], uint64(len(msg)))
	d := &ssa.Data{Sym: sym, Size: 16, Align: 8, Bytes: data, ReadOnly: true, Local: true}
	if msg != "" {
		d.Relocs = []ssa.DataReloc{{Off: 0, Sym: l.stringData(msg)}}
	}
	l.out.Data = append(l.out.Data, d)
	return sym
}

// errorsNewValue reports a use of errors.New other than a direct call: the
// static error it makes belongs to a call site, so there is no function to
// take the value of or defer.
func (l *Lowerer) errorsNewValue(fn *types2.Func, pos token.Pos, use string) bool {
	if !types2.IsErrorsNew(fn) {
		return false
	}
	l.errorf(pos, diag.ErrorsNewValue, "errors.New cannot be %s: each call site makes one static error, so call it directly with a constant message", use)
	return true
}
