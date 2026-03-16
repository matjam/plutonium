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
	// Handle boolean constants
	if e.Name == "true" {
		return ConstInt(I64, 1)
	}
	if e.Name == "false" {
		return ConstInt(I64, 0)
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
	// For volatile variables, mark the load instruction so it must not be
	// elided, coalesced, or reordered by any future optimizer.
	if l.volatileVars[e.Name] {
		return l.bb.LoadVolatile(typ, slot)
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

	// Check for packed type field access (direct field: reg.mode)
	if ident, ok := e.X.(*ast.Ident); ok {
		if pinfo, ok := l.packedVars[ident.Name]; ok {
			return l.lowerPackedFieldRead(ident.Name, e.Sel.Name, pinfo)
		}
	}

	// Check for double-selector packed group access: reg.flags.ENABLE
	// e.X is SelectorExpr{X: reg, Sel: flags}, e.Sel is ENABLE
	if sel, ok := e.X.(*ast.SelectorExpr); ok {
		if ident, ok := sel.X.(*ast.Ident); ok {
			if pinfo, ok := l.packedVars[ident.Name]; ok {
				// sel.Sel.Name is the group name, e.Sel.Name is the sub-field
				return l.lowerPackedGroupFieldRead(ident.Name, sel.Sel.Name, e.Sel.Name, pinfo)
			}
		}
	}

	// Struct field access
	base := l.lowerExprValue(e.X)
	if base == nil {
		return ConstInt(I32, 0)
	}

	// Resolve field name to index using the struct type info.
	// NOTE(v0.1): If the struct type is not tracked in structTypes (e.g. the variable
	// was assigned through a code path we don't yet handle), fieldIdx defaults to 0.
	// This is an intentional fallback for the v0.1 compiler — full named type resolution
	// will replace this with proper error reporting in a future task.
	fieldIdx := 0
	fieldType := I32
	if ident, ok := e.X.(*ast.Ident); ok {
		if stype, ok := l.structTypes[ident.Name]; ok && stype.Kind == TyStruct {
			for i, name := range stype.FieldNames {
				if name == e.Sel.Name {
					fieldIdx = i
					if i < len(stype.Fields) {
						fieldType = stype.Fields[i]
					}
					break
				}
			}
		}
	}
	return l.bb.ExtractField(fieldType, base, fieldIdx)
}

// lowerPackedFieldRead generates IR to read a bitfield from a packed variable.
// Read: (backing >> bitOffset) & ((1 << width) - 1)
// If the packed type has a byte-swap annotation, the load is marked with NeedsBSwap.
func (l *lowerer) lowerPackedFieldRead(varName, fieldName string, pinfo *packedTypeInfo) *Value {
	// Find the field
	for _, f := range pinfo.fields {
		if f.name == fieldName {
			// Load the backing value from the alloca slot
			slot := l.env[varName]
			if slot == nil {
				return ConstInt(I64, 0)
			}
			backingType := pinfo.backingType
			backing := l.bb.LoadBSwap(backingType, slot, pinfo.needsBSwap)

			// Shift right by offset
			if f.offset > 0 {
				shiftAmt := ConstInt(backingType, int64(f.offset))
				backing = l.bb.BinOp(OpShr, backingType, backing, shiftAmt)
			}

			// Mask to width bits: & ((1 << width) - 1)
			mask := int64((1 << f.width) - 1)
			maskVal := ConstInt(backingType, mask)
			result := l.bb.BinOp(OpAnd, backingType, backing, maskVal)

			return result
		}
	}
	return ConstInt(I64, 0)
}

// lowerPackedGroupFieldRead generates IR to read a sub-field within a bits group.
// It finds the sub-field by matching both the group name and the sub-field name.
func (l *lowerer) lowerPackedGroupFieldRead(varName, groupName, subFieldName string, pinfo *packedTypeInfo) *Value {
	for _, f := range pinfo.fields {
		if f.groupName == groupName && f.name == subFieldName {
			slot := l.env[varName]
			if slot == nil {
				return ConstInt(I64, 0)
			}
			backingType := pinfo.backingType
			backing := l.bb.LoadBSwap(backingType, slot, pinfo.needsBSwap)

			if f.offset > 0 {
				shiftAmt := ConstInt(backingType, int64(f.offset))
				backing = l.bb.BinOp(OpShr, backingType, backing, shiftAmt)
			}

			mask := int64((1 << f.width) - 1)
			maskVal := ConstInt(backingType, mask)
			result := l.bb.BinOp(OpAnd, backingType, backing, maskVal)

			return result
		}
	}
	return ConstInt(I64, 0)
}

// lowerPackedGroupFieldWrite generates IR to write a sub-field within a bits group.
func (l *lowerer) lowerPackedGroupFieldWrite(varName, groupName, subFieldName string, val *Value, pinfo *packedTypeInfo) {
	for _, f := range pinfo.fields {
		if f.groupName == groupName && f.name == subFieldName {
			slot := l.env[varName]
			if slot == nil {
				return
			}
			backingType := pinfo.backingType

			backing := l.bb.LoadBSwap(backingType, slot, pinfo.needsBSwap)

			mask := int64((1 << f.width) - 1)
			maskVal := ConstInt(backingType, mask)
			maskedVal := l.bb.BinOp(OpAnd, backingType, val, maskVal)

			var shiftedVal *Value
			if f.offset > 0 {
				shiftAmt := ConstInt(backingType, int64(f.offset))
				shiftedVal = l.bb.BinOp(OpShl, backingType, maskedVal, shiftAmt)
			} else {
				shiftedVal = maskedVal
			}

			clearMask := int64(^(mask << f.offset))
			clearMaskVal := ConstInt(backingType, clearMask)
			cleared := l.bb.BinOp(OpAnd, backingType, backing, clearMaskVal)

			result := l.bb.BinOp(OpOr, backingType, cleared, shiftedVal)

			l.bb.StoreBSwap(backingType, result, slot, pinfo.needsBSwap)
			return
		}
	}
}

// lowerPackedFieldWrite generates IR to write a bitfield in a packed variable.
// Write: backing = (backing & ~(mask << offset)) | ((val & mask) << offset)
// If the packed type has a byte-swap annotation, LoadBSwap/StoreBSwap handle it.
func (l *lowerer) lowerPackedFieldWrite(varName, fieldName string, val *Value, pinfo *packedTypeInfo) {
	for _, f := range pinfo.fields {
		if f.name == fieldName {
			slot := l.env[varName]
			if slot == nil {
				return
			}
			backingType := pinfo.backingType

			// Load current backing value (with BSWAP if needed)
			backing := l.bb.LoadBSwap(backingType, slot, pinfo.needsBSwap)

			// Mask the value to width bits: val & mask
			mask := int64((1 << f.width) - 1)
			maskVal := ConstInt(backingType, mask)
			maskedVal := l.bb.BinOp(OpAnd, backingType, val, maskVal)

			// Shift the masked value left by offset: maskedVal << offset
			var shiftedVal *Value
			if f.offset > 0 {
				shiftAmt := ConstInt(backingType, int64(f.offset))
				shiftedVal = l.bb.BinOp(OpShl, backingType, maskedVal, shiftAmt)
			} else {
				shiftedVal = maskedVal
			}

			// Clear the target bits in backing: backing & ~(mask << offset)
			clearMask := int64(^(mask << f.offset))
			clearMaskVal := ConstInt(backingType, clearMask)
			cleared := l.bb.BinOp(OpAnd, backingType, backing, clearMaskVal)

			// Combine: cleared | shiftedVal
			result := l.bb.BinOp(OpOr, backingType, cleared, shiftedVal)

			// Store back (with BSWAP if needed)
			l.bb.StoreBSwap(backingType, result, slot, pinfo.needsBSwap)
			return
		}
	}
}

func (l *lowerer) lowerCompositeLit(e *ast.CompositeLit) *Value {
	// For struct composite literals, allocate a slot and fill fields
	var typ *Type
	if e.Type != nil {
		typ = l.lowerType(e.Type)
	} else {
		typ = I32
	}

	// If the type is still I32 (fallback from an unresolved named type)
	// but we have key-value elements, build the struct type from the elements
	// and populate the fields in a single pass to avoid double evaluation.
	// Each element is checked individually — only KeyValue pairs with an Ident
	// key contribute to the struct type, keeping Fields and FieldNames in sync.
	if typ == I32 && len(e.Elts) > 0 {
		var fields []*Type
		var fieldNames []string
		hasKV := false
		for _, elt := range e.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			ident, ok := kv.Key.(*ast.Ident)
			if !ok {
				continue
			}
			hasKV = true
			fieldNames = append(fieldNames, ident.Name)
			// Infer type from the value expression without lowering
			fields = append(fields, l.inferExprType(kv.Value))
		}
		if hasKV {
			typ = &Type{Kind: TyStruct, Fields: fields, FieldNames: fieldNames}
		}
	}

	slot := l.bb.Alloca(typ)

	// Populate struct fields from key-value elements
	if typ.Kind == TyStruct && len(e.Elts) > 0 {
		for i, elt := range e.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			// Find the field index by name
			fieldIdx := i
			if ident, ok := kv.Key.(*ast.Ident); ok {
				for j, name := range typ.FieldNames {
					if name == ident.Name {
						fieldIdx = j
						break
					}
				}
			}
			val := l.lowerExprValue(kv.Value)
			if val != nil {
				ft := I32
				if fieldIdx < len(typ.Fields) {
					ft = typ.Fields[fieldIdx]
				}
				l.bb.InsertField(ft, slot, fieldIdx, val)
			}
		}
	}

	return slot
}

// inferExprType infers the IR type of an expression without emitting instructions.
// Used during struct type construction to avoid double-lowering values.
func (l *lowerer) inferExprType(expr ast.Expr) *Type {
	switch e := expr.(type) {
	case *ast.BasicLit:
		switch e.Kind {
		case token.INT:
			return I64
		case token.FLOAT:
			return F64
		case token.STRING:
			return PtrTo(U8)
		case token.CHAR:
			return I32
		}
	case *ast.Ident:
		if slot, ok := l.env[e.Name]; ok && slot.Type != nil {
			return slot.Type
		}
	case *ast.CallExpr:
		// Type conversions like int32(x)
		if ident, ok := e.Fun.(*ast.Ident); ok {
			if isTypeName(ident.Name) {
				return l.lowerIdentType(ident.Name)
			}
		}
	}
	return I32
}
