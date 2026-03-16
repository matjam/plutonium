package ir

import (
	"strconv"

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
		if len(l.loopStack) > 0 {
			ctx := l.loopStack[len(l.loopStack)-1]
			switch s.Tok {
			case token.BREAK:
				l.bb.Br(ctx.exitBlock)
			case token.CONTINUE:
				l.bb.Br(ctx.postBlock)
			}
		}

	case *ast.DeferStmt:
		if s.Call != nil {
			l.deferStack = append(l.deferStack, s.Call)
		}

	case *ast.SwitchStmt:
		l.lowerSwitch(s)

	case *ast.CaseClause:
		// Handled inside lowerSwitch; no-op here.

	case *ast.EmptyStmt:
		// nothing

	case *ast.RangeStmt:
		l.lowerRange(s)
	}
}

func (l *lowerer) lowerReturn(s *ast.ReturnStmt) {
	if len(s.Results) == 0 {
		l.emitDefers()
		l.emitRAIIFrees(nil)
		l.bb.RetVoid()
		return
	}
	if len(s.Results) == 1 {
		val := l.lowerExprValue(s.Results[0])
		retType := l.fn.RetType
		if retType == nil {
			retType = I32
		}
		// Determine which heap-owned variables are being returned
		// so we skip freeing them (ownership transfers to caller).
		returned := l.returnedVarNames(s.Results)
		l.emitDefers()
		l.emitRAIIFrees(returned)
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
	returned := l.returnedVarNames(s.Results)
	l.emitDefers()
	l.emitRAIIFrees(returned)
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

			// Check if RHS is alloc.New[T]() — track for RAII auto-free
			var allocTypeName string
			var allocSize int64
			if i < len(s.Rhs) {
				if typeArg, ok := isAllocNewCall(s.Rhs[i]); ok {
					irType := l.lowerType(typeArg)
					allocSize = SizeOf(irType)
					if typeIdent, ok := typeArg.(*ast.Ident); ok {
						allocTypeName = typeIdent.Name
					}
				}
			}

			// Check if RHS is a heap-owned variable (pointer copy → refcount increment)
			var copyFromHeapOwned *heapOwnedInfo
			if i < len(s.Rhs) {
				if rhsIdent, ok := s.Rhs[i].(*ast.Ident); ok {
					if info, ok := l.heapOwned[rhsIdent.Name]; ok {
						copyFromHeapOwned = info
					}
				}
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
			// Record heap ownership for RAII auto-free
			if allocSize > 0 {
				l.heapOwned[ident.Name] = &heapOwnedInfo{
					slot:     slot,
					size:     allocSize,
					typeName: allocTypeName,
				}
			}

			// If this is a copy of a heap-owned pointer, emit incRef
			// and track the new variable as heap-owned too.
			if copyFromHeapOwned != nil {
				incRefFn := l.allFuncs["alloc.incRef"]
				if incRefFn == nil {
					incRefFn = &Function{Name: "alloc.incRef", RetType: Void, nextID: 1}
					l.allFuncs["alloc.incRef"] = incRefFn
				}
				ptr := l.bb.Load(typ, slot)
				l.bb.Call(incRefFn, Void, ptr)
				l.heapOwned[ident.Name] = &heapOwnedInfo{
					slot:     slot,
					size:     copyFromHeapOwned.size,
					typeName: copyFromHeapOwned.typeName,
				}
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
					typ := lhsVal.Type
					if typ == nil {
						typ = I32
					}
					if s.Tok == token.AND_NOT_ASSIGN {
						// x &^= y  →  x = x & (^y)
						allOnes := ConstInt(typ, -1)
						notY := l.bb.BinOp(OpXor, typ, rhs, allOnes)
						rhs = l.bb.BinOp(OpAnd, typ, lhsVal, notY)
					} else {
						op := compoundOp(s.Tok)
						rhs = l.bb.BinOp(op, typ, lhsVal, rhs)
					}
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
		// Handle packed group field read for compound assignment: reg.flags.ENABLE
		if sel, ok := e.X.(*ast.SelectorExpr); ok {
			if ident, ok := sel.X.(*ast.Ident); ok {
				if pinfo, ok := l.packedVars[ident.Name]; ok {
					return l.lowerPackedGroupFieldRead(ident.Name, sel.Sel.Name, e.Sel.Name, pinfo)
				}
			}
		}
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
		// For volatile variables, mark the store so it must not be
		// elided, coalesced, or reordered by any future optimizer.
		if l.volatileVars[e.Name] {
			l.bb.StoreVolatile(typ, val, slot)
		} else {
			l.bb.Store(typ, val, slot)
		}
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
		// Check for packed group field write: reg.flags.ENABLE = true
		if sel, ok := e.X.(*ast.SelectorExpr); ok {
			if ident, ok := sel.X.(*ast.Ident); ok {
				if pinfo, ok := l.packedVars[ident.Name]; ok {
					l.lowerPackedGroupFieldWrite(ident.Name, sel.Sel.Name, e.Sel.Name, val, pinfo)
					return
				}
			}
		}
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

	// Body — push loop context so break/continue resolve correctly
	l.bb = bodyBlock
	l.loopStack = append(l.loopStack, &loopContext{exitBlock: exitBlock, postBlock: postBlock})
	l.lowerBlock(s.Body)
	l.loopStack = l.loopStack[:len(l.loopStack)-1]
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
	// Determine if the range expression is a decomposed string/slice
	// (has a __len companion in the environment).
	var basePtr *Value // pointer to the first element
	var lenVal *Value  // number of elements
	var elemSize int64 // size of each element in bytes
	var elemType *Type // IR type of each element

	isDecomposed := false
	if ident, ok := s.X.(*ast.Ident); ok {
		if lenSlot, hasLen := l.env[ident.Name+"__len"]; hasLen {
			isDecomposed = true
			// Decomposed string/slice: load ptr and len from separate slots
			ptrSlot := l.env[ident.Name]
			if ptrSlot != nil {
				basePtr = l.bb.Load(PtrTo(U8), ptrSlot)
			}
			lenVal = l.bb.Load(I64, lenSlot)
			// Determine element type from the pointer type
			elemSize = 1 // default: byte (string)
			elemType = U8
			if ptrSlot != nil && ptrSlot.Type != nil && ptrSlot.Type.Kind == TyPtr && ptrSlot.Type.Elem != nil {
				elemType = ptrSlot.Type.Elem
				sz := SizeOf(elemType)
				if sz > 0 {
					elemSize = sz
				}
			}
		}
	}

	if !isDecomposed {
		// Non-decomposed: evaluate the range value as a struct/slice
		rangeVal := l.lowerExprValue(s.X)
		if rangeVal != nil {
			// For a slice struct, extract ptr (field 0) and len (field 1)
			basePtr = l.bb.ExtractField(PtrTo(U8), rangeVal, 0)
			lenVal = l.bb.ExtractField(I64, rangeVal, 1)
		} else {
			lenVal = ConstInt(I64, 0)
		}
		elemSize = 8
		elemType = I64
	}

	if lenVal == nil {
		lenVal = ConstInt(I64, 0)
	}

	// Create loop with index
	idxSlot := l.bb.Alloca(I64)
	l.bb.Store(I64, ConstInt(I64, 0), idxSlot)

	postBlock := l.fn.NewBlock("range.post")
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
	// Bind value variable
	if s.Value != nil {
		if valIdent, ok := s.Value.(*ast.Ident); ok && valIdent.Name != "_" {
			if basePtr != nil && elemType != nil {
				// Compute address: basePtr + idx * elemSize
				addr := l.bb.GEPSized(basePtr, []*Value{idx}, elemSize)
				// Load the element value
				elemVal := l.bb.Load(elemType, addr)
				valSlot := l.bb.Alloca(elemType)
				l.bb.Store(elemType, elemVal, valSlot)
				l.env[valIdent.Name] = valSlot
			}
		}
	}
	// Push loop context — continue goes to postBlock
	l.loopStack = append(l.loopStack, &loopContext{exitBlock: exitBlock, postBlock: postBlock})
	l.lowerBlock(s.Body)
	l.loopStack = l.loopStack[:len(l.loopStack)-1]
	if l.bb.Term == nil {
		l.bb.Br(postBlock)
	}

	// Post block: increment index and loop back
	l.bb = postBlock
	postIdx := l.bb.Load(I64, idxSlot)
	newIdx := l.bb.BinOp(OpAdd, I64, postIdx, ConstInt(I64, 1))
	l.bb.Store(I64, newIdx, idxSlot)
	l.bb.Br(condBlock)

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

			// Check if this is a ring[T, N] type — allocate separate head, tail, data slots
			if vs.Type != nil {
				if rt, ok := vs.Type.(*ast.RingType); ok {
					elemType := l.lowerType(rt.Elem)
					var capacity int64
					if lit, ok := rt.Size.(*ast.BasicLit); ok {
						capacity, _ = strconv.ParseInt(lit.Value, 10, 64)
					}
					// TODO(v0.2): validate power-of-2 capacity and report diagnostic
					_ = capacity <= 0 || (capacity&(capacity-1)) != 0
					// Allocate head, tail as separate u64 slots
					headSlot := l.bb.Alloca(U64)
					l.bb.Store(U64, ConstInt(U64, 0), headSlot)
					tailSlot := l.bb.Alloca(U64)
					l.bb.Store(U64, ConstInt(U64, 0), tailSlot)
					// Allocate data array: [N]elemType
					dataType := ArrayOf(capacity, elemType)
					dataSlot := l.bb.Alloca(dataType)
					// Store a dummy env entry so the variable is "known"
					l.env[name.Name] = headSlot
					l.ringVars[name.Name] = &ringTypeInfo{
						elemType: elemType,
						capacity: capacity,
						mask:     capacity - 1,
						headSlot: headSlot,
						tailSlot: tailSlot,
						dataSlot: dataSlot,
					}
					continue
				}
			}

			// Check if this is a bits[V] lens — record alias, no allocation needed
			if vs.Type != nil {
				if bt, ok := vs.Type.(*ast.BitsLensType); ok {
					targetName := bt.Target.Name
					// Look up the target variable's type from the environment
					targetSlot := l.env[targetName]
					targetType := U64
					if targetSlot != nil && targetSlot.Type != nil {
						targetType = targetSlot.Type
					}
					l.bitsAliases[name.Name] = &bitsLensInfo{
						targetVar:  targetName,
						targetType: targetType,
					}
					// No allocation — bits lens is zero-cost
					continue
				}
			}

			// Check if this is a volatile type — allocate the element type
			// and track the variable as volatile for load/store marking.
			if typ.Kind == TyVolatile && typ.Elem != nil {
				elemType := typ.Elem
				slot := l.bb.Alloca(elemType)
				// Zero-initialize
				l.bb.Store(elemType, ConstInt(elemType, 0), slot)
				l.env[name.Name] = slot
				l.volatileVars[name.Name] = true
				continue
			}

			// Check if the AST type is aligned[T, N] — allocate with
			// explicit alignment metadata so the codegen rounds up the
			// stack offset to meet the alignment requirement.
			var allocAlign int
			if vs.Type != nil {
				if at, ok := vs.Type.(*ast.AlignedType); ok {
					if lit, ok := at.Alignment.(*ast.BasicLit); ok {
						allocAlign, _ = strconv.Atoi(lit.Value)
					}
				}
			}

			slot := l.bb.AllocaAligned(typ, allocAlign)
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

// --- RAII auto-free support ---

// isAllocNewCall checks if an expression is an alloc.New[T]() call.
// Returns the type argument expression and true if it is, otherwise nil and false.
func isAllocNewCall(expr ast.Expr) (ast.Expr, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	// alloc.New[T]() is parsed as CallExpr with Fun = IndexExpr{X: SelectorExpr{X: "alloc", Sel: "New"}, Index: T}
	idxExpr, ok := call.Fun.(*ast.IndexExpr)
	if !ok {
		return nil, false
	}
	sel, ok := idxExpr.X.(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	pkgIdent, ok := sel.X.(*ast.Ident)
	if !ok {
		return nil, false
	}
	if pkgIdent.Name == "alloc" && sel.Sel.Name == "New" {
		return idxExpr.Index, true
	}
	return nil, false
}

// returnedVarNames extracts variable names from return expressions.
// Used to determine which heap-owned variables are being returned
// (ownership transfer — don't free those).
func (l *lowerer) returnedVarNames(results []ast.Expr) map[string]bool {
	if len(l.heapOwned) == 0 {
		return nil
	}
	names := make(map[string]bool)
	for _, expr := range results {
		switch e := expr.(type) {
		case *ast.Ident:
			if _, ok := l.heapOwned[e.Name]; ok {
				names[e.Name] = true
			}
		}
	}
	return names
}

// emitRAIIFrees emits alloc.decRef calls for all heap-owned variables
// that are not being returned. decRef decrements the refcount and only
// frees when it hits zero. If a type has a Drop() method, it is
// called before decrementing.
func (l *lowerer) emitRAIIFrees(returned map[string]bool) {
	if len(l.heapOwned) == 0 {
		return
	}

	// Get or create the alloc.decRef function reference
	decRefFn := l.allFuncs["alloc.decRef"]
	if decRefFn == nil {
		decRefFn = &Function{Name: "alloc.decRef", RetType: Void, nextID: 1}
		l.allFuncs["alloc.decRef"] = decRefFn
	}

	for varName, info := range l.heapOwned {
		// Skip variables whose ownership is being transferred (returned)
		if returned != nil && returned[varName] {
			continue
		}

		// Load the pointer from the alloca slot
		ptr := l.bb.Load(info.slot.Type, info.slot)

		// If the type has a Drop() method, call it before decrementing
		if info.typeName != "" {
			dropName := info.typeName + ".Drop"
			if dropFn, ok := l.allFuncs[dropName]; ok {
				l.bb.Call(dropFn, Void, ptr)
			}
		}

		// Emit alloc.decRef(ptr, size)
		sizeVal := ConstInt(I64, info.size)
		l.bb.Call(decRefFn, Void, ptr, sizeVal)
	}
}

// emitDefers emits deferred calls in LIFO order (last defer runs first).
func (l *lowerer) emitDefers() {
	for i := len(l.deferStack) - 1; i >= 0; i-- {
		l.lowerExprValue(l.deferStack[i])
	}
}

// lowerSwitch lowers a switch statement as a chain of if-else comparisons.
func (l *lowerer) lowerSwitch(s *ast.SwitchStmt) {
	if s.Init != nil {
		l.lowerStmt(s.Init)
	}

	// Evaluate tag once
	var tag *Value
	if s.Tag != nil {
		tag = l.lowerExprValue(s.Tag)
	}

	mergeBlock := l.fn.NewBlock("switch.merge")

	// Collect case clauses from the body
	if s.Body == nil {
		l.bb.Br(mergeBlock)
		l.bb = mergeBlock
		return
	}
	body := s.Body

	var defaultClause *ast.CaseClause
	for _, stmt := range body.List {
		cc, ok := stmt.(*ast.CaseClause)
		if !ok {
			continue
		}
		if cc.List == nil {
			// default case
			defaultClause = cc
			continue
		}

		// Create blocks for this case
		caseBody := l.fn.NewBlock("switch.case")
		nextCase := l.fn.NewBlock("switch.next")

		// Compare tag to each case value (OR them together)
		for i, expr := range cc.List {
			val := l.lowerExprValue(expr)
			tagType := I32
			if tag != nil && tag.Type != nil {
				tagType = tag.Type
			}
			cmp := l.bb.Cmp(CmpEQ, tagType, tag, val)
			if i == len(cc.List)-1 {
				l.bb.CondBr(cmp, caseBody, nextCase)
			} else {
				// Multiple values in one case: if match, go to body
				matchBlock := l.fn.NewBlock("switch.match")
				l.bb.CondBr(cmp, caseBody, matchBlock)
				l.bb = matchBlock
			}
		}

		// Lower case body
		l.bb = caseBody
		for _, bodyStmt := range cc.Body {
			l.lowerStmt(bodyStmt)
			if l.bb.Term != nil {
				break
			}
		}
		if l.bb.Term == nil {
			l.bb.Br(mergeBlock) // implicit break (no fallthrough)
		}

		l.bb = nextCase
	}

	// Default case
	if defaultClause != nil {
		for _, bodyStmt := range defaultClause.Body {
			l.lowerStmt(bodyStmt)
			if l.bb.Term != nil {
				break
			}
		}
	}
	if l.bb.Term == nil {
		l.bb.Br(mergeBlock)
	}

	l.bb = mergeBlock
}
