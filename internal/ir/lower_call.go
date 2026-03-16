package ir

import (
	"strings"

	"github.com/matjam/plutonium/internal/ast"
)

// --- Call expression lowering ---

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

	// Handle len() builtin
	if ident, ok := e.Fun.(*ast.Ident); ok && ident.Name == "len" {
		if len(e.Args) == 1 {
			return l.lowerLen(e.Args[0])
		}
	}

	// Handle generic instantiation calls: Max[int64](args...) or
	// unsafe.Sizeof[T](), unsafe.As[T](ptr), etc.
	if idxExpr, ok := e.Fun.(*ast.IndexExpr); ok {
		// Check for unsafe package intrinsics: unsafe.Sizeof[T](), unsafe.As[T](ptr), etc.
		// Also check for alloc package generics: alloc.New[T]().
		if sel, ok := idxExpr.X.(*ast.SelectorExpr); ok {
			if pkgIdent, ok := sel.X.(*ast.Ident); ok && pkgIdent.Name == "unsafe" {
				return l.lowerUnsafeGenericCall(sel.Sel.Name, idxExpr.Index, e)
			}
			if pkgIdent, ok := sel.X.(*ast.Ident); ok && pkgIdent.Name == "alloc" {
				return l.lowerAllocGenericCall(sel.Sel.Name, idxExpr.Index, e)
			}
		}

		if funcIdent, ok := idxExpr.X.(*ast.Ident); ok {
			typeArgName := ""
			if typeIdent, ok := idxExpr.Index.(*ast.Ident); ok {
				typeArgName = typeIdent.Name
			}
			mangledName := funcIdent.Name + "__" + typeArgName

			var args []*Value
			for _, arg := range e.Args {
				v := l.lowerExprValue(arg)
				args = append(args, v)
			}

			fn := l.allFuncs[mangledName]
			if fn == nil {
				fn = l.allFuncs[funcIdent.Name]
			}
			if fn == nil {
				fn = &Function{Name: mangledName, RetType: I64, nextID: 1}
				l.allFuncs[mangledName] = fn
			}

			return l.bb.Call(fn, fn.RetType, args...)
		}
	}

	// Handle SelectorExpr calls: pkg.Func(args) or receiver.Method(args)
	if sel, ok := e.Fun.(*ast.SelectorExpr); ok {
		if ident, ok := sel.X.(*ast.Ident); ok {
			// Handle unsafe package non-generic intrinsics:
			// unsafe.FromUintptr(n) and unsafe.ToUintptr(p)
			if ident.Name == "unsafe" {
				return l.lowerUnsafeSimpleCall(sel.Sel.Name, e)
			}

			// Handle alloc package non-generic intrinsics:
			// alloc.Free(ptr)
			if ident.Name == "alloc" {
				return l.lowerAllocSimpleCall(sel.Sel.Name, e)
			}

			// Check for ring method calls: r.Push(val), r.Pop(), etc.
			if rinfo, ok := l.ringVars[ident.Name]; ok {
				return l.lowerRingMethodCall(ident.Name, sel.Sel.Name, e, rinfo)
			}

			// Check for bits lens method calls: b.Set(3), b.Clear(3), etc.
			if binfo, ok := l.bitsAliases[ident.Name]; ok {
				return l.lowerBitsLensMethodCall(ident.Name, sel.Sel.Name, e, binfo)
			}

			// Check if the identifier is a local variable (method call)
			// vs. a package name (package-qualified call).
			if _, isLocal := l.env[ident.Name]; isLocal {
				return l.lowerMethodCall(ident, sel.Sel.Name, e)
			}

			qualifiedName := ident.Name + "." + sel.Sel.Name

			fn := l.allFuncs[qualifiedName]
			if fn == nil {
				fn = &Function{
					Name:    qualifiedName,
					RetType: I64,
				}
				l.allFuncs[qualifiedName] = fn
			}

			// Check if the callee is variadic
			if info, ok := l.variadicInfo[qualifiedName]; ok {
				return l.lowerVariadicCall(fn, e, info)
			}

			// Non-variadic: lower arguments normally
			var args []*Value
			for _, arg := range e.Args {
				v := l.lowerExprValue(arg)
				args = append(args, v)
			}

			return l.bb.Call(fn, fn.RetType, args...)
		}
	}

	// Look up callee function by name
	if ident, ok := e.Fun.(*ast.Ident); ok {
		var callee *Function
		var calleeName string

		if fn := l.allFuncs[ident.Name]; fn != nil {
			callee = fn
			calleeName = ident.Name
		} else if l.fn != nil && l.fn.Name != "" {
			if dotIdx := strings.LastIndex(l.fn.Name, "."); dotIdx >= 0 {
				pkgPrefix := l.fn.Name[:dotIdx+1]
				qualifiedName := pkgPrefix + ident.Name
				if fn := l.allFuncs[qualifiedName]; fn != nil {
					callee = fn
					calleeName = qualifiedName
				}
			}
		}
		if callee == nil {
			for _, fn := range l.pkg.Functions {
				if fn.Name == ident.Name {
					callee = fn
					calleeName = ident.Name
					break
				}
			}
		}

		if callee != nil {
			if info, ok := l.variadicInfo[calleeName]; ok {
				return l.lowerVariadicCall(callee, e, info)
			}
			var args []*Value
			for _, arg := range e.Args {
				v := l.lowerExprValue(arg)
				args = append(args, v)
			}
			return l.bb.Call(callee, callee.RetType, args...)
		}
	}

	// Fallback
	for _, arg := range e.Args {
		l.lowerExprValue(arg)
	}
	return ConstInt(I32, 0)
}

