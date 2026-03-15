package ir

import (
	"fmt"

	"github.com/matjam/plutonium/internal/ast"
	"github.com/matjam/plutonium/internal/token"
)

// --- Expression lowering ---

// lowerExpr evaluates an expression, emitting IR instructions.
// Returns the result value (may be nil for void expressions).
func (l *lowerer) lowerExpr(expr ast.Expr) *Value {
	return l.lowerExprValue(expr)
}

func (l *lowerer) lowerExprValue(expr ast.Expr) *Value {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *ast.BasicLit:
		return l.lowerBasicLit(e)

	case *ast.Ident:
		return l.lowerIdent(e)

	case *ast.BinaryExpr:
		return l.lowerBinaryExpr(e)

	case *ast.UnaryExpr:
		return l.lowerUnaryExpr(e)

	case *ast.CallExpr:
		return l.lowerCallExpr(e)

	case *ast.ParenExpr:
		return l.lowerExprValue(e.X)

	case *ast.StarExpr:
		// Dereference
		ptr := l.lowerExprValue(e.X)
		if ptr != nil && ptr.Type != nil {
			return l.bb.Load(ptr.Type, ptr)
		}
		return ptr

	case *ast.IndexExpr:
		return l.lowerIndexExpr(e)

	case *ast.SelectorExpr:
		return l.lowerSelectorExpr(e)

	case *ast.CompositeLit:
		return l.lowerCompositeLit(e)
	}
	return nil
}

func (l *lowerer) lowerBasicLit(e *ast.BasicLit) *Value {
	switch e.Kind {
	case token.INT:
		var n int64
		fmt.Sscanf(e.Value, "%d", &n)
		return ConstInt(I64, n)
	case token.FLOAT:
		return &Value{Kind: ValConst, Type: F64}
	case token.STRING:
		lit := l.internString(e.Value)
		// Return a value referencing the string data.
		// The string is a pointer to the .rodata symbol.
		v := &Value{
			ID:     l.fn.nextID,
			Kind:   ValGlobal,
			Type:   PtrTo(U8),
			Name:   fmt.Sprintf(".str.%d", lit.ID),
			IntVal: int64(len(lit.Data)),
		}
		l.fn.nextID++
		return v
	case token.CHAR:
		return ConstInt(I32, 0)
	}
	return ConstInt(I32, 0)
}

func (l *lowerer) lowerIdent(e *ast.Ident) *Value {
	if e.Name == "_" {
		return nil
	}
	slot := l.env[e.Name]
	if slot == nil {
		return ConstInt(I32, 0) // undefined, return 0
	}
	// Load from the alloca slot
	typ := slot.Type
	if typ == nil {
		typ = I32
	}
	return l.bb.Load(typ, slot)
}

func (l *lowerer) lowerBinaryExpr(e *ast.BinaryExpr) *Value {
	x := l.lowerExprValue(e.X)
	y := l.lowerExprValue(e.Y)
	if x == nil || y == nil {
		return ConstInt(I32, 0)
	}

	typ := x.Type
	if typ == nil {
		typ = I32
	}

	switch e.Op {
	case token.ADD:
		return l.bb.BinOp(OpAdd, typ, x, y)
	case token.SUB:
		return l.bb.BinOp(OpSub, typ, x, y)
	case token.MUL:
		return l.bb.BinOp(OpMul, typ, x, y)
	case token.QUO:
		return l.bb.BinOp(OpDiv, typ, x, y)
	case token.REM:
		return l.bb.BinOp(OpRem, typ, x, y)
	case token.AND:
		return l.bb.BinOp(OpAnd, typ, x, y)
	case token.OR:
		return l.bb.BinOp(OpOr, typ, x, y)
	case token.XOR:
		return l.bb.BinOp(OpXor, typ, x, y)
	case token.SHL:
		return l.bb.BinOp(OpShl, typ, x, y)
	case token.SHR:
		return l.bb.BinOp(OpShr, typ, x, y)
	case token.EQL:
		return l.bb.Cmp(CmpEQ, typ, x, y)
	case token.NEQ:
		return l.bb.Cmp(CmpNE, typ, x, y)
	case token.LSS:
		return l.bb.Cmp(CmpLT, typ, x, y)
	case token.LEQ:
		return l.bb.Cmp(CmpLE, typ, x, y)
	case token.GTR:
		return l.bb.Cmp(CmpGT, typ, x, y)
	case token.GEQ:
		return l.bb.Cmp(CmpGE, typ, x, y)
	case token.LAND:
		// Short-circuit AND — for now just AND the bool values
		return l.bb.BinOp(OpAnd, Bool, x, y)
	case token.LOR:
		return l.bb.BinOp(OpOr, Bool, x, y)
	}
	return ConstInt(I32, 0)
}

