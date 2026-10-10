## 5. Constants

There are boolean constants, rune constants, integer constants, floating-point constants and string
constants. Rune, integer and floating-point constants are collectively called *numeric constants*.
Plutonium has no complex constants (section 7.2, Numeric types).

A constant value is represented by

- a rune, integer, floating-point or string literal (chapter 4, Lexical elements);
- an identifier denoting a constant, including a member of an enum (section 7.12, Enums);
- a constant expression (section 11.25, Constant expressions);
- a conversion with a result that is a constant;
- the result value of some built-in functions applied to constant arguments: `min` and `max` (section
  13.8, Min and max), `len` and `cap` applied to some expressions (section 13.6, Length and capacity), and
  `unsafe.Sizeof`, `unsafe.Alignof` and `unsafe.Offsetof` applied to certain values (section 20.1, Package
  `unsafe`);
- `len(E)` where `E` is an enum type, the number of its members (section 13.6).

The boolean truth values are represented by the predeclared constants `true` and `false`. The predeclared
identifier `iota` denotes an integer constant (section 10.7, Iota).

Numeric constants represent exact values of arbitrary precision and do not overflow. Consequently, there
are no constants denoting the IEEE 754 negative zero, infinity and not-a-number values.

Constants may be *typed* or *untyped*. Literal constants, `true`, `false`, `iota`, and certain constant
expressions containing only untyped constant operands are untyped.

A constant may be given a type explicitly by a constant declaration (section 10.6, Constant declarations) or
conversion (section 11.24, Conversions), or implicitly when used in a variable declaration or an
assignment statement or as an operand in an expression. It is an error if the constant value cannot be
represented as a value of the respective type (section 8.5, Representability). If the type is a type
parameter, the constant is converted into a non-constant value of the type parameter.

The type of a typed constant is a boolean, numeric or string type, or an enum type, whose underlying type
is an integer type. The members of an enum are typed constants of the enum type. An untyped constant does
not convert implicitly to an enum type (section 8.4, Assignability).

An untyped constant has a *default type*, which is the type to which the constant is implicitly converted
in contexts where a typed value is required, for instance in a short variable declaration such as `i := 0`
where there is no explicit type. The default type of an untyped constant is `bool`, `rune`, `int`,
`float64` or `string`, depending on whether it is a boolean, rune, integer, floating-point or string
constant. The type `int` is 64 bits wide (section 7.2).

```go
const (
	a = 1       // untyped integer constant, default type int
	b = 'x'     // untyped rune constant, default type rune
	c = 2.5     // untyped floating-point constant, default type float64
	d = "text"  // untyped string constant, default type string
	e = a < 2   // untyped boolean constant, default type bool
)

var i = a   // i has type int
var r = b   // r has type rune
var f = c   // f has type float64
```

A constant never allocates and never needs an owner: its value is part of the program (section 18.8,
Strings, for string constants).

**Precision.** Although numeric constants have arbitrary precision in the language, an implementation may
represent them with limited precision. Every implementation must

- represent integer constants with at least 256 bits;
- represent floating-point constants with a mantissa of at least 256 bits and a signed binary exponent of
  at least 16 bits;
- give an error if it is unable to represent an integer constant precisely;
- give an error if it is unable to represent a floating-point constant due to overflow;
- round to the nearest representable constant if it is unable to represent a floating-point constant due
  to limits on precision.

These requirements apply both to literal constants and to the result of evaluating constant expressions.
The exact precision beyond these minimums is left to the implementation. Chapter 21 (Implementation
requirements, section 21.4) states the limits that an implementation must document.

Adapted in part from the Go specification, copyright The Go Authors, CC BY 4.0; see
[Attribution](../language.md#attribution).