// --- Method call lowering ---

// lowerMethodCall handles a call like c.Inc() where c is a local variable.
// It resolves the method name to TypeName.MethodName, prepends the receiver
// as the first argument, and emits the call.
func (l *lowerer) lowerMethodCall(recv *ast.Ident, methodName string, e *ast.CallExpr) *Value {
	// Look up the receiver's struct type to determine the qualified method name.
	recvTypeName := ""
	isPointerRecv := false

	if stype, ok := l.structTypes[recv.Name]; ok {
		// The struct type is tracked — use FieldNames source to get the type name.
		// We need to find the type name from the allFuncs registry.
		// Try to find a matching method in allFuncs.
		for fnName := range l.allFuncs {
			// Check for "TypeName.methodName" pattern
			dotIdx := strings.LastIndex(fnName, ".")
			if dotIdx < 0 {
				continue
			}
			if fnName[dotIdx+1:] == methodName {
				recvTypeName = fnName[:dotIdx]
				break
			}
			_ = stype
		}
	}

	// If we couldn't resolve from structTypes, scan allFuncs for any match
	if recvTypeName == "" {
		for fnName := range l.allFuncs {
			dotIdx := strings.LastIndex(fnName, ".")
			if dotIdx < 0 {
				continue
			}
			if fnName[dotIdx+1:] == methodName {
				recvTypeName = fnName[:dotIdx]
				break
			}
		}
	}

	qualifiedName := recvTypeName + "." + methodName
	fn := l.allFuncs[qualifiedName]
	if fn == nil {
		// Fallback: create a stub
		fn = &Function{Name: qualifiedName, RetType: I64}
		l.allFuncs[qualifiedName] = fn
	}

	// Build args: receiver first, then call args
	var args []*Value

	// Get the receiver value. For pointer receivers, pass the address (&c).
	// For value receivers, pass the value directly.
	// Check if the method function's first param is a pointer type.
	if len(fn.Params) > 0 && fn.Params[0].Type != nil && fn.Params[0].Type.Kind == TyPtr {
		isPointerRecv = true
	}

	if isPointerRecv {
		// Pass address of the receiver variable
		slot := l.env[recv.Name]
		if slot != nil {
			args = append(args, slot)
		}
	} else {
		// Pass the receiver value
		val := l.lowerExprValue(recv)
		args = append(args, val)
	}

	// Append regular arguments
	for _, arg := range e.Args {
		v := l.lowerExprValue(arg)
		args = append(args, v)
	}

	return l.bb.Call(fn, fn.RetType, args...)
}

// --- len() builtin ---

