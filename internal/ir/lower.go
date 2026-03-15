package ir

import (
	"fmt"
	"strings"

	"github.com/matjam/plutonium/internal/ast"
	"github.com/matjam/plutonium/internal/mono"
	"github.com/matjam/plutonium/internal/token"
)

// Lower converts a monomorphization result into a Plutonium IR Program.
func Lower(result *mono.Result, fset *token.FileSet) (*Program, error) {
	l := &lowerer{
		fset:     fset,
		prog:     &Program{},
		allFuncs: make(map[string]*Function),
	}

	pkg := &IRPackage{Name: "main"}
	l.pkg = pkg

	// Pass 1: Create all function shells (name + return type) so that
	// forward references and cross-function calls can be resolved.
	for _, fe := range result.Functions {
		retType := l.lowerReturnType(fe.Decl.Type)
		fn := &Function{
			Name:    fe.Name,
			RetType: retType,
			nextID:  1,
		}
		pkg.Functions = append(pkg.Functions, fn)
		l.allFuncs[fe.Name] = fn
	}

	// Pass 2: Lower function bodies into the pre-created shells.
	for i, fe := range result.Functions {
		fn := pkg.Functions[i]
		if err := l.lowerFuncBody(fe, fn); err != nil {
			return nil, fmt.Errorf("lowering %s: %w", fe.Name, err)
		}
	}

	l.prog.Packages = append(l.prog.Packages, pkg)
	return l.prog, nil
}

type lowerer struct {
	fset     *token.FileSet
	prog     *Program
	pkg      *IRPackage
	fn       *Function
	bb       *BasicBlock
	env      map[string]*Value    // name → alloca slot
	retSlots []*Value             // return value slots (for multiple returns)
	allFuncs map[string]*Function // all known functions by qualified name
}

// lowerFuncBody lowers the body of a function into a pre-created Function shell.
func (l *lowerer) lowerFuncBody(fe *mono.FuncEntry, fn *Function) error {
	d := fe.Decl

	// If it's an assembly stub (no body), nothing to lower
	if d.Body == nil {
		return nil
	}

	// Create child lowerer for this function
	fl := &lowerer{
		fset:     l.fset,
		prog:     l.prog,
		pkg:      l.pkg,
		fn:       fn,
		env:      make(map[string]*Value),
		allFuncs: l.allFuncs,
	}

	entry := fn.NewBlock("entry")
	fl.bb = entry

	// Add parameters to env — alloca a slot for each param
	if d.Type.Params != nil {
		for _, field := range d.Type.Params.List {
			paramType := fl.lowerType(field.Type)
			for _, name := range field.Names {
				param := fn.NewParam(name.Name, paramType)
				// Alloca for each param so we can take address
				slot := entry.Alloca(paramType)
				entry.Store(paramType, param, slot)
				fl.env[name.Name] = slot
			}
		}
	}

	// Allocate return slots for multiple returns
	if d.Type.Results != nil && len(d.Type.Results.List) > 1 {
		for _, field := range d.Type.Results.List {
			typ := fl.lowerType(field.Type)
			slot := entry.Alloca(typ)
			fl.retSlots = append(fl.retSlots, slot)
			for _, name := range field.Names {
				fl.env[name.Name] = slot
			}
		}
	}

	// Lower the body
	fl.lowerBlock(d.Body)

	// If the last block has no terminator, add RetVoid or Unreachable
	retType := fn.RetType
	if fl.bb.Term == nil {
		if retType == nil || retType.Kind == TyVoid {
			fl.bb.RetVoid()
		} else {
			fl.bb.Unreachable()
		}
	}

	return nil
}

func (l *lowerer) lowerReturnType(ft *ast.FuncType) *Type {
	if ft.Results == nil || ft.Results.NumFields() == 0 {
		return Void
	}
	if ft.Results.NumFields() == 1 {
		return l.lowerType(ft.Results.List[0].Type)
	}
	// Multiple returns: use struct type
	var fields []*Type
	for _, f := range ft.Results.List {
		fields = append(fields, l.lowerType(f.Type))
	}
	return &Type{Kind: TyStruct, Fields: fields}
}

