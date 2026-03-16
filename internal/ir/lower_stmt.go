package ir

import (
	"github.com/matjam/plutonium/internal/ast"
	"github.com/matjam/plutonium/internal/token"
)

// --- Statement lowering ---

func (l *lowerer) lowerBlock(block *ast.BlockStmt) {
	for _, stmt := range block.List {
		l.lowerStmt(stmt)
		// Stop emitting into this block once we have a terminator
		if l.bb.Term != nil {
			break
		}
	}
}

func (l *lowerer) lowerStmt(stmt ast.Stmt) {
	switch s := stmt.(type) {
	case *ast.ReturnStmt:
		l.lowerReturn(s)

	case *ast.AssignStmt:
		l.lowerAssign(s)

	case *ast.ExprStmt:
		l.lowerExpr(s.X)

	case *ast.IncDecStmt:
		l.lowerIncDec(s)

	case *ast.IfStmt:
		l.lowerIf(s)

	case *ast.ForStmt:
		l.lowerFor(s)

	case *ast.BlockStmt:
		l.lowerBlock(s)

	case *ast.DeclStmt:
		l.lowerDeclStmt(s)

	case *ast.BranchStmt:
		// break/continue/goto — TODO for now skip

	case *ast.DeferStmt:
		// defer — TODO for now skip

	case *ast.EmptyStmt:
		// nothing

	case *ast.RangeStmt:
		l.lowerRange(s)
	}
}

func (l *lowerer) lowerReturn(s *ast.ReturnStmt) {
	if len(s.Results) == 0 {
		l.bb.RetVoid()
		return
	}
	if len(s.Results) == 1 {
		val := l.lowerExprValue(s.Results[0])
		retType := l.fn.RetType
		if retType == nil {
			retType = I32
		}
		l.bb.Ret(retType, val)
		return
	}
	// Multiple returns: store into slots, then return void (simplified for v0.1)
	for i, expr := range s.Results {
		if i < len(l.retSlots) {
			val := l.lowerExprValue(expr)
			slotType := l.retSlots[i].Type
			if slotType == nil {
				slotType = I32
			}
			l.bb.Store(slotType, val, l.retSlots[i])
		}
	}
	l.bb.RetVoid()
}

func (l *lowerer) lowerAssign(s *ast.AssignStmt) {
	if s.Tok == token.DEFINE {
		// Short variable declaration
		for i, lhs := range s.Lhs {
			ident, ok := lhs.(*ast.Ident)
			if !ok || ident.Name == "_" {
				continue
			}
			var rhs *Value
			if i < len(s.Rhs) {
				rhs = l.lowerExprValue(s.Rhs[i])
			}
			// Determine type from RHS
			var typ *Type
			if rhs != nil && rhs.Type != nil {
				typ = rhs.Type
			} else {
				typ = I32
			}
			slot := l.bb.Alloca(typ)
			if rhs != nil {
				l.bb.Store(typ, rhs, slot)
			}
			l.env[ident.Name] = slot
			// Track struct types for field access resolution
			if typ.Kind == TyStruct && len(typ.FieldNames) > 0 {
				l.structTypes[ident.Name] = typ
			}
		}
	} else {
		// Regular assignment (=, +=, etc.)
		for i, lhs := range s.Lhs {
			if i >= len(s.Rhs) {
				break
			}
			rhs := l.lowerExprValue(s.Rhs[i])

			if s.Tok != token.ASSIGN {
				// Compound assignment: +=, -=, etc.
				lhsVal := l.loadLHS(lhs)
				if lhsVal != nil && rhs != nil {
					op := compoundOp(s.Tok)
					typ := lhsVal.Type
					if typ == nil {
						typ = I32
					}
					rhs = l.bb.BinOp(op, typ, lhsVal, rhs)
				}
			}
			l.storeLHS(lhs, rhs)
		}
	}
}

func (l *lowerer) loadLHS(expr ast.Expr) *Value {
	switch e := expr.(type) {
	case *ast.Ident:
		if slot, ok := l.env[e.Name]; ok {
			typ := slot.Type
			if typ == nil {
				typ = I32
			}
			return l.bb.Load(typ, slot)
		}
	case *ast.StarExpr:
		ptr := l.lowerExprValue(e.X)
		if ptr != nil && ptr.Type != nil {
			return l.bb.Load(ptr.Type, ptr)
		}
	case *ast.SelectorExpr:
		// Handle packed field read for compound assignment
		if ident, ok := e.X.(*ast.Ident); ok {
			if pinfo, ok := l.packedVars[ident.Name]; ok {
				return l.lowerPackedFieldRead(ident.Name, e.Sel.Name, pinfo)
			}
		}
		return l.lowerSelectorExpr(e)
	}
	return nil
}