// lowerLen handles the len() builtin for strings and slices.
func (l *lowerer) lowerLen(arg ast.Expr) *Value {
	// For identifiers, check for decomposed string/slice __len companion
	if ident, ok := arg.(*ast.Ident); ok {
		if lenSlot, ok := l.env[ident.Name+"__len"]; ok {
			return l.bb.Load(I64, lenSlot)
		}
	}
	// Fallback: string literal — length is in IntVal
	val := l.lowerExprValue(arg)
	if val != nil && val.Kind == ValGlobal && val.IntVal > 0 {
		return ConstInt(I64, val.IntVal)
	}
	return ConstInt(I64, 0)
}

// --- Argument expansion ---

// expandStringArg evaluates an expression and appends it to the args slice.
// If the expression is a string (literal or decomposed variable), it expands
// into (ptr, len) — two separate arguments.
func (l *lowerer) expandStringArg(expr ast.Expr, args *[]*Value) {
	// Check if argument is a decomposed string variable (has __len but NOT __cap)
	if ident, ok := expr.(*ast.Ident); ok {
		_, hasLen := l.env[ident.Name+"__len"]
		_, hasCap := l.env[ident.Name+"__cap"]
		if hasLen && !hasCap {
			// Decomposed string (not slice): pass ptr and len as separate args
			ptrSlot := l.env[ident.Name]
			ptr := l.bb.Load(PtrTo(U8), ptrSlot)
			lenSlot := l.env[ident.Name+"__len"]
			length := l.bb.Load(I64, lenSlot)
			*args = append(*args, ptr, length)
			return
		}
	}

	val := l.lowerExprValue(expr)
	if val == nil {
		*args = append(*args, ConstInt(I64, 0))
		return
	}

	// String literal: expand into (ptr, len)
	if val.Kind == ValGlobal && val.Type != nil && val.Type.Kind == TyPtr &&
		val.Type.Elem != nil && val.Type.Elem.Kind == TyU8 && val.IntVal > 0 {
		*args = append(*args, val, ConstInt(I64, val.IntVal))
		return
	}

	*args = append(*args, val)
}

// --- Variadic call lowering ---

// lowerVariadicCall handles calling a variadic function. It:
// 1. Evaluates fixed arguments (expanding multi-word types)
// 2. Packs variadic args into any structs {typeID, data}
// 3. Allocates a stack array of any values
// 4. Builds a slice header (ptr, len, cap)
// 5. Passes fixed args + slice ptr + slice len + slice cap
func (l *lowerer) lowerVariadicCall(fn *Function, e *ast.CallExpr, info *variadicFuncInfo) *Value {
	// Evaluate fixed arguments, expanding multi-word types
	var fixedArgs []*Value
	for i := 0; i < info.fixedParams && i < len(e.Args); i++ {
		// Check if this fixed param is a string type
		if i < len(info.fixedParamTypes) && info.fixedParamTypes[i] != nil &&
			info.fixedParamTypes[i].Kind == TyString {
			l.expandStringArg(e.Args[i], &fixedArgs)
		} else {
			v := l.lowerExprValue(e.Args[i])
			fixedArgs = append(fixedArgs, v)
		}
	}

	// Evaluate variadic arguments
	type argWithType struct {
		val    *Value
		irType *Type
	}
	var varArgs []argWithType
	for i := info.fixedParams; i < len(e.Args); i++ {
		v := l.lowerExprValue(e.Args[i])
		typ := I64
		if v != nil && v.Type != nil {
			typ = v.Type
		}
		varArgs = append(varArgs, argWithType{val: v, irType: typ})
	}

	nVariadic := int64(len(varArgs))

	if nVariadic == 0 {
		nullPtr := ConstInt(I64, 0)
		zeroLen := ConstInt(I64, 0)
		zeroCap := ConstInt(I64, 0)
		allArgs := append(fixedArgs, nullPtr, zeroLen, zeroCap)
		return l.bb.Call(fn, fn.RetType, allArgs...)
	}

	// Allocate stack array of [N]any — each any is 16 bytes (2 x i64)
	arrayType := ArrayOf(nVariadic, Any)
	arraySlot := l.bb.Alloca(arrayType)

	// Pack each variadic argument into its any slot
	for i, va := range varArgs {
		typeID := AnyTypeIDFor(va.irType)

		var dataVal *Value
		if va.irType.Kind == TyString || (va.irType.Kind == TyPtr && va.irType.Elem != nil && va.irType.Elem.Kind == TyU8) {
			if va.val != nil && va.val.Kind == ValGlobal {
				hdrSlot := l.bb.Alloca(Str)
				l.bb.InsertField(I64, hdrSlot, 0, va.val)
				lenVal := ConstInt(I64, va.val.IntVal)
				l.bb.InsertField(I64, hdrSlot, 1, lenVal)
				dataVal = hdrSlot
			} else if va.val != nil {
				dataVal = va.val
			} else {
				dataVal = ConstInt(I64, 0)
			}
		} else {
			if va.val != nil {
				dataVal = va.val
			} else {
				dataVal = ConstInt(I64, 0)
			}
		}

		offset := int64(i) * 16
		typeIDConst := ConstInt(I64, typeID)

		l.bb.InsertField(I64, arraySlot, int(offset/8), typeIDConst)
		l.bb.InsertField(I64, arraySlot, int(offset/8)+1, dataVal)
	}

	slicePtr := arraySlot
	sliceLen := ConstInt(I64, nVariadic)
	sliceCap := ConstInt(I64, nVariadic)

	allArgs := append(fixedArgs, slicePtr, sliceLen, sliceCap)
	return l.bb.Call(fn, fn.RetType, allArgs...)
}