func (l *lowerer) lowerType(expr ast.Expr) *Type {
	switch e := expr.(type) {
	case *ast.Ident:
		return l.lowerIdentType(e.Name)
	case *ast.StarExpr:
		elem := l.lowerType(e.X)
		return PtrTo(elem)
	case *ast.ArrayType:
		elem := l.lowerType(e.Elt)
		if e.Len == nil {
			return SliceOf(elem)
		}
		return ArrayOf(0, elem) // TODO: eval constant length
	case *ast.PointerType:
		elem := l.lowerType(e.X)
		return PtrTo(elem)
	}
	return I32 // fallback
}

func (l *lowerer) lowerIdentType(name string) *Type {
	switch name {
	case "bool":
		return Bool
	case "int8":
		return I8
	case "int16":
		return I16
	case "int32":
		return I32
	case "int64":
		return I64
	case "uint8", "byte":
		return U8
	case "uint16":
		return U16
	case "uint32":
		return U32
	case "uint64":
		return U64
	case "int":
		return I64 // platform int = 64-bit
	case "uint":
		return U64
	case "uintptr":
		return U64
	case "float32":
		return F32
	case "float64":
		return F64
	case "rune":
		return I32
	case "error":
		return I64 // interface type represented as i64 pointer
	}
	return I32 // fallback for named types
}

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
	case *ast.StarExpr:
		ptr := l.lowerExprValue(e.X)
		if ptr != nil {
			typ := val.Type
			if typ == nil {
				typ = I32
			}
			l.bb.Store(typ, val, ptr)
		}
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
			slot := l.bb.Alloca(typ)
			if i < len(vs.Values) {
				val := l.lowerExprValue(vs.Values[i])
				if val != nil {
					l.bb.Store(typ, val, slot)
				}
			}
			l.env[name.Name] = slot
		}
	}
}

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

// internString strips quotes, resolves escape sequences, and interns the
// resulting string into the package's string table.
func (l *lowerer) internString(s string) *StringLit {
	// Strip quotes from the literal value
	unquoted := s
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		unquoted = s[1 : len(s)-1]
	}
	// Handle escape sequences
	unquoted = unescapeString(unquoted)

	// Check if already interned
	for _, lit := range l.pkg.Strings {
		if string(lit.Data) == unquoted {
			return lit
		}
	}

	lit := &StringLit{
		ID:   len(l.pkg.Strings),
		Data: []byte(unquoted),
	}
	l.pkg.Strings = append(l.pkg.Strings, lit)
	return lit
}

// unescapeString handles basic escape sequences: \n, \t, \r, \\, \", \0.
func unescapeString(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n':
				b = append(b, '\n')
				i++
			case 't':
				b = append(b, '\t')
				i++
			case 'r':
				b = append(b, '\r')
				i++
			case '\\':
				b = append(b, '\\')
				i++
			case '"':
				b = append(b, '"')
				i++
			case '0':
				b = append(b, 0)
				i++
			default:
				b = append(b, s[i])
			}
		} else {
			b = append(b, s[i])
		}
	}
	return string(b)
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

