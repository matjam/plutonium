package errors

// Error codes for the rules Plutonium adds to Go's, checked by the type
// checker. They are numbered from 2000 so that they never collide with the
// codes of go/types; the compiler shows a code as "E" and four digits
// (E2001). Each has an explanation in internal/diag (catalog_pluto.go),
// and TestCatalogComplete fails for a code without one.
//
// Add new codes at the end of a group; never renumber one.
const (
	_ Code = 2000 + iota

	// MapNotPermitted: a map type, which needs a garbage-collected hash
	// table.
	MapNotPermitted
	// ChanNotPermitted: a channel type or operation, without a scheduler.
	ChanNotPermitted
	// GoNotPermitted: a go statement, without a scheduler.
	GoNotPermitted
	// RecoverNotPermitted: recover, which needs stack unwinding.
	RecoverNotPermitted
	// ReflectNotPermitted: an import of reflect.
	ReflectNotPermitted
	// ComplexNotPermitted: a value of complex type.
	ComplexNotPermitted
	// MissingAllocatorType: an allocator argument to new or make when
	// pluto/rt, which declares the Allocator type, cannot be found.
	MissingAllocatorType
	// ErrorsNewNotConstant: errors.New with a message that is not a
	// constant.
	ErrorsNewNotConstant
	// BadMainSignature: func main with parameters, or with results other
	// than a single int64.
	BadMainSignature
	// InvalidUTF8String: a string literal that is not valid UTF-8, which
	// no string may hold.
	InvalidUTF8String
)

// Packed types, bit fields, volatile, aligned and ring.
const (
	_ Code = 2100 + iota

	// BadPackedBacking: a packed type's backing type is not an integer,
	// a byte array or []byte.
	BadPackedBacking
	// PackedTooLarge: a packed type's fields need more bits than its
	// backing type holds.
	PackedTooLarge
	// BadBitField: a bits[T, N] field whose type or width is wrong.
	BadBitField
	// BadPackedField: a packed field of a type that has no fixed bit
	// layout.
	BadPackedField
	// BadPackedLiteral: a packed literal that does not name its fields.
	BadPackedLiteral
	// WrongWrapperArgs: volatile, aligned or ring with the wrong number of
	// arguments.
	WrongWrapperArgs
	// NotPowerOfTwo: the alignment of aligned or the size of ring is not a
	// power of two.
	NotPowerOfTwo
	// BadVolatileElem: volatile[T] with T other than an integer, bool or
	// pointer.
	BadVolatileElem
	// BadWrapperSize: the constant argument of aligned or ring is not a
	// positive integer constant.
	BadWrapperSize
	// RingLiteralNotEmpty: a ring literal with elements.
	RingLiteralNotEmpty
)

// Default parameter values.
const (
	_ Code = 2200 + iota

	// MisplacedDefault: a default value on a receiver, a result, or a
	// parameter of a function type, function literal or interface method.
	MisplacedDefault
	// VariadicDefault: a default value in a variadic function.
	VariadicDefault
	// MissingDefault: a parameter without a default after one with a
	// default.
	MissingDefault
	// SharedDefault: a default value after several parameter names.
	SharedDefault
	// DefaultRefersToParam: a default value that uses a parameter of the
	// function.
	DefaultRefersToParam
)

// Enums.
const (
	_ Code = 2300 + iota

	// MisplacedEnum: an enum type outside a type definition.
	MisplacedEnum
	// GenericEnum: an enum type with type parameters.
	GenericEnum
	// BadEnumBase: an enum whose underlying type is not an integer type.
	BadEnumBase
	// EmptyEnum: an enum without members.
	EmptyEnum
	// BlankEnumMember: an enum member named _.
	BlankEnumMember
	// DuplicateEnumValue: two members of an enum with the same value.
	DuplicateEnumValue
	// EnumWithoutZero: an enum with no member whose value is 0.
	EnumWithoutZero
	// EnumOverflow: an enum member's value does not fit its type.
	EnumOverflow
	// BadEnumValue: an enum member's value is not a constant integer.
	BadEnumValue
	// EnumSelfReference: an enum used in its own declaration.
	EnumSelfReference
	// UnexportedEnumMember: an unexported member of another package's
	// enum.
	UnexportedEnumMember
	// MissingEnumCase: a switch on an enum value without a default case
	// that leaves out members.
	MissingEnumCase
	// BadEnumConversion: a conversion to an enum from a type that is not
	// an integer, or from a constant that is not a member's value.
	BadEnumConversion
	// UncheckedEnumConversion: a conversion of a non-constant integer to an
	// enum without the comma-ok form.
	UncheckedEnumConversion
	// NotEnumType: an enum operation on a type that is not an enum.
	NotEnumType
	// EnumArithmetic: arithmetic or a shift on an enum value.
	EnumArithmetic
	// RangeOverEnumValue: range over an enum value instead of its type.
	RangeOverEnumValue
)

// Unions.
const (
	_ Code = 2400 + iota

	// MisplacedUnion: a union type outside a type definition.
	MisplacedUnion
	// GenericPackedUnion: a packed union with type parameters.
	GenericPackedUnion
	// EmptyUnion: a union without variants.
	EmptyUnion
	// BadVariantDecl: a union variant without a name, named _, with a tag,
	// or sharing a case clause of a packed union with another variant.
	BadVariantDecl
	// TooManyVariants: a union with more variants than its tag can number.
	TooManyVariants
	// UnionOwnerPayload: a variant payload that holds an owned, shared or
	// synced pointer, which needs the ownership checker.
	UnionOwnerPayload
	// BadUnionTag: a packed union without a backing type, or without
	// exactly one bool, integer or enum tag field before its variants.
	BadUnionTag
	// BadVariantCase: a tag value of a packed union variant that is not a
	// constant, does not fit the tag, or selects another variant too.
	BadVariantCase
	// VariantNotType: a union variant used as a type.
	VariantNotType
	// UnknownVariant: a variant name that the union does not have, or a
	// variant literal on a type that is not a union.
	UnknownVariant
	// UnexportedVariant: an unexported variant of another package's union.
	UnexportedVariant
	// UnionLiteral: a composite literal of a union type itself, of a
	// variant without a payload, or one that sets a packed union's tag.
	UnionLiteral
	// NotAVariant: a case of a type switch on a union, or the type of an
	// assertion on one, that does not name one of its variants.
	NotAVariant
	// MissingVariantCase: a type switch on a union without a default case
	// that leaves out variants.
	MissingVariantCase
	// PackedUnionNoDefault: a type switch on a packed union without a
	// default case whose cases do not cover every value of its tag.
	PackedUnionNoDefault
)