// --- unsafe package intrinsics ---

// lowerUnsafeGenericCall handles generic unsafe intrinsics:
//   - unsafe.Sizeof[T]()  → compile-time integer constant (size in bytes)
//   - unsafe.Alignof[T]() → compile-time integer constant (alignment in bytes)
//   - unsafe.As[T](ptr)   → no-op cast: unsafe.Pointer → *T (same register)
//   - unsafe.From[T](ptr) → no-op cast: *T → unsafe.Pointer (same register)
func (l *lowerer) lowerUnsafeGenericCall(funcName string, typeArg ast.Expr, e *ast.CallExpr) *Value {
	switch funcName {
	case "Sizeof":
		irType := l.lowerType(typeArg)
		return ConstInt(I64, SizeOf(irType))

	case "Alignof":
		irType := l.lowerType(typeArg)
		// Natural alignment: same as size for scalars, capped at 8 for x86_64
		align := SizeOf(irType)
		if align > 8 {
			align = 8
		}
		return ConstInt(I64, align)

	case "As":
		// unsafe.As[T](ptr) — no-op cast: return the argument unchanged.
		// unsafe.Pointer and *T are both 8-byte pointers on 64-bit.
		if len(e.Args) == 1 {
			return l.lowerExprValue(e.Args[0])
		}
		return ConstInt(I64, 0)

	case "From":
		// unsafe.From[T](ptr) — no-op cast: return the argument unchanged.
		// *T and unsafe.Pointer are both 8-byte pointers on 64-bit.
		if len(e.Args) == 1 {
			return l.lowerExprValue(e.Args[0])
		}
		return ConstInt(I64, 0)
	}

	// Unknown unsafe generic function — fall through
	return ConstInt(I64, 0)
}

// lowerUnsafeSimpleCall handles non-generic unsafe intrinsics:
//   - unsafe.FromUintptr(n)  → no-op: uintptr → unsafe.Pointer (same register)
//   - unsafe.ToUintptr(p)    → no-op: unsafe.Pointer → uintptr (same register)
func (l *lowerer) lowerUnsafeSimpleCall(funcName string, e *ast.CallExpr) *Value {
	switch funcName {
	case "FromUintptr":
		// uintptr → unsafe.Pointer — no-op (both 8 bytes on 64-bit)
		if len(e.Args) == 1 {
			return l.lowerExprValue(e.Args[0])
		}
		return ConstInt(I64, 0)

	case "ToUintptr":
		// unsafe.Pointer → uintptr — no-op (both 8 bytes on 64-bit)
		if len(e.Args) == 1 {
			return l.lowerExprValue(e.Args[0])
		}
		return ConstInt(I64, 0)
	}

	// Unknown unsafe simple function — fall through
	return ConstInt(I64, 0)
}

// --- alloc package intrinsics ---

