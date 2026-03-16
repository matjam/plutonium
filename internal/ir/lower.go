package ir

import (
	"fmt"

	"github.com/matjam/plutonium/internal/ast"
	"github.com/matjam/plutonium/internal/mono"
	"github.com/matjam/plutonium/internal/token"
)

// Lower converts a monomorphization result into a Plutonium IR Program.
func Lower(result *mono.Result, fset *token.FileSet) (*Program, error) {
	l := &lowerer{
		fset:         fset,
		prog:         &Program{},
		allFuncs:     make(map[string]*Function),
		variadicInfo: make(map[string]*variadicFuncInfo),
		namedTypes:   make(map[string]*Type),
	}

	pkg := &IRPackage{Name: "main"}
	l.pkg = pkg

	// Pass 0: Scan type declarations to build named type map.
	for _, file := range result.Files {
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				irType := l.lowerType(ts.Type)
				l.namedTypes[ts.Name.Name] = irType
			}
		}
	}

	// Pass 1: Create all function shells (name + return type) so that
	// forward references and cross-function calls can be resolved.
	// Also detect variadic functions and collect parameter type info.
	for _, fe := range result.Functions {
		retType := l.lowerReturnType(fe.Decl.Type)
		fn := &Function{
			Name:    fe.Name,
			RetType: retType,
			nextID:  1,
		}
		pkg.Functions = append(pkg.Functions, fn)
		l.allFuncs[fe.Name] = fn

		// Scan for variadic and collect fixed param types
		if fe.Decl.Type.Params != nil {
			for i, field := range fe.Decl.Type.Params.List {
				if _, isEllipsis := field.Type.(*ast.Ellipsis); isEllipsis {
					fixedCount := 0
					var fixedTypes []*Type
					for j := 0; j < i; j++ {
						ft := l.lowerType(fe.Decl.Type.Params.List[j].Type)
						n := len(fe.Decl.Type.Params.List[j].Names)
						if n == 0 {
							n = 1
						}
						for k := 0; k < n; k++ {
							fixedTypes = append(fixedTypes, ft)
							fixedCount++
						}
					}
					l.variadicInfo[fe.Name] = &variadicFuncInfo{
						fixedParams:     fixedCount,
						fixedParamTypes: fixedTypes,
					}
					break
				}
			}
		}
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
	fset         *token.FileSet
	prog         *Program
	pkg          *IRPackage
	fn           *Function
	bb           *BasicBlock
	env          map[string]*Value            // name → alloca slot
	structTypes  map[string]*Type             // name → struct IR type (for field name resolution)
	namedTypes   map[string]*Type             // type name → IR struct type (from type declarations)
	packedTypes  map[*Type]*packedTypeInfo    // IR type → packed type metadata
	packedVars   map[string]*packedTypeInfo   // variable name → packed type metadata
	volatileVars map[string]bool              // variable name → true if volatile
	ringVars     map[string]*ringTypeInfo     // variable name → ring type metadata
	bitsAliases  map[string]*bitsLensInfo     // bits lens variable name → lens metadata
	retSlots     []*Value                     // return value slots (for multiple returns)
	allFuncs     map[string]*Function         // all known functions by qualified name
	variadicInfo map[string]*variadicFuncInfo // function name → variadic info
}

// ringTypeInfo stores metadata about a ring[T, N] variable.
type ringTypeInfo struct {
	elemType *Type  // IR type for element T
	capacity int64  // N (must be power of 2)
	mask     int64  // N - 1 (used for AND-masking)
	headSlot *Value // alloca for head counter (uint64)
	tailSlot *Value // alloca for tail counter (uint64)
	dataSlot *Value // alloca for data array ([N]T)
}

// bitsLensInfo stores metadata about a bits[V] lens variable.
type bitsLensInfo struct {
	targetVar  string // name of the underlying variable V
	targetType *Type  // IR type of V (must be integer type)
}

