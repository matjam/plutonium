package types

// Universe scope contains predeclared identifiers.
var Universe *Scope

// Typ holds the canonical basic type instances.
// Access via Typ[Int32], Typ[Bool], etc.
var Typ = [UnsafePointer + 1]*Basic{}

func init() {
	// Initialize basic types
	basicTypes := []struct {
		kind BasicKind
		name string
	}{
		{Invalid, "invalid"},
		{Bool, "bool"},
		{Int8, "int8"},
		{Int16, "int16"},
		{Int32, "int32"},
		{Int64, "int64"},
		{Uint8, "uint8"},
		{Uint16, "uint16"},
		{Uint32, "uint32"},
		{Uint64, "uint64"},
		{Int, "int"},
		{Uint, "uint"},
		{Uintptr, "uintptr"},
		{Float32, "float32"},
		{Float64, "float64"},
		{Byte, "byte"},
		{Rune, "rune"},
		{String, "string"},
		{UnsafePointer, "unsafe.Pointer"},
	}

	for _, bt := range basicTypes {
		Typ[bt.kind] = &Basic{kind: bt.kind, name: bt.name}
	}

	Universe = NewScope(nil)

	// Add basic types to universe scope (skip Invalid)
	for _, bt := range basicTypes[1:] {
		obj := &Object{Kind: TypObj, Name: bt.name, Type: Typ[bt.kind]}
		Universe.Insert(obj)
	}

	// Add bool constants
	Universe.Insert(&Object{Kind: ConObj, Name: "true", Type: Typ[Bool], Value: true})
	Universe.Insert(&Object{Kind: ConObj, Name: "false", Type: Typ[Bool], Value: false})

	// Add nil
	Universe.Insert(&Object{Kind: NilObj, Name: "nil", Type: nil})

	// Add iota (special, handled at const evaluation)
	Universe.Insert(&Object{Kind: ConObj, Name: "iota", Type: Typ[Int]})

	// Add built-in functions
	builtins := []struct {
		name      string
		id        BuiltinID
		forbidden bool
	}{
		{"len", BuiltinLen, false},
		{"cap", BuiltinCap, false},
		{"copy", BuiltinCopy, false},
		{"append", BuiltinAppend, false},
		{"new", BuiltinNew, true},         // FORBIDDEN
		{"make", BuiltinMake, false},      // partially forbidden - checked at call site
		{"panic", BuiltinPanic, true},     // FORBIDDEN
		{"recover", BuiltinRecover, true}, // FORBIDDEN
		{"delete", BuiltinDelete, true},   // FORBIDDEN (no maps)
		{"close", BuiltinClose, true},     // FORBIDDEN (no channels)
		{"print", BuiltinPrint, false},    // allowed (debug)
		{"println", BuiltinPrintln, false},
		{"real", BuiltinReal, false},
		{"imag", BuiltinImag, false},
		{"complex", BuiltinComplex, false},
	}

	for _, b := range builtins {
		bi := &Builtin{id: b.id, name: b.name, forbidden: b.forbidden}
		Universe.Insert(&Object{Kind: BuiltinObj, Name: b.name, Type: bi})
	}

	// The built-in error interface
	errorInterface := &Interface{
		methods: []*Func{
			{name: "Error", sig: &Signature{
				results: []*Var{{name: "_", typ: Typ[String]}},
			}},
		},
	}
	Universe.Insert(&Object{Kind: TypObj, Name: "error", Type: errorInterface})

	// Add "any" as alias for interface{} — rejected during type checking
	Universe.Insert(&Object{Kind: TypObj, Name: "any", Type: &Interface{}})
}

// BuiltinID identifies a built-in function.
type BuiltinID int

const (
	BuiltinLen BuiltinID = iota
	BuiltinCap
	BuiltinCopy
	BuiltinAppend
	BuiltinNew
	BuiltinMake
	BuiltinPanic
	BuiltinRecover
	BuiltinDelete
	BuiltinClose
	BuiltinPrint
	BuiltinPrintln
	BuiltinReal
	BuiltinImag
	BuiltinComplex
)

// Builtin represents a built-in function type.
type Builtin struct {
	id        BuiltinID
	name      string
	forbidden bool // true if this built-in is forbidden in Plutonium
}

func (b *Builtin) typeNode()        {}
func (b *Builtin) String() string   { return "builtin " + b.name }
func (b *Builtin) Underlying() Type { return b }
func (b *Builtin) ID() BuiltinID    { return b.id }
func (b *Builtin) Forbidden() bool  { return b.forbidden }