// lowerAllocGenericCall handles generic alloc intrinsics:
//   - alloc.New[T]() → calls alloc.newRaw(sizeof(T)), returns *T
func (l *lowerer) lowerAllocGenericCall(funcName string, typeArg ast.Expr, e *ast.CallExpr) *Value {
	switch funcName {
	case "New":
		// alloc.New[T]() — allocate sizeof(T) bytes and return *T
		irType := l.lowerType(typeArg)
		size := SizeOf(irType)

		// Call alloc.newRaw(size)
		fn := l.allFuncs["alloc.newRaw"]
		if fn == nil {
			fn = &Function{Name: "alloc.newRaw", RetType: PtrTo(U8), nextID: 1}
			l.allFuncs["alloc.newRaw"] = fn
		}

		sizeVal := ConstInt(I64, size)
		result := l.bb.Call(fn, PtrTo(irType), sizeVal)
		return result
	}

	// Unknown alloc generic function — fall through
	return ConstInt(I64, 0)
}

// lowerAllocSimpleCall handles non-generic alloc intrinsics:
//   - alloc.Free(ptr) → infers sizeof(T) from ptr's *T type, calls alloc.freeRaw(ptr, sizeof(T))
func (l *lowerer) lowerAllocSimpleCall(funcName string, e *ast.CallExpr) *Value {
	switch funcName {
	case "Free":
		// alloc.Free(ptr) — free sizeof(T) bytes
		if len(e.Args) != 1 {
			return ConstInt(I64, 0)
		}

		ptr := l.lowerExprValue(e.Args[0])
		if ptr == nil {
			return ConstInt(I64, 0)
		}

		// Infer size from the pointer's element type
		size := int64(8) // default to 8 bytes (int64-sized)
		if ptr.Type != nil && ptr.Type.Kind == TyPtr && ptr.Type.Elem != nil {
			size = SizeOf(ptr.Type.Elem)
		}

		// Call alloc.freeRaw(ptr, size)
		fn := l.allFuncs["alloc.freeRaw"]
		if fn == nil {
			fn = &Function{Name: "alloc.freeRaw", RetType: Void, nextID: 1}
			l.allFuncs["alloc.freeRaw"] = fn
		}

		sizeVal := ConstInt(I64, size)
		l.bb.Call(fn, Void, ptr, sizeVal)
		return ConstInt(I64, 0) // Free returns nothing
	}

	// Unknown alloc simple function — fall through
	return ConstInt(I64, 0)
}

// --- ring[T, N] method calls ---