func (l *lowerer) storeLHS(expr ast.Expr, val *Value) {
	if val == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.Ident:
		if e.Name == "_" {
			return
		}
		slot := l.env[e.Name]
		if slot == nil {
			// Auto-declare
			typ := val.Type
			if typ == nil {
				typ = I32
			}
			slot = l.bb.Alloca(typ)
			l.env[e.Name] = slot
		}
		typ := val.Type
		if typ == nil {
			typ = I32
		}
		l.bb.Store(typ, val, slot)
		// Track struct types for field access resolution on regular assignment
		if typ.Kind == TyStruct && len(typ.FieldNames) > 0 {
			l.structTypes[e.Name] = typ
		}
	case *ast.StarExpr:
		ptr := l.lowerExprValue(e.X)
		if ptr != nil {
			typ := val.Type
			if typ == nil {
				typ = I32
			}
			l.bb.Store(typ, val, ptr)
		}
	case *ast.SelectorExpr:
		// Check for packed type field write
		if ident, ok := e.X.(*ast.Ident); ok {
			if pinfo, ok := l.packedVars[ident.Name]; ok {
				l.lowerPackedFieldWrite(ident.Name, e.Sel.Name, val, pinfo)
				return
			}
		}
		// Regular struct field store
		base := l.lowerExprValue(e.X)
		if base == nil {
			return
		}
		fieldIdx := 0
		if ident, ok := e.X.(*ast.Ident); ok {
			if stype, ok := l.structTypes[ident.Name]; ok && stype.Kind == TyStruct {
				for i, name := range stype.FieldNames {
					if name == e.Sel.Name {
						fieldIdx = i
						break
					}
				}
			}
		}
		fieldType := val.Type
		if fieldType == nil {
			fieldType = I32
		}
		l.bb.InsertField(fieldType, base, fieldIdx, val)
	}
}

func compoundOp(tok token.Token) OpCode {
	switch tok {
	case token.ADD_ASSIGN:
		return OpAdd
	case token.SUB_ASSIGN:
		return OpSub
	case token.MUL_ASSIGN:
		return OpMul
	case token.QUO_ASSIGN:
		return OpDiv
	case token.REM_ASSIGN:
		return OpRem
	case token.AND_ASSIGN:
		return OpAnd
	case token.OR_ASSIGN:
		return OpOr
	case token.XOR_ASSIGN:
		return OpXor
	case token.SHL_ASSIGN:
		return OpShl
	case token.SHR_ASSIGN:
		return OpShr
	}
	return OpAdd
}

func (l *lowerer) lowerIncDec(s *ast.IncDecStmt) {
	val := l.loadLHS(s.X)
	if val == nil {
		return
	}
	typ := val.Type
	if typ == nil {
		typ = I32
	}
	one := ConstInt(typ, 1)
	var result *Value
	if s.Tok == token.INC {
		result = l.bb.BinOp(OpAdd, typ, val, one)
	} else {
		result = l.bb.BinOp(OpSub, typ, val, one)
	}
	l.storeLHS(s.X, result)
}

func (l *lowerer) lowerIf(s *ast.IfStmt) {
	if s.Init != nil {
		l.lowerStmt(s.Init)
	}

	cond := l.lowerExprValue(s.Cond)

	thenBlock := l.fn.NewBlock("if.then")
	mergeBlock := l.fn.NewBlock("if.merge")

	var elseBlock *BasicBlock
	if s.Else != nil {
		elseBlock = l.fn.NewBlock("if.else")
		l.bb.CondBr(cond, thenBlock, elseBlock)
	} else {
		l.bb.CondBr(cond, thenBlock, mergeBlock)
	}

	// Then block
	savedBB := l.bb
	_ = savedBB
	l.bb = thenBlock
	l.lowerBlock(s.Body)
	if l.bb.Term == nil {
		l.bb.Br(mergeBlock)
	}

	// Else block
	if s.Else != nil {
		l.bb = elseBlock
		l.lowerStmt(s.Else)
		if l.bb.Term == nil {
			l.bb.Br(mergeBlock)
		}
	}

	l.bb = mergeBlock
}