func (l *lowerer) lowerCallExpr(e *ast.CallExpr) *Value {
	// Handle type conversions: int32(x), uint64(p), etc.
	if ident, ok := e.Fun.(*ast.Ident); ok {
		if isTypeName(ident.Name) {
			toType := l.lowerIdentType(ident.Name)
			if len(e.Args) == 1 {
				val := l.lowerExprValue(e.Args[0])
				if val != nil {
					fromType := val.Type
					if fromType == nil {
						fromType = I32
					}
					return l.bb.Convert(OpBitcast, fromType, toType, val)
				}
				return ConstInt(toType, 0)
			}
		}
	}

	// Handle generic instantiation calls: Max[int64](args...)
	// The AST represents this as CallExpr{Fun: IndexExpr{X: Ident("Max"), Index: Ident("int64")}}
	if idxExpr, ok := e.Fun.(*ast.IndexExpr); ok {
		if funcIdent, ok := idxExpr.X.(*ast.Ident); ok {
			// Construct the mangled name: Max__int64
			typeArgName := ""
			if typeIdent, ok := idxExpr.Index.(*ast.Ident); ok {
				typeArgName = typeIdent.Name
			}
			mangledName := funcIdent.Name + "__" + typeArgName

			// Lower arguments
			var args []*Value
			for _, arg := range e.Args {
				v := l.lowerExprValue(arg)
				args = append(args, v)
			}

			// Look up the monomorphized function
			fn := l.allFuncs[mangledName]
			if fn == nil {
				// Try without mangling (maybe not generic)
				fn = l.allFuncs[funcIdent.Name]
			}
			if fn == nil {
				// Create external reference
				fn = &Function{Name: mangledName, RetType: I64, nextID: 1}
				l.allFuncs[mangledName] = fn
			}

			return l.bb.Call(fn, fn.RetType, args...)
		}
	}

	// Handle SelectorExpr calls: pkg.Func(args)
	if sel, ok := e.Fun.(*ast.SelectorExpr); ok {
		if pkgIdent, ok := sel.X.(*ast.Ident); ok {
			qualifiedName := pkgIdent.Name + "." + sel.Sel.Name

			// Lower arguments
			var args []*Value
			for _, arg := range e.Args {
				v := l.lowerExprValue(arg)
				args = append(args, v)
			}

			// Look up in known functions
			fn := l.allFuncs[qualifiedName]
			if fn == nil {
				// Create external function declaration (forward reference)
				fn = &Function{
					Name:    qualifiedName,
					RetType: I64, // default; would be better with type info
				}
				l.allFuncs[qualifiedName] = fn
			}

			return l.bb.Call(fn, fn.RetType, args...)
		}
	}

	// Regular function call — evaluate all args first
	var args []*Value
	for _, arg := range e.Args {
		v := l.lowerExprValue(arg)
		args = append(args, v)
	}

	// Look up callee function by name
	if ident, ok := e.Fun.(*ast.Ident); ok {
		// First try exact match
		if fn := l.allFuncs[ident.Name]; fn != nil {
			return l.bb.Call(fn, fn.RetType, args...)
		}
		// For intra-package calls in dependency packages: the current
		// function has a qualified name like "fmt.Println" and the call
		// uses the unqualified name "write". Derive the package prefix
		// from the current function and try a qualified lookup.
		if l.fn != nil && l.fn.Name != "" {
			if dotIdx := strings.LastIndex(l.fn.Name, "."); dotIdx >= 0 {
				pkgPrefix := l.fn.Name[:dotIdx+1]
				qualifiedName := pkgPrefix + ident.Name
				if fn := l.allFuncs[qualifiedName]; fn != nil {
					return l.bb.Call(fn, fn.RetType, args...)
				}
			}
		}
		// Legacy fallback: scan pkg functions
		for _, fn := range l.pkg.Functions {
			if fn.Name == ident.Name {
				return l.bb.Call(fn, fn.RetType, args...)
			}
		}
	}

	// Built-in calls (len, cap, etc.) — simplified
	return ConstInt(I32, 0)
}

func (l *lowerer) lowerIndexExpr(e *ast.IndexExpr) *Value {
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

func isTypeName(name string) bool {
	switch name {
	case "bool", "int8", "int16", "int32", "int64",
		"uint8", "uint16", "uint32", "uint64",
		"byte", "rune", "int", "uint", "uintptr",
		"float32", "float64":
		return true
	}
	return false
}