// lowerRingMethodCall handles method calls on ring[T, N] variables:
//   - r.Push(val) → store val at data[head & mask], increment head; returns bool (1=success, 0=full)
//   - r.Pop()     → load data[tail & mask], increment tail; returns val (0 if empty)
//   - r.Peek()    → load data[tail & mask] without consuming; returns val (0 if empty)
//   - r.Len()     → head - tail
//   - r.Full()    → (head - tail) == capacity
//   - r.Empty()   → head == tail
//   - r.Reset()   → head = tail = 0
//
// Memory layout (flat byte array):
//
//	bytes  0-7:  head (uint64)
//	bytes  8-15: tail (uint64)
//	bytes 16+:   data[N] (N elements of T)
func (l *lowerer) lowerRingMethodCall(varName, method string, e *ast.CallExpr, rinfo *ringTypeInfo) *Value {
	headSlot := rinfo.headSlot
	tailSlot := rinfo.tailSlot
	dataSlot := rinfo.dataSlot

	if headSlot == nil || tailSlot == nil || dataSlot == nil {
		return ConstInt(I64, 0)
	}

	// Simple load/store for head and tail (direct stack slot access)
	loadHead := func() *Value {
		return l.bb.Load(U64, headSlot)
	}
	loadTail := func() *Value {
		return l.bb.Load(U64, tailSlot)
	}
	storeHead := func(v *Value) {
		l.bb.Store(U64, v, headSlot)
	}
	storeTail := func(v *Value) {
		l.bb.Store(U64, v, tailSlot)
	}
	// Helper: get pointer to data[idx] using GEP on the data array slot
	elemSize := SizeOf(rinfo.elemType)
	dataElemPtr := func(idx *Value) *Value {
		return l.bb.GEPSized(dataSlot, []*Value{idx}, elemSize)
	}

	maskVal := ConstInt(U64, rinfo.mask)
	capVal := ConstInt(U64, rinfo.capacity)

	switch method {
	case "Push":
		// Push with full-check: if head-tail==N return 0; else store and return 1
		if len(e.Args) < 1 {
			return ConstInt(I64, 0)
		}
		val := l.lowerExprValue(e.Args[0])

		// Result slot for the return value (1=success, 0=full)
		resultSlot := l.bb.Alloca(I64)
		l.bb.Store(I64, ConstInt(I64, 0), resultSlot)

		head := loadHead()
		tail := loadTail()
		diff := l.bb.BinOp(OpSub, U64, head, tail)
		isFull := l.bb.Cmp(CmpEQ, U64, diff, capVal)

		pushBlock := l.fn.NewBlock("ring.push")
		pushDone := l.fn.NewBlock("ring.push.done")
		l.bb.CondBr(isFull, pushDone, pushBlock)

		// Push body: store val, increment head, set result=1
		l.bb = pushBlock
		headForIdx := l.bb.Load(U64, headSlot)
		idx := l.bb.BinOp(OpAnd, U64, headForIdx, maskVal)
		elemPtr := dataElemPtr(idx)
		l.bb.Store(rinfo.elemType, val, elemPtr)
		// Reload head after the store to avoid register clobbering
		headForInc := l.bb.Load(U64, headSlot)
		newHead := l.bb.BinOp(OpAdd, U64, headForInc, ConstInt(U64, 1))
		storeHead(newHead)
		l.bb.Store(I64, ConstInt(I64, 1), resultSlot)
		l.bb.Br(pushDone)

		l.bb = pushDone
		return l.bb.Load(I64, resultSlot)

	case "Pop":
		// Pop with empty-check: if head==tail return 0; else load, increment tail, return val
		resultSlot := l.bb.Alloca(rinfo.elemType)
		l.bb.Store(rinfo.elemType, ConstInt(rinfo.elemType, 0), resultSlot)

		head := loadHead()
		tail := loadTail()
		isEmpty := l.bb.Cmp(CmpEQ, U64, head, tail)

		popBlock := l.fn.NewBlock("ring.pop")
		popDone := l.fn.NewBlock("ring.pop.done")
		l.bb.CondBr(isEmpty, popDone, popBlock)

		// Pop body: load val, increment tail
		l.bb = popBlock
		tailForIdx := l.bb.Load(U64, tailSlot)
		idx := l.bb.BinOp(OpAnd, U64, tailForIdx, maskVal)
		elemPtr := dataElemPtr(idx)
		val := l.bb.Load(rinfo.elemType, elemPtr)
		l.bb.Store(rinfo.elemType, val, resultSlot)
		// Reload tail after the load/store to avoid register clobbering
		tailForInc := l.bb.Load(U64, tailSlot)
		newTail := l.bb.BinOp(OpAdd, U64, tailForInc, ConstInt(U64, 1))
		storeTail(newTail)
		l.bb.Br(popDone)

		l.bb = popDone
		return l.bb.Load(rinfo.elemType, resultSlot)

	case "Peek":
		// Simplified v0.1: no empty check (caller uses Empty() if needed)
		// val = data[tail & mask]; return val (don't increment tail)
		tail := loadTail()
		idx := l.bb.BinOp(OpAnd, U64, tail, maskVal)
		elemPtr := dataElemPtr(idx)
		return l.bb.Load(rinfo.elemType, elemPtr)

	case "Len":
		head := loadHead()
		tail := loadTail()
		return l.bb.BinOp(OpSub, U64, head, tail)

	case "Full":
		head := loadHead()
		tail := loadTail()
		diff := l.bb.BinOp(OpSub, U64, head, tail)
		return l.bb.Cmp(CmpEQ, U64, diff, capVal)

	case "Empty":
		head := loadHead()
		tail := loadTail()
		return l.bb.Cmp(CmpEQ, U64, head, tail)

	case "Reset":
		storeHead(ConstInt(U64, 0))
		storeTail(ConstInt(U64, 0))
		return ConstInt(I64, 0)
	}

	return ConstInt(I64, 0)
}

// --- bits[V] lens method calls ---