func (l *lowerer) lowerFor(s *ast.ForStmt) {
	if s.Init != nil {
		l.lowerStmt(s.Init)
	}

	condBlock := l.fn.NewBlock("for.cond")
	bodyBlock := l.fn.NewBlock("for.body")
	postBlock := l.fn.NewBlock("for.post")
	exitBlock := l.fn.NewBlock("for.exit")

	l.bb.Br(condBlock)

	// Condition
	l.bb = condBlock
	if s.Cond != nil {
		cond := l.lowerExprValue(s.Cond)
		l.bb.CondBr(cond, bodyBlock, exitBlock)
	} else {
		l.bb.Br(bodyBlock) // infinite loop
	}

	// Body
	l.bb = bodyBlock
	l.lowerBlock(s.Body)
	if l.bb.Term == nil {
		l.bb.Br(postBlock)
	}

	// Post
	l.bb = postBlock
	if s.Post != nil {
		l.lowerStmt(s.Post)
	}
	if l.bb.Term == nil {
		l.bb.Br(condBlock)
	}

	l.bb = exitBlock
}

func (l *lowerer) lowerRange(s *ast.RangeStmt) {
	// Simplified: lower the range value, create loop blocks
	rangeVal := l.lowerExprValue(s.X)

	// Get length
	var lenVal *Value
	if rangeVal != nil {
		// For a slice, extract the len field
		lenVal = l.bb.ExtractField(I64, rangeVal, 1)
	} else {
		lenVal = ConstInt(I64, 0)
	}

	// Create loop with index
	idxSlot := l.bb.Alloca(I64)
	l.bb.Store(I64, ConstInt(I64, 0), idxSlot)

	condBlock := l.fn.NewBlock("range.cond")
	bodyBlock := l.fn.NewBlock("range.body")
	exitBlock := l.fn.NewBlock("range.exit")

	l.bb.Br(condBlock)

	l.bb = condBlock
	idx := l.bb.Load(I64, idxSlot)
	cond := l.bb.Cmp(CmpLT, I64, idx, lenVal)
	l.bb.CondBr(cond, bodyBlock, exitBlock)

	l.bb = bodyBlock
	// Bind key variable
	if s.Key != nil {
		if keyIdent, ok := s.Key.(*ast.Ident); ok && keyIdent.Name != "_" {
			keySlot := l.bb.Alloca(I64)
			l.bb.Store(I64, idx, keySlot)
			l.env[keyIdent.Name] = keySlot
		}
	}
	l.lowerBlock(s.Body)
	if l.bb.Term == nil {
		// Increment index
		newIdx := l.bb.BinOp(OpAdd, I64, idx, ConstInt(I64, 1))
		l.bb.Store(I64, newIdx, idxSlot)
		l.bb.Br(condBlock)
	}

	l.bb = exitBlock
}

func (l *lowerer) lowerDeclStmt(s *ast.DeclStmt) {
	gd, ok := s.Decl.(*ast.GenDecl)
	if !ok {
		return
	}
	for _, spec := range gd.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for i, name := range vs.Names {
			if name.Name == "_" {
				continue
			}
			var typ *Type
			if vs.Type != nil {
				typ = l.lowerType(vs.Type)
			} else {
				typ = I32
			}

			// Check if this is a packed type — allocate backing type and register packed info
			if pinfo, ok := l.packedTypes[typ]; ok {
				backingType := pinfo.backingType
				slot := l.bb.Alloca(backingType)
				// Zero-initialize the backing value
				l.bb.Store(backingType, ConstInt(backingType, 0), slot)
				l.env[name.Name] = slot
				l.packedVars[name.Name] = pinfo
				continue
			}

			slot := l.bb.Alloca(typ)
			if i < len(vs.Values) {
				val := l.lowerExprValue(vs.Values[i])
				if val != nil {
					storeType := typ
					// If the value has a more specific type (e.g. struct from composite lit),
					// prefer it for both the store and struct type tracking.
					if val.Type != nil && val.Type.Kind == TyStruct && len(val.Type.FieldNames) > 0 {
						storeType = val.Type
						l.structTypes[name.Name] = val.Type
					}
					l.bb.Store(storeType, val, slot)
				}
			}
			l.env[name.Name] = slot
		}
	}
}