// variadicFuncInfo stores metadata about a variadic function declaration.
type variadicFuncInfo struct {
	fixedParams     int     // number of non-variadic params (logical, before expansion)
	fixedParamTypes []*Type // IR types of fixed params
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
		fset:         l.fset,
		prog:         l.prog,
		pkg:          l.pkg,
		fn:           fn,
		env:          make(map[string]*Value),
		structTypes:  make(map[string]*Type),
		namedTypes:   l.namedTypes,
		packedTypes:  l.packedTypes,
		packedVars:   make(map[string]*packedTypeInfo),
		volatileVars: make(map[string]bool),
		ringVars:     make(map[string]*ringTypeInfo),
		bitsAliases:  make(map[string]*bitsLensInfo),
		allFuncs:     l.allFuncs,
		variadicInfo: l.variadicInfo,
	}

	entry := fn.NewBlock("entry")
	fl.bb = entry

	// Add receiver as first parameter (for methods).
	if d.Recv != nil {
		for _, field := range d.Recv.List {
			paramType := fl.lowerType(field.Type)
			for _, name := range field.Names {
				fl.lowerParam(entry, fn, name.Name, paramType)
				// Track struct type for field access on the receiver
				if paramType.Kind == TyPtr && paramType.Elem != nil && paramType.Elem.Kind == TyStruct {
					fl.structTypes[name.Name] = paramType.Elem
				}
			}
		}
	}

	// Add parameters to env — alloca a slot for each param.
	// Multi-word types (string, slice) are decomposed into separate params.
	if d.Type.Params != nil {
		for _, field := range d.Type.Params.List {
			paramType := fl.lowerType(field.Type)
			for _, name := range field.Names {
				fl.lowerParam(entry, fn, name.Name, paramType)
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

// lowerParam creates IR parameters and alloca slots for a function parameter.
// Multi-word types are decomposed into separate 8-byte parameters.
func (l *lowerer) lowerParam(entry *BasicBlock, fn *Function, name string, paramType *Type) {
	switch paramType.Kind {
	case TyString:
		// String {ptr, len} → two params
		ptrParam := fn.NewParam(name+".ptr", PtrTo(U8))
		lenParam := fn.NewParam(name+".len", I64)
		ptrSlot := entry.Alloca(PtrTo(U8))
		lenSlot := entry.Alloca(I64)
		entry.Store(PtrTo(U8), ptrParam, ptrSlot)
		entry.Store(I64, lenParam, lenSlot)
		l.env[name] = ptrSlot
		l.env[name+"__len"] = lenSlot

	case TySlice:
		// Slice {ptr, len, cap} → three params
		elemPtrType := PtrTo(U8)
		if paramType.Elem != nil {
			elemPtrType = PtrTo(paramType.Elem)
		}
		ptrParam := fn.NewParam(name+".ptr", elemPtrType)
		lenParam := fn.NewParam(name+".len", I64)
		capParam := fn.NewParam(name+".cap", I64)
		ptrSlot := entry.Alloca(elemPtrType)
		lenSlot := entry.Alloca(I64)
		capSlot := entry.Alloca(I64)
		entry.Store(elemPtrType, ptrParam, ptrSlot)
		entry.Store(I64, lenParam, lenSlot)
		entry.Store(I64, capParam, capSlot)
		l.env[name] = ptrSlot
		l.env[name+"__len"] = lenSlot
		l.env[name+"__cap"] = capSlot

	default:
		// Scalar types: one param, one alloca
		param := fn.NewParam(name, paramType)
		slot := entry.Alloca(paramType)
		entry.Store(paramType, param, slot)
		l.env[name] = slot
	}
}

// --- Type lowering ---

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
	case *ast.Ellipsis:
		// ...T in parameter → slice of T (at IR level)
		elem := l.lowerType(e.Elt)
		return SliceOf(elem)
	case *ast.StructType:
		var fields []*Type
		var fieldNames []string
		if e.Fields != nil {
			for _, f := range e.Fields.List {
				ft := l.lowerType(f.Type)
				if len(f.Names) == 0 {
					// Embedded/anonymous field
					fields = append(fields, ft)
					fieldNames = append(fieldNames, "")
				} else {
					for _, name := range f.Names {
						fields = append(fields, ft)
						fieldNames = append(fieldNames, name.Name)
					}
				}
			}
		}
		return &Type{Kind: TyStruct, Fields: fields, FieldNames: fieldNames}
	case *ast.PackedType:
		return l.lowerPackedType(e)
	case *ast.BitsType:
		return l.lowerType(e.AccessType)
	case *ast.BitsGroupType:
		// A bits group is internally Bool — individual 1-bit fields
		return Bool
	case *ast.VolatileType:
		elem := l.lowerType(e.Elem)
		return &Type{Kind: TyVolatile, Elem: elem}
	case *ast.AlignedType:
		// aligned[T, N] lowers to the same IR type as T, but with
		// alignment metadata on the alloca (handled in lowerDeclStmt).
		return l.lowerType(e.Elem)
	case *ast.RingType:
		// ring[T, N] lowers to a struct { head u64, tail u64, data [N]T }
		// The actual layout is managed in lowerDeclStmt; here we return
		// a struct type for the ring buffer.
		elemType := l.lowerType(e.Elem)
		var capacity int64
		if lit, ok := e.Size.(*ast.BasicLit); ok {
			fmt.Sscanf(lit.Value, "%d", &capacity)
		}
		dataType := ArrayOf(capacity, elemType)
		return &Type{
			Kind:       TyStruct,
			Fields:     []*Type{U64, U64, dataType},
			FieldNames: []string{"head", "tail", "data"},
		}
	case *ast.BitsLensType:
		// bits[V] is a zero-cost lens — it has no storage of its own.
		// We return U64 as a placeholder; the actual behavior is handled
		// in lowerDeclStmt which records the alias.
		return U64
	}
	return I32 // fallback
}

// packedTypeInfo stores metadata about a packed type for field access codegen.
type packedTypeInfo struct {
	backingType *Type
	fields      []packedFieldInfo
	needsBSwap  bool // true if endianness mismatches native (x86_64 = LE)
}

type packedFieldInfo struct {
	name       string
	width      int    // bit width
	offset     int    // bit offset from LSB
	accessType *Type  // IR type for reading/writing this field
	groupName  string // non-empty if part of a bits { ... } group
}

func (l *lowerer) lowerPackedType(e *ast.PackedType) *Type {
	backingType := l.lowerType(e.BackingType)

	// Determine if byte-swap is needed. x86_64 is little-endian,
	// so only big-endian annotations require BSWAP.
	needsBSwap := e.ByteOrder == ast.EndianBig

	// Build packed type info for field access
	info := &packedTypeInfo{backingType: backingType, needsBSwap: needsBSwap}
	bitOffset := 0
	var fieldNames []string

	if e.Fields != nil {
		for _, f := range e.Fields.List {
			switch ft := f.Type.(type) {
			case *ast.BitsType:
				width := 0
				if lit, ok := ft.Width.(*ast.BasicLit); ok {
					fmt.Sscanf(lit.Value, "%d", &width)
				}
				accessType := l.lowerType(ft.AccessType)

				for _, name := range f.Names {
					info.fields = append(info.fields, packedFieldInfo{
						name:       name.Name,
						width:      width,
						offset:     bitOffset,
						accessType: accessType,
					})
					fieldNames = append(fieldNames, name.Name)
					bitOffset += width
				}
			case *ast.BitsGroupType:
				// bits { FLAG_A, FLAG_B, ... } — each sub-field is 1-bit bool
				groupName := ""
				if len(f.Names) > 0 {
					groupName = f.Names[0].Name
				}
				// The group name itself is a "field" for namespace purposes
				if groupName != "" {
					fieldNames = append(fieldNames, groupName)
				}
				for _, subName := range ft.Names {
					info.fields = append(info.fields, packedFieldInfo{
						name:       subName.Name,
						width:      1,
						offset:     bitOffset,
						accessType: Bool,
						groupName:  groupName,
					})
					bitOffset++
				}
			}
		}
	}

	// Store the packed type info in the lowerer for later field access
	// Use a struct type with the same field names but the backing type as the only "real" storage
	irType := &Type{
		Kind:       TyStruct,
		Fields:     []*Type{backingType},
		FieldNames: fieldNames,
	}

	// Store packed info keyed by the IR type pointer
	if l.packedTypes == nil {
		l.packedTypes = make(map[*Type]*packedTypeInfo)
	}
	l.packedTypes[irType] = info

	return irType
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
	case "string":
		return Str // fat pointer { ptr *byte, len int64 }
	case "any":
		return Any // tagged union { typeID uint64, data uint64 }
	}
	// Check named types (from type declarations)
	if l.namedTypes != nil {
		if t, ok := l.namedTypes[name]; ok {
			return t
		}
	}
	return I32 // fallback for named types
}

// --- String interning ---

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

// isTypeName returns true if the given name is a built-in type name
// used for type conversions like int32(x).
func isTypeName(name string) bool {
	switch name {
	case "bool", "int8", "int16", "int32", "int64",
		"uint8", "uint16", "uint32", "uint64",
		"byte", "rune", "int", "uint", "uintptr",
		"float32", "float64", "string":
		return true
	}
	return false
}