// lowerBitsLensMethodCall handles method calls on bits[V] lens variables:
//   - b.Set(n)      → V |= (1 << n)
//   - b.Clear(n)    → V &= ^(1 << n)
//   - b.Test(n)     → (V >> n) & 1
//   - b.Toggle(n)   → V ^= (1 << n)
//   - b.PopCount()  → popcount(V) (software fallback)
func (l *lowerer) lowerBitsLensMethodCall(lensName, method string, e *ast.CallExpr, binfo *bitsLensInfo) *Value {
	targetSlot := l.env[binfo.targetVar]
	if targetSlot == nil {
		return ConstInt(I64, 0)
	}
	targetType := binfo.targetType

	switch method {
	case "Set":
		// V |= (1 << n)
		if len(e.Args) < 1 {
			return ConstInt(I64, 0)
		}
		n := l.lowerExprValue(e.Args[0])
		current := l.bb.Load(targetType, targetSlot)
		one := ConstInt(targetType, 1)
		mask := l.bb.BinOp(OpShl, targetType, one, n)
		result := l.bb.BinOp(OpOr, targetType, current, mask)
		l.bb.Store(targetType, result, targetSlot)
		return ConstInt(I64, 0)

	case "Clear":
		// V &= ^(1 << n)
		if len(e.Args) < 1 {
			return ConstInt(I64, 0)
		}
		n := l.lowerExprValue(e.Args[0])
		current := l.bb.Load(targetType, targetSlot)
		one := ConstInt(targetType, 1)
		mask := l.bb.BinOp(OpShl, targetType, one, n)
		invMask := l.bb.BinOp(OpXor, targetType, mask, ConstInt(targetType, -1))
		result := l.bb.BinOp(OpAnd, targetType, current, invMask)
		l.bb.Store(targetType, result, targetSlot)
		return ConstInt(I64, 0)

	case "Test":
		// (V >> n) & 1
		if len(e.Args) < 1 {
			return ConstInt(I64, 0)
		}
		n := l.lowerExprValue(e.Args[0])
		current := l.bb.Load(targetType, targetSlot)
		shifted := l.bb.BinOp(OpShr, targetType, current, n)
		return l.bb.BinOp(OpAnd, targetType, shifted, ConstInt(targetType, 1))

	case "Toggle":
		// V ^= (1 << n)
		if len(e.Args) < 1 {
			return ConstInt(I64, 0)
		}
		n := l.lowerExprValue(e.Args[0])
		current := l.bb.Load(targetType, targetSlot)
		one := ConstInt(targetType, 1)
		mask := l.bb.BinOp(OpShl, targetType, one, n)
		result := l.bb.BinOp(OpXor, targetType, current, mask)
		l.bb.Store(targetType, result, targetSlot)
		return ConstInt(I64, 0)

	case "PopCount":
		// Software popcount using Brian Kernighan's algorithm:
		// count = 0; while (v) { v &= v-1; count++; }
		// For v0.1, we'll generate a loop.
		current := l.bb.Load(targetType, targetSlot)
		countSlot := l.bb.Alloca(U64)
		l.bb.Store(U64, ConstInt(U64, 0), countSlot)
		tempSlot := l.bb.Alloca(targetType)
		l.bb.Store(targetType, current, tempSlot)

		condBlock := l.fn.NewBlock("popcount.cond")
		bodyBlock := l.fn.NewBlock("popcount.body")
		exitBlock := l.fn.NewBlock("popcount.exit")

		l.bb.Br(condBlock)

		// Condition: temp != 0
		l.bb = condBlock
		temp := l.bb.Load(targetType, tempSlot)
		isNonZero := l.bb.Cmp(CmpNE, targetType, temp, ConstInt(targetType, 0))
		l.bb.CondBr(isNonZero, bodyBlock, exitBlock)

		// Body: temp &= temp - 1; count++
		l.bb = bodyBlock
		temp2 := l.bb.Load(targetType, tempSlot)
		minus1 := l.bb.BinOp(OpSub, targetType, temp2, ConstInt(targetType, 1))
		newTemp := l.bb.BinOp(OpAnd, targetType, temp2, minus1)
		l.bb.Store(targetType, newTemp, tempSlot)
		cnt := l.bb.Load(U64, countSlot)
		newCnt := l.bb.BinOp(OpAdd, U64, cnt, ConstInt(U64, 1))
		l.bb.Store(U64, newCnt, countSlot)
		l.bb.Br(condBlock)

		l.bb = exitBlock
		return l.bb.Load(U64, countSlot)
	}

	return ConstInt(I64, 0)
}