func (l *lowerer) lowerUnaryExpr(e *ast.UnaryExpr) *Value {
	x := l.lowerExprValue(e.X)
	switch e.Op {
	case token.SUB:
		// Negate: 0 - x
		if x == nil {
			return ConstInt(I32, 0)
		}
		zero := ConstInt(x.Type, 0)
		return l.bb.BinOp(OpSub, x.Type, zero, x)
	case token.NOT:
		if x == nil {
			return ConstBool(false)
		}
		one := ConstInt(Bool, 1)
		return l.bb.BinOp(OpXor, Bool, x, one)
	case token.AND:
		// Address-of: return the slot directly (without loading)
		if ident, ok := e.X.(*ast.Ident); ok {
			if slot := l.env[ident.Name]; slot != nil {
				return slot
			}
		}
		return x
	case token.XOR:
		// Bitwise NOT: ^x = x XOR -1
		if x != nil {
			allOnes := ConstInt(x.Type, -1)
			return l.bb.BinOp(OpXor, x.Type, x, allOnes)
		}
	}
	return x
}

func (l *lowerer) lowerIndexExpr(e *ast.IndexExpr) *Value {
	// Check for string indexing: format[i]
	// If the variable has a __len companion, it's a decomposed string.
	if ident, ok := e.X.(*ast.Ident); ok {
		if _, hasLen := l.env[ident.Name+"__len"]; hasLen {
			// This is a decomposed string or slice — load ptr, GEP, load element
			ptrSlot := l.env[ident.Name]
			if ptrSlot == nil {
				return ConstInt(I32, 0)
			}
			ptr := l.bb.Load(PtrTo(U8), ptrSlot)
			idx := l.lowerExprValue(e.Index)
			if idx == nil {
				return ConstInt(I32, 0)
			}

			// Determine element size from the pointer type
			elemSize := int64(1) // default: byte (string indexing)
			if ptrSlot.Type != nil && ptrSlot.Type.Kind == TyPtr && ptrSlot.Type.Elem != nil {
				elemSize = SizeOf(ptrSlot.Type.Elem)
				if elemSize <= 0 {
					elemSize = 1
				}
			}

			addr := l.bb.GEPSized(ptr, []*Value{idx}, elemSize)

			// For byte-sized elements, load a byte
			if elemSize == 1 {
				return l.bb.Load(U8, addr)
			}
			// For larger elements, return the pointer (caller will extract fields)
			return addr
		}
	}

	base := l.lowerExprValue(e.X)
	idx := l.lowerExprValue(e.Index)
	if base == nil || idx == nil {
		return ConstInt(I32, 0)
	}
	// For a slice, GEP into the data pointer
	ptr := l.bb.GEP(base, []*Value{idx})
	elemType := I32
	if base.Type != nil && base.Type.Kind == TySlice && base.Type.Elem != nil {
		elemType = base.Type.Elem
	}
	return l.bb.Load(elemType, ptr)
}

func (l *lowerer) lowerSelectorExpr(e *ast.SelectorExpr) *Value {
	// Check if X is a package name (not a local variable)
	if ident, ok := e.X.(*ast.Ident); ok {
		if _, isLocal := l.env[ident.Name]; !isLocal {
			// Treat as package-qualified reference (constant or global)
			qualifiedName := ident.Name + "." + e.Sel.Name
			// Look up in allFuncs for function references
			if fn, ok := l.allFuncs[qualifiedName]; ok {
				return &Value{Kind: ValFunc, Type: I64, Name: fn.Name}
			}
			// For package-level constants/variables, return a placeholder
			return ConstInt(I64, 0)
		}
	}

	// Struct field access
	base := l.lowerExprValue(e.X)
	if base == nil {
		return ConstInt(I32, 0)
	}
	// Field access — extract field by index (simplified: always index 0)
	return l.bb.ExtractField(I32, base, 0)
}

func (l *lowerer) lowerCompositeLit(e *ast.CompositeLit) *Value {
	// For struct composite literals, allocate a slot and fill fields
	var typ *Type
	if e.Type != nil {
		typ = l.lowerType(e.Type)
	} else {
		typ = I32
	}
	slot := l.bb.Alloca(typ)
	return slot
}
