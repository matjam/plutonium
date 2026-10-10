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
	// 2002 and 2003 were ChanNotPermitted and GoNotPermitted, from before
	// the compiler supported channels and go statements.
	_
	_
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
	// NotSendable: a value that crosses puroutines (a go statement's
	// argument or receiver, or a channel's element type) of a type that
	// holds a shared[T].
	NotSendable
	// FrameViewSent: a view of the current frame (&x of a local, a slice
	// of a local array, &T{...}) passed to a go statement or sent on a
	// channel.
	FrameViewSent
	// InvalidUTF8String: a string literal that is not valid UTF-8, which
	// no string may hold.
	InvalidUTF8String
	// AppendNotPermitted: a call of append, which in Go grows a slice by
	// allocating behind the program's back; a Plutonium slice cannot grow.
	AppendNotPermitted
	// MakeSliceNotPermitted: make of a slice type, whose array nothing
	// would own; vec.Make returns an owner.
	MakeSliceNotPermitted
	// BytesToStringNotPermitted: string(b) of a byte slice, which would
	// replace the bytes that are not valid UTF-8; strings.FromUTF8 refuses
	// them and strings.FromUTF8Lossy replaces them by name.
	BytesToStringNotPermitted
	// StringToSliceNotPermitted: []byte(s) or []rune(s) of a string, whose
	// copy nothing would own; strings.Bytes and strings.Runes return an
	// owner, and copy fills a caller's buffer.
	StringToSliceNotPermitted
	// IntToStringNotPermitted: string(i) of an integer that is not a rune
	// or an untyped constant, which makes the character with that code
	// where the program usually wants the digits.
	IntToStringNotPermitted
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

// Ownership (language spec section 6.4). Ownership and lifetime errors are
// E3000-E3999; the type checker's, about where owner may and must appear,
// start at 3200, and the others are in internal/diag/codes.go.
const (
	_ Code = 3200 + iota

	// DropNotOwner: a Drop method on a type not declared owner.
	DropNotOwner
	// MustBeOwner: a type with an owner field, an embedded owner, an
	// array of owners, an owning interface embedded, or a definition from
	// an owner, that is not declared owner.
	MustBeOwner
	// UnnamedOwnerStruct: a struct type literal with an owner field.
	UnnamedOwnerStruct
	// BadOwnerKind: owner on a kind of type that cannot own, such as a
	// slice or an enum.
	BadOwnerKind
	// OwnerAlias: owner in an alias declaration.
	OwnerAlias
	// BadDropSignature: a Drop method with parameters or results.
	BadDropSignature
	// OwnerTypeArg: an owner type argument for a type parameter whose
	// constraint is not marked owner.
	OwnerTypeArg
	// BadTakeArg: take with an argument that is not a pointer.
	BadTakeArg
	// BadUnsafeDropArg: unsafe.Drop with an argument that is not a
	// pointer.
	BadUnsafeDropArg
	// BoxOfInterface: owned, shared or synced of an interface type, which
	// an owning interface replaces.
	BoxOfInterface
	// BadWeakElem: weak[P] with P other than shared[T] or synced[T].
	BadWeakElem
	// OwnerConversion: a conversion of a value into an owner type outside
	// the package that declares it, or of a view into a box.
	OwnerConversion
)

// The indexing interface: index expressions on library containers.
const (
	_ Code = 2450 + iota

	// MissingIndexMethod: an index expression on a container that lacks
	// the method this use of it calls (Index, SetIndex or IndexOk).
	MissingIndexMethod
	// BadIndexMethod: an indexing method with the wrong signature.
	BadIndexMethod
	// IndexMethodMismatch: indexing methods of one type that disagree on
	// the key or element type.
	IndexMethodMismatch
	// ContainerIndexAddress: &x[k] where x is a container.
	ContainerIndexAddress
	// ContainerElemAssign: an assignment to a field or element of x[k]
	// where x is a container.
	ContainerElemAssign
	// RangeOverContainer: a range over a container, or another struct or
	// pointer to one, that has no All method.
	RangeOverContainer
	// UnaddressableContainer: an index expression on a container that is
	// not addressable, whose method has a pointer receiver.
	UnaddressableContainer
	// BadRangeMethod: a range over a container whose All method is not
	// of the form All() followed by an iterator type.
	BadRangeMethod
	// UnaddressableRange: a range over a container that is not
	// addressable, whose All method has a pointer receiver.
	UnaddressableRange
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
	// UnionOwnerPayload: a variant payload that is an owner, in a union not
	// declared owner.
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
